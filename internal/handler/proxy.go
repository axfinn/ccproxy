package handler

import (
	"bufio"
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
}

type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	System        string             `json:"system,omitempty"`
}

type AnthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	var req OpenAIChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
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
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			if anthropicReq.System != "" {
				anthropicReq.System += "\n"
			}
			anthropicReq.System += msg.Content
		} else {
			role := msg.Role
			if role == "assistant" {
				role = "assistant"
			} else {
				role = "user"
			}
			anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{
				Role:    role,
				Content: msg.Content,
			})
		}
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
			parts = append(parts, fmt.Sprintf("[System: %s]", msg.Content))
		case "user":
			parts = append(parts, msg.Content)
		case "assistant":
			parts = append(parts, fmt.Sprintf("[Assistant: %s]", msg.Content))
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
		// Add OAuth beta flag if available
		r.SetHeader("anthropic-beta", "oauth-2025-04-20")
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
