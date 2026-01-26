package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/imroc/req/v3"
	"github.com/rs/zerolog/log"

	"ccproxy/internal/httpclient"
	"ccproxy/internal/loadbalancer"
	"ccproxy/internal/middleware"
	"ccproxy/internal/store"
)

// ProxyHandler handles unified proxy requests with OpenAI-compatible interface
type ProxyHandler struct {
	store     *store.Store
	keyPool   *loadbalancer.KeyPool
	webURL    string
	apiURL    string
	reqClient *req.Client
}

func NewProxyHandler(store *store.Store, keyPool *loadbalancer.KeyPool, webURL, apiURL string) *ProxyHandler {
	return &ProxyHandler{
		store:     store,
		keyPool:   keyPool,
		webURL:    webURL,
		apiURL:    apiURL,
		reqClient: httpclient.GetClient(),
	}
}

// OpenAI-compatible request/response structures
type OpenAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	TopP        float64         `json:"top_p,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Stop        []string        `json:"stop,omitempty"`
	Metadata    map[string]any  `json:"metadata,omitempty"`
}

type OpenAIMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or []any for content blocks
}

type OpenAIChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   *OpenAIUsage   `json:"usage,omitempty"`
}

type OpenAIChoice struct {
	Index        int          `json:"index"`
	Message      OpenAIMessage `json:"message,omitempty"`
	Delta        *OpenAIMessage `json:"delta,omitempty"`
	FinishReason *string      `json:"finish_reason"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Anthropic API structures
type AnthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []AnthropicMessage `json:"messages"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   float64            `json:"temperature,omitempty"`
	TopP          float64            `json:"top_p,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	System        interface{}        `json:"system,omitempty"` // Can be string or []any for system blocks
}

type AnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or []any for content blocks
}

// extractTextFromContent 提取 content 字段中的文本内容
// 支持两种格式:
// 1. 字符串: "content": "text"
// 2. 内容块数组: "content": [{"type": "text", "text": "..."}]
func extractTextFromContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var texts []string
		for _, block := range v {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if blockType, ok := blockMap["type"].(string); ok && blockType == "text" {
					if text, ok := blockMap["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
		}
		return strings.Join(texts, "")
	default:
		return ""
	}
}

// appendToSystem 向 system 字段追加文本，处理 interface{} 类型
func appendToSystem(system interface{}, text string) string {
	if text == "" {
		return extractTextFromContent(system)
	}
	existing := extractTextFromContent(system)
	if existing == "" {
		return text
	}
	return existing + "\n" + text
}

// FilterThinkingBlocks removes invalid thinking blocks; fail-safe returns original body on errors.
// Mirrors sub2api behaviour to avoid upstream 400 when thinking signatures are missing/invalid.
func FilterThinkingBlocks(body []byte) []byte {
	return filterThinkingBlocksInternal(body, false)
}

// FilterThinkingBlocksForRetry is a stricter variant for retry; preserves thinking content as text.
func FilterThinkingBlocksForRetry(body []byte) []byte {
	hasThinkingContent := bytes.Contains(body, []byte(`"type":"thinking"`)) ||
		bytes.Contains(body, []byte(`"type": "thinking"`)) ||
		bytes.Contains(body, []byte(`"type":"redacted_thinking"`)) ||
		bytes.Contains(body, []byte(`"type": "redacted_thinking"`)) ||
		bytes.Contains(body, []byte(`"thinking":`)) ||
		bytes.Contains(body, []byte(`"thinking" :`))

	hasEmptyContent := bytes.Contains(body, []byte(`"content":[]`)) ||
		bytes.Contains(body, []byte(`"content": []`)) ||
		bytes.Contains(body, []byte(`"content" : []`)) ||
		bytes.Contains(body, []byte(`"content" :[]`))

	if !hasThinkingContent && !hasEmptyContent {
		return body
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	modified := false

	messages, ok := req["messages"].([]any)
	if !ok {
		return body
	}

	if _, exists := req["thinking"]; exists {
		delete(req, "thinking")
		modified = true
	}

	newMessages := make([]any, 0, len(messages))
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			newMessages = append(newMessages, msg)
			continue
		}

		role, _ := msgMap["role"].(string)
		content, ok := msgMap["content"].([]any)
		if !ok {
			newMessages = append(newMessages, msg)
			continue
		}

		newContent := make([]any, 0, len(content))
		modifiedThisMsg := false

		for _, block := range content {
			blockMap, ok := block.(map[string]any)
			if !ok {
				newContent = append(newContent, block)
				continue
			}

			blockType, _ := blockMap["type"].(string)
			switch blockType {
			case "thinking":
				modifiedThisMsg = true
				thinkingText, _ := blockMap["thinking"].(string)
				if thinkingText == "" {
					continue
				}
				newContent = append(newContent, map[string]any{"type": "text", "text": thinkingText})
				continue
			case "redacted_thinking":
				modifiedThisMsg = true
				continue
			}

			if blockType == "" {
				if rawThinking, hasThinking := blockMap["thinking"]; hasThinking {
					modifiedThisMsg = true
					switch v := rawThinking.(type) {
					case string:
						if v != "" {
							newContent = append(newContent, map[string]any{"type": "text", "text": v})
						}
					default:
						if b, err := json.Marshal(v); err == nil && len(b) > 0 {
							newContent = append(newContent, map[string]any{"type": "text", "text": string(b)})
						}
					}
					continue
				}
			}

			newContent = append(newContent, block)
		}

		if len(newContent) == 0 {
			modified = true
			placeholder := "(content removed)"
			if role == "assistant" {
				placeholder = "(assistant content removed)"
			}
			newContent = append(newContent, map[string]any{"type": "text", "text": placeholder})
			msgMap["content"] = newContent
		} else if modifiedThisMsg {
			modified = true
			msgMap["content"] = newContent
		}

		newMessages = append(newMessages, msgMap)
	}

	if modified {
		req["messages"] = newMessages
	} else {
		return body
	}

	newBody, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return newBody
}

// FilterSignatureSensitiveBlocksForRetry converts tool_use/tool_result to text for signature errors.
func FilterSignatureSensitiveBlocksForRetry(body []byte) []byte {
	if !bytes.Contains(body, []byte(`"type":"thinking"`)) &&
		!bytes.Contains(body, []byte(`"type": "thinking"`)) &&
		!bytes.Contains(body, []byte(`"type":"redacted_thinking"`)) &&
		!bytes.Contains(body, []byte(`"type": "redacted_thinking"`)) &&
		!bytes.Contains(body, []byte(`"type":"tool_use"`)) &&
		!bytes.Contains(body, []byte(`"type": "tool_use"`)) &&
		!bytes.Contains(body, []byte(`"type":"tool_result"`)) &&
		!bytes.Contains(body, []byte(`"type": "tool_result"`)) &&
		!bytes.Contains(body, []byte(`"thinking":`)) &&
		!bytes.Contains(body, []byte(`"thinking" :`)) {
		return body
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	modified := false

	if _, exists := req["thinking"]; exists {
		delete(req, "thinking")
		modified = true
	}

	messages, ok := req["messages"].([]any)
	if !ok {
		return body
	}

	newMessages := make([]any, 0, len(messages))
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			newMessages = append(newMessages, msg)
			continue
		}

		role, _ := msgMap["role"].(string)
		content, ok := msgMap["content"].([]any)
		if !ok {
			newMessages = append(newMessages, msg)
			continue
		}

		newContent := make([]any, 0, len(content))
		modifiedThisMsg := false

		for _, block := range content {
			blockMap, ok := block.(map[string]any)
			if !ok {
				newContent = append(newContent, block)
				continue
			}

			blockType, _ := blockMap["type"].(string)
			switch blockType {
			case "thinking":
				modifiedThisMsg = true
				thinkingText, _ := blockMap["thinking"].(string)
				if thinkingText == "" {
					continue
				}
				newContent = append(newContent, map[string]any{"type": "text", "text": thinkingText})
				continue
			case "redacted_thinking":
				modifiedThisMsg = true
				continue
			case "tool_use":
				modifiedThisMsg = true
				name, _ := blockMap["name"].(string)
				id, _ := blockMap["id"].(string)
				input := blockMap["input"]
				inputJSON, _ := json.Marshal(input)
				text := "(tool_use)"
				if name != "" {
					text += " name=" + name
				}
				if id != "" {
					text += " id=" + id
				}
				if len(inputJSON) > 0 && string(inputJSON) != "null" {
					text += " input=" + string(inputJSON)
				}
				newContent = append(newContent, map[string]any{"type": "text", "text": text})
				continue
			case "tool_result":
				modifiedThisMsg = true
				toolUseID, _ := blockMap["tool_use_id"].(string)
				isError, _ := blockMap["is_error"].(bool)
				blockContent := blockMap["content"]
				contentJSON, _ := json.Marshal(blockContent)
				text := "(tool_result)"
				if toolUseID != "" {
					text += " tool_use_id=" + toolUseID
				}
				if isError {
					text += " is_error=true"
				}
				if len(contentJSON) > 0 && string(contentJSON) != "null" {
					text += "\n" + string(contentJSON)
				}
				newContent = append(newContent, map[string]any{"type": "text", "text": text})
				continue
			}

			if blockType == "" {
				if rawThinking, hasThinking := blockMap["thinking"]; hasThinking {
					modifiedThisMsg = true
					switch v := rawThinking.(type) {
					case string:
						if v != "" {
							newContent = append(newContent, map[string]any{"type": "text", "text": v})
						}
					default:
						if b, err := json.Marshal(v); err == nil && len(b) > 0 {
							newContent = append(newContent, map[string]any{"type": "text", "text": string(b)})
						}
					}
					continue
				}
			}

			newContent = append(newContent, block)
		}

		if modifiedThisMsg {
			modified = true
			if len(newContent) == 0 {
				placeholder := "(content removed)"
				if role == "assistant" {
					placeholder = "(assistant content removed)"
				}
				newContent = append(newContent, map[string]any{"type": "text", "text": placeholder})
			}
			msgMap["content"] = newContent
		}

		newMessages = append(newMessages, msgMap)
	}

	if !modified {
		return body
	}

	req["messages"] = newMessages
	newBody, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return newBody
}

// filterThinkingBlocksInternal removes invalid thinking blocks while respecting enabled thinking.
func filterThinkingBlocksInternal(body []byte, _ bool) []byte {
	if !bytes.Contains(body, []byte(`"type":"thinking"`)) &&
		!bytes.Contains(body, []byte(`"type": "thinking"`)) &&
		!bytes.Contains(body, []byte(`"type":"redacted_thinking"`)) &&
		!bytes.Contains(body, []byte(`"type": "redacted_thinking"`)) &&
		!bytes.Contains(body, []byte(`"thinking":`)) &&
		!bytes.Contains(body, []byte(`"thinking" :`)) {
		return body
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	thinkingEnabled := false
	if thinking, ok := req["thinking"].(map[string]any); ok {
		if thinkType, ok := thinking["type"].(string); ok && thinkType == "enabled" {
			thinkingEnabled = true
		}
	}

	messages, ok := req["messages"].([]any)
	if !ok {
		return body
	}

	filtered := false
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msgMap["role"].(string)
		content, ok := msgMap["content"].([]any)
		if !ok {
			continue
		}

		newContent := make([]any, 0, len(content))
		filteredThisMessage := false

		for _, block := range content {
			blockMap, ok := block.(map[string]any)
			if !ok {
				newContent = append(newContent, block)
				continue
			}

			blockType, _ := blockMap["type"].(string)
			if blockType == "thinking" || blockType == "redacted_thinking" {
				if thinkingEnabled && role == "assistant" {
					signature, _ := blockMap["signature"].(string)
					if signature != "" && signature != "skip_thought_signature_validator" {
						newContent = append(newContent, block)
						continue
					}
				}
				filtered = true
				filteredThisMessage = true
				continue
			}

			if blockType == "" {
				if _, hasThinking := blockMap["thinking"]; hasThinking {
					filtered = true
					filteredThisMessage = true
					continue
				}
			}

			newContent = append(newContent, block)
		}

		if filteredThisMessage {
			msgMap["content"] = newContent
		}
	}

	if !filtered {
		return body
	}

	newBody, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return newBody
}

type AnthropicResponse struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Content      []AnthropicContent `json:"content"`
	Model        string             `json:"model"`
	StopReason   string             `json:"stop_reason"`
	StopSequence *string            `json:"stop_sequence"`
	Usage        AnthropicUsage     `json:"usage"`
}

type AnthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AnthropicStreamEvent represents an SSE event from Anthropic
type AnthropicStreamEvent struct {
	Type         string            `json:"type"`
	Index        int               `json:"index,omitempty"`
	ContentBlock *AnthropicContent `json:"content_block,omitempty"`
	Delta        *AnthropicDelta   `json:"delta,omitempty"`
	Message      *AnthropicResponse `json:"message,omitempty"`
	Usage        *AnthropicUsage   `json:"usage,omitempty"`
}

type AnthropicDelta struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
}

// ChatCompletions handles OpenAI-compatible chat completions
func (h *ProxyHandler) ChatCompletions(c *gin.Context) {
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}
	// Restore body for downstream and apply thinking filters before unmarshalling.
	filteredBody := FilterThinkingBlocks(rawBody)
	c.Request.Body = io.NopCloser(bytes.NewBuffer(filteredBody))

	var req OpenAIChatRequest
	if err := json.Unmarshal(filteredBody, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Determine mode based on token or configuration
	mode := h.determineMode(c)

	if mode == "web" {
		h.handleWebMode(c, &req)
	} else {
		h.handleAPIMode(c, &req)
	}
}

func (h *ProxyHandler) determineMode(c *gin.Context) string {
	// Check token mode
	if tokenMode, exists := c.Get(middleware.ContextKeyTokenMode); exists {
		mode := tokenMode.(string)
		if mode != "both" {
			return mode
		}
	}

	// Check header override
	if modeHeader := c.GetHeader("X-Proxy-Mode"); modeHeader != "" {
		if modeHeader == "web" || modeHeader == "api" {
			return modeHeader
		}
	}

	// Default to API mode if keys are available, otherwise web
	if h.keyPool.Size() > 0 {
		return "api"
	}
	return "web"
}

func (h *ProxyHandler) handleAPIMode(c *gin.Context, openaiReq *OpenAIChatRequest) {
	apiKey := h.keyPool.Get()
	if apiKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no API keys available"})
		return
	}

	// Convert OpenAI format to Anthropic format
	anthropicReq := h.convertToAnthropic(openaiReq)
	payloadBytes, _ := json.Marshal(anthropicReq)

	targetURL := h.apiURL + "/v1/messages"

	r := h.reqClient.R().
		SetContext(c.Request.Context()).
		SetBodyBytes(payloadBytes).
		SetHeader("x-api-key", apiKey).
		SetHeader("anthropic-version", "2023-06-01").
		SetHeader("Content-Type", "application/json")

	// Enable streaming for response
	r.DisableAutoReadResponse()

	resp, err := r.Post(targetURL)
	if err != nil {
		h.keyPool.ReportError(apiKey)
		log.Error().Err(err).Msg("failed to call Anthropic API")
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to Anthropic API"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		h.keyPool.ReportSuccess(apiKey)
	} else if resp.StatusCode == 401 || resp.StatusCode == 403 {
		h.keyPool.ReportError(apiKey)
	}

	if openaiReq.Stream {
		h.streamAPIResponse(c, resp, openaiReq.Model)
	} else {
		h.handleAPIResponse(c, resp, openaiReq.Model)
	}
}

func (h *ProxyHandler) convertToAnthropic(req *OpenAIChatRequest) *AnthropicRequest {
	anthropicReq := &AnthropicRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}

	if anthropicReq.MaxTokens == 0 {
		anthropicReq.MaxTokens = 4096
	}

	if len(req.Stop) > 0 {
		anthropicReq.StopSequences = req.Stop
	}

	// Convert messages and extract system prompt
	var systemText string
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemText = appendToSystem(systemText, extractTextFromContent(msg.Content))
		} else {
			role := msg.Role
			if role == "assistant" {
				role = "assistant"
			} else {
				role = "user"
			}
			anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{
				Role:    role,
				Content: msg.Content, // Keep original format (string or []any)
			})
		}
	}

	if systemText != "" {
		anthropicReq.System = systemText
	}

	return anthropicReq
}

func (h *ProxyHandler) handleAPIResponse(c *gin.Context, resp *req.Response, model string) {
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, "application/json", body)
		return
	}

	var anthropicResp AnthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse response"})
		return
	}

	// Convert to OpenAI format
	openaiResp := h.convertToOpenAI(&anthropicResp, model)
	c.JSON(http.StatusOK, openaiResp)
}

func (h *ProxyHandler) convertToOpenAI(resp *AnthropicResponse, model string) *OpenAIChatResponse {
	content := ""
	for _, c := range resp.Content {
		if c.Type == "text" {
			content += c.Text
		}
	}

	finishReason := "stop"
	if resp.StopReason == "max_tokens" {
		finishReason = "length"
	}

	return &OpenAIChatResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []OpenAIChoice{
			{
				Index: 0,
				Message: OpenAIMessage{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: &finishReason,
			},
		},
		Usage: &OpenAIUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
}

func (h *ProxyHandler) streamAPIResponse(c *gin.Context, resp *req.Response, model string) {
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, "application/json", body)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	responseID := "chatcmpl-" + uuid.New().String()[:8]

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			c.Writer.Flush()
			continue
		}

		var event AnthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		// Convert Anthropic stream events to OpenAI format
		switch event.Type {
		case "content_block_delta":
			if event.Delta != nil && event.Delta.Text != "" {
				chunk := OpenAIChatResponse{
					ID:      responseID,
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   model,
					Choices: []OpenAIChoice{
						{
							Index: 0,
							Delta: &OpenAIMessage{
								Content: event.Delta.Text,
							},
							FinishReason: nil,
						},
					},
				}
				chunkJSON, _ := json.Marshal(chunk)
				fmt.Fprintf(c.Writer, "data: %s\n\n", chunkJSON)
				c.Writer.Flush()
			}
		case "message_delta":
			if event.Delta != nil && event.Delta.StopReason != "" {
				finishReason := "stop"
				if event.Delta.StopReason == "max_tokens" {
					finishReason = "length"
				}
				chunk := OpenAIChatResponse{
					ID:      responseID,
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   model,
					Choices: []OpenAIChoice{
						{
							Index:        0,
							Delta:        &OpenAIMessage{},
							FinishReason: &finishReason,
						},
					},
				}
				chunkJSON, _ := json.Marshal(chunk)
				fmt.Fprintf(c.Writer, "data: %s\n\n", chunkJSON)
				c.Writer.Flush()
			}
		case "message_stop":
			fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			c.Writer.Flush()
		}
	}
}

func (h *ProxyHandler) handleWebMode(c *gin.Context, openaiReq *OpenAIChatRequest) {
	account, err := h.store.GetActiveAccount()
	if err != nil || account == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no active account available"})
		return
	}

	// For web mode, we need to:
	// 1. Create a conversation (or use existing)
	// 2. Send the message and get response

	// Build prompt from messages
	prompt := h.buildPromptFromMessages(openaiReq.Messages)

	// Create conversation
	convUUID := uuid.New().String()
	createPayload := map[string]interface{}{
		"uuid": convUUID,
		"name": "",
	}
	createPayloadBytes, _ := json.Marshal(createPayload)

	createURL := fmt.Sprintf("%s/api/organizations/%s/chat_conversations", h.webURL, account.OrganizationID)

	r := h.reqClient.R().
		SetContext(c.Request.Context()).
		SetBodyBytes(createPayloadBytes)
	h.setReqHeaders(r, account)
	r.SetHeader("Content-Type", "application/json")

	createResp, err := r.Post(createURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to claude.ai"})
		return
	}

	if createResp.StatusCode != http.StatusOK && createResp.StatusCode != http.StatusCreated {
		log.Error().Int("status", createResp.StatusCode).Str("body", createResp.String()).Msg("failed to create conversation")
		c.JSON(createResp.StatusCode, gin.H{"error": "failed to create conversation", "details": createResp.String()})
		return
	}

	// Send message
	msgPayload := map[string]interface{}{
		"prompt":      prompt,
		"timezone":    "UTC",
		"attachments": []any{},
		"files":       []any{},
	}
	msgPayloadBytes, _ := json.Marshal(msgPayload)

	msgURL := fmt.Sprintf("%s/api/organizations/%s/chat_conversations/%s/completion",
		h.webURL, account.OrganizationID, convUUID)

	msgR := h.reqClient.R().
		SetContext(c.Request.Context()).
		SetBodyBytes(msgPayloadBytes)
	h.setReqHeaders(msgR, account)
	msgR.SetHeader("Content-Type", "application/json")
	msgR.SetHeader("Accept", "text/event-stream")

	// Enable streaming response
	msgR.DisableAutoReadResponse()

	msgResp, err := msgR.Post(msgURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to claude.ai"})
		return
	}
	defer msgResp.Body.Close()

	go h.store.UpdateAccountLastUsed(account.ID)

	if msgResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(msgResp.Body)
		c.Data(msgResp.StatusCode, "application/json", body)
		return
	}

	if openaiReq.Stream {
		h.streamWebResponse(c, msgResp, openaiReq.Model)
	} else {
		h.handleWebResponse(c, msgResp, openaiReq.Model)
	}
}

func (h *ProxyHandler) buildPromptFromMessages(messages []OpenAIMessage) string {
	var parts []string
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			parts = append(parts, fmt.Sprintf("[System: %s]", extractTextFromContent(msg.Content)))
		case "user":
			parts = append(parts, extractTextFromContent(msg.Content))
		case "assistant":
			parts = append(parts, fmt.Sprintf("[Assistant: %s]", extractTextFromContent(msg.Content)))
		}
	}
	return strings.Join(parts, "\n\n")
}

func (h *ProxyHandler) handleWebResponse(c *gin.Context, resp *req.Response, model string) {
	// Read and parse SSE to extract complete response
	var content strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	eventCount := 0
	stopReason := "stop"

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue // Skip empty lines
		}

		eventCount++
		log.Debug().Str("line", line).Int("event", eventCount).Msg("SSE event")

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Handle special [DONE] marker
			if data == "[DONE]" {
				break
			}

			var event map[string]interface{}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				log.Debug().Err(err).Str("data", data).Msg("failed to unmarshal SSE event")
				continue
			}

			log.Debug().Interface("event", event).Msg("parsed SSE event")

			// Extract completion text
			if completion, ok := event["completion"].(string); ok && completion != "" {
				content.WriteString(completion)
			}

			// Extract stop reason if present
			if reason, ok := event["stop_reason"].(string); ok && reason != "" {
				stopReason = reason
				if reason == "max_tokens" {
					stopReason = "length"
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Error().Err(err).Msg("scanner error reading SSE stream")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read stream"})
		return
	}

	log.Info().Int("events", eventCount).Int("content_length", content.Len()).Msg("finished reading web response")

	// Check if we got any content
	if content.Len() == 0 {
		log.Warn().Msg("no content received from claude.ai")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no response content"})
		return
	}

	openaiResp := &OpenAIChatResponse{
		ID:      "chatcmpl-" + uuid.New().String()[:8],
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []OpenAIChoice{
			{
				Index: 0,
				Message: OpenAIMessage{
					Role:    "assistant",
					Content: content.String(),
				},
				FinishReason: &stopReason,
			},
		},
	}

	c.JSON(http.StatusOK, openaiResp)
}

func (h *ProxyHandler) streamWebResponse(c *gin.Context, resp *req.Response, model string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	responseID := "chatcmpl-" + uuid.New().String()[:8]

	defer func() {
		// Always send [DONE] at the end
		fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
		c.Writer.Flush()
	}()

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue // Skip empty lines
		}

		log.Debug().Str("line", line).Msg("streaming SSE event")

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// Handle special [DONE] marker
		if data == "[DONE]" {
			break
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			log.Debug().Err(err).Str("data", data).Msg("failed to unmarshal streaming SSE event")
			continue
		}

		log.Debug().Interface("event", event).Msg("parsed streaming SSE event")

		// Send completion chunks
		if completion, ok := event["completion"].(string); ok && completion != "" {
			chunk := OpenAIChatResponse{
				ID:      responseID,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   model,
				Choices: []OpenAIChoice{
					{
						Index: 0,
						Delta: &OpenAIMessage{
							Content: completion,
						},
						FinishReason: nil,
					},
				},
			}
			chunkJSON, _ := json.Marshal(chunk)
			fmt.Fprintf(c.Writer, "data: %s\n\n", chunkJSON)
			c.Writer.Flush()
		}

		// Send finish reason when stop_reason is present
		if stopReason, ok := event["stop_reason"].(string); ok && stopReason != "" {
			finishReason := "stop"
			if stopReason == "max_tokens" {
				finishReason = "length"
			}
			chunk := OpenAIChatResponse{
				ID:      responseID,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   model,
				Choices: []OpenAIChoice{
					{
						Index:        0,
						Delta:        &OpenAIMessage{},
						FinishReason: &finishReason,
					},
				},
			}
			chunkJSON, _ := json.Marshal(chunk)
			fmt.Fprintf(c.Writer, "data: %s\n\n", chunkJSON)
			c.Writer.Flush()
			break
		}
	}

	if err := scanner.Err(); err != nil {
		log.Error().Err(err).Msg("scanner error in stream")
		// Send error event
		errorChunk := map[string]interface{}{
			"error": map[string]interface{}{
				"message": "stream read error",
				"type":    "stream_error",
			},
		}
		errorJSON, _ := json.Marshal(errorChunk)
		fmt.Fprintf(c.Writer, "data: %s\n\n", errorJSON)
		c.Writer.Flush()
	}
}

func (h *ProxyHandler) setReqHeaders(r *req.Request, account *store.Account) {
	// Note: User-Agent and TLS fingerprint are already set by ImpersonateChrome()
	// We only need to set additional headers

	// 现代浏览器的 Client Hints
	r.SetHeader("Sec-Ch-Ua", `"Chromium";v="131", "Not_A Brand";v="24"`)
	r.SetHeader("Sec-Ch-Ua-Mobile", "?0")
	r.SetHeader("Sec-Ch-Ua-Platform", `"macOS"`)

	// 安全相关头
	r.SetHeader("Sec-Fetch-Site", "same-origin")
	r.SetHeader("Sec-Fetch-Mode", "cors")
	r.SetHeader("Sec-Fetch-Dest", "empty")

	// 标准请求头
	r.SetHeader("Accept", "application/json")
	r.SetHeader("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7")
	r.SetHeader("Cache-Control", "no-cache")
	r.SetHeader("Pragma", "no-cache")

	// Origin 和 Referer
	r.SetHeader("Origin", h.webURL)
	r.SetHeader("Referer", h.webURL+"/")

	// Set authentication based on account type
	if account.IsOAuth() {
		// OAuth accounts use Bearer token
		r.SetHeader("Authorization", "Bearer "+account.Credentials.AccessToken)
		// Add complete OAuth beta flags (matches sub2api's DefaultBetaHeader)
		// Includes: claude-code, oauth, interleaved-thinking, fine-grained-tool-streaming
		r.SetHeader("anthropic-beta", "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14")
	} else {
		// Session key accounts use Cookie
		r.SetHeader("Cookie", fmt.Sprintf("sessionKey=%s", account.Credentials.SessionKey))
	}
}

// ListModels returns available models (OpenAI-compatible)
func (h *ProxyHandler) ListModels(c *gin.Context) {
	models := []map[string]interface{}{
		{"id": "claude-3-opus-20240229", "object": "model", "owned_by": "anthropic"},
		{"id": "claude-3-sonnet-20240229", "object": "model", "owned_by": "anthropic"},
		{"id": "claude-3-haiku-20240307", "object": "model", "owned_by": "anthropic"},
		{"id": "claude-3-5-sonnet-20240620", "object": "model", "owned_by": "anthropic"},
		{"id": "claude-3-5-sonnet-20241022", "object": "model", "owned_by": "anthropic"},
		{"id": "claude-opus-4-20250514", "object": "model", "owned_by": "anthropic"},
		{"id": "claude-sonnet-4-20250514", "object": "model", "owned_by": "anthropic"},
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}
