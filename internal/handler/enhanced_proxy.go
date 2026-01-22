package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"ccproxy/internal/circuit"
	"ccproxy/internal/concurrency"
	"ccproxy/internal/loadbalancer"
	"ccproxy/internal/metrics"
	"ccproxy/internal/middleware"
	"ccproxy/internal/pool"
	"ccproxy/internal/ratelimit"
	"ccproxy/internal/retry"
	"ccproxy/internal/scheduler"
	"ccproxy/internal/store"
)

// EnhancedProxyHandler handles proxy requests with advanced features
type EnhancedProxyHandler struct {
	store   *store.Store
	keyPool *loadbalancer.KeyPool
	webURL  string
	apiURL  string

	// New components
	pool        pool.Pool
	scheduler   scheduler.Scheduler
	circuit     circuit.Manager
	concurrency concurrency.Manager
	ratelimit   ratelimit.MultiLimiter
	retry       retry.Executor
	metrics     *metrics.Metrics
}

// EnhancedProxyConfig holds configuration for the enhanced proxy handler
type EnhancedProxyConfig struct {
	Store       *store.Store
	KeyPool     *loadbalancer.KeyPool
	WebURL      string
	APIURL      string
	Pool        pool.Pool
	Scheduler   scheduler.Scheduler
	Circuit     circuit.Manager
	Concurrency concurrency.Manager
	RateLimit   ratelimit.MultiLimiter
	Retry       retry.Executor
	Metrics     *metrics.Metrics
}

// NewEnhancedProxyHandler creates a new enhanced proxy handler
func NewEnhancedProxyHandler(cfg EnhancedProxyConfig) *EnhancedProxyHandler {
	return &EnhancedProxyHandler{
		store:       cfg.Store,
		keyPool:     cfg.KeyPool,
		webURL:      cfg.WebURL,
		apiURL:      cfg.APIURL,
		pool:        cfg.Pool,
		scheduler:   cfg.Scheduler,
		circuit:     cfg.Circuit,
		concurrency: cfg.Concurrency,
		ratelimit:   cfg.RateLimit,
		retry:       cfg.Retry,
		metrics:     cfg.Metrics,
	}
}

// ChatCompletions handles OpenAI-compatible chat completions with enhanced features
func (h *EnhancedProxyHandler) ChatCompletions(c *gin.Context) {
	var req OpenAIChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user info from context
	userID, _ := c.Get(middleware.ContextKeyTokenID)
	userIDStr, _ := userID.(string)

	// Start metrics tracking
	mode := h.determineMode(c)
	tracker := h.metrics.NewRequestTracker(mode, req.Model)
	defer func() {
		tracker.Finish(c.Writer.Status())
	}()

	// Rate limit check
	if h.ratelimit != nil {
		result, err := h.ratelimit.CheckAll(c.Request.Context(), userIDStr, "", c.ClientIP())
		if err != nil || !result.Allowed {
			if h.metrics != nil {
				h.metrics.RecordRateLimitHit("user")
			}
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":    "rate limit exceeded",
				"retry_at": result.RetryAt,
			})
			return
		}
	}

	// Acquire user concurrency slot
	if h.concurrency != nil {
		result, err := h.concurrency.AcquireUserSlot(c.Request.Context(), userIDStr)
		if err != nil {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many concurrent requests"})
			return
		}
		if result.WaitTime > 0 && h.metrics != nil {
			h.metrics.RecordWait("user", result.WaitTime)
		}
		defer h.concurrency.ReleaseUserSlot(userIDStr)
	}

	if mode == "web" {
		h.handleWebModeEnhanced(c, &req, userIDStr, tracker)
	} else {
		h.handleAPIModeEnhanced(c, &req, userIDStr, tracker)
	}
}

func (h *EnhancedProxyHandler) determineMode(c *gin.Context) string {
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

func (h *EnhancedProxyHandler) handleAPIModeEnhanced(c *gin.Context, req *OpenAIChatRequest, userID string, tracker *metrics.RequestTracker) {
	apiKey := h.keyPool.Get()
	if apiKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no API keys available"})
		return
	}

	// Convert OpenAI format to Anthropic format
	anthropicReq := h.convertToAnthropic(req)
	payloadBytes, _ := json.Marshal(anthropicReq)

	targetURL := h.apiURL + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", targetURL, bytes.NewReader(payloadBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}

	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Content-Type", "application/json")

	var resp *http.Response
	if h.pool != nil {
		resp, err = h.pool.Do(httpReq, "api")
	} else {
		client := &http.Client{Timeout: 10 * time.Minute}
		resp, err = client.Do(httpReq)
	}

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

	if req.Stream {
		h.streamAPIResponseEnhanced(c, resp, req.Model, tracker)
	} else {
		h.handleAPIResponseEnhanced(c, resp, req.Model)
	}
}

func (h *EnhancedProxyHandler) handleWebModeEnhanced(c *gin.Context, req *OpenAIChatRequest, userID string, tracker *metrics.RequestTracker) {
	ctx := c.Request.Context()

	// Get available accounts
	accounts, err := h.store.ListAccounts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list accounts"})
		return
	}

	var accountIDs []string
	for _, acc := range accounts {
		if acc.IsActive && !acc.IsExpired() {
			accountIDs = append(accountIDs, acc.ID)
		}
	}

	if len(accountIDs) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no active accounts available"})
		return
	}

	// Generate sticky session hash
	stickyOpts := scheduler.StickyHashOptions{
		UserID: userID,
	}
	for _, msg := range req.Messages {
		if msg.Role == "system" && stickyOpts.SystemPrompt == "" {
			stickyOpts.SystemPrompt = msg.Content
		} else if msg.Role == "user" && len(stickyOpts.Messages) == 0 {
			stickyOpts.Messages = append(stickyOpts.Messages, msg.Content)
		}
	}
	sessionHash := scheduler.GenerateStickyHash(stickyOpts)

	// Select account with retry support
	selectFn := func(ctx context.Context, excludeIDs []string) (string, error) {
		if h.scheduler != nil {
			result, err := h.scheduler.SelectAccountWithRetry(ctx, scheduler.SelectOptions{
				AccountIDs:  accountIDs,
				SessionHash: sessionHash,
				UserID:      userID,
			}, excludeIDs)
			if err != nil {
				return "", err
			}
			return result.AccountID, nil
		}
		// Fallback: return first available
		for _, id := range accountIDs {
			excluded := false
			for _, ex := range excludeIDs {
				if id == ex {
					excluded = true
					break
				}
			}
			if !excluded {
				return id, nil
			}
		}
		return "", fmt.Errorf("no accounts available")
	}

	// Operation function
	opFn := func(ctx context.Context, accountID string) (*http.Response, error) {
		return h.executeWebRequest(ctx, accountID, req)
	}

	// Execute with retry
	var result *retry.ExecuteResult
	if h.retry != nil {
		result, err = h.retry.Execute(ctx, selectFn, opFn)
	} else {
		// Simple execution without retry
		accountID, err := selectFn(ctx, nil)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		resp, err := opFn(ctx, accountID)
		result = &retry.ExecuteResult{
			Response:  resp,
			AccountID: accountID,
			Attempts:  1,
		}
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
	}

	if err != nil {
		log.Error().Err(err).Int("attempts", result.Attempts).Int("switches", result.AccountSwitches).Msg("web request failed")
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	if result.Response == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "no response"})
		return
	}
	defer result.Response.Body.Close()

	// Update account last used
	go h.store.UpdateAccountLastUsed(result.AccountID)

	// Record metrics
	if h.metrics != nil {
		h.metrics.RecordAccountRequest(result.AccountID)
	}

	if result.Response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(result.Response.Body)
		c.Data(result.Response.StatusCode, "application/json", body)
		return
	}

	if req.Stream {
		h.streamWebResponseEnhanced(c, result.Response, req.Model, tracker)
	} else {
		h.handleWebResponseEnhanced(c, result.Response, req.Model)
	}
}

// executeWebRequest executes a web request for a specific account
func (h *EnhancedProxyHandler) executeWebRequest(ctx context.Context, accountID string, req *OpenAIChatRequest) (*http.Response, error) {
	account, err := h.store.GetAccount(accountID)
	if err != nil || account == nil {
		return nil, fmt.Errorf("account not found: %s", accountID)
	}

	// Acquire account concurrency slot
	if h.concurrency != nil {
		result, err := h.concurrency.AcquireAccountSlot(ctx, accountID)
		if err != nil {
			return nil, fmt.Errorf("account concurrency limit: %w", err)
		}
		if result.WaitTime > 0 && h.metrics != nil {
			h.metrics.RecordWait("account", result.WaitTime)
		}
		defer h.concurrency.ReleaseAccountSlot(accountID)
	}

	// Check circuit breaker
	if h.circuit != nil && !h.circuit.IsAvailable(accountID) {
		return nil, fmt.Errorf("account unavailable (circuit open)")
	}

	// Build prompt from messages
	prompt := h.buildPromptFromMessages(req.Messages)

	// Create conversation
	convUUID := uuid.New().String()
	createPayload := map[string]interface{}{
		"uuid": convUUID,
		"name": "",
	}
	createPayloadBytes, _ := json.Marshal(createPayload)

	createURL := fmt.Sprintf("%s/api/organizations/%s/chat_conversations", h.webURL, account.OrganizationID)
	createReq, _ := http.NewRequestWithContext(ctx, "POST", createURL, bytes.NewReader(createPayloadBytes))
	h.setWebHeaders(createReq, account)
	createReq.Header.Set("Content-Type", "application/json")

	var createResp *http.Response
	if h.pool != nil {
		createResp, err = h.pool.Do(createReq, accountID)
	} else {
		client := &http.Client{Timeout: 30 * time.Second}
		createResp, err = client.Do(createReq)
	}

	if err != nil {
		h.recordAccountError(accountID)
		return nil, fmt.Errorf("failed to create conversation: %w", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusOK && createResp.StatusCode != http.StatusCreated {
		h.recordAccountError(accountID)
		body, _ := io.ReadAll(createResp.Body)
		return nil, fmt.Errorf("failed to create conversation: %s", string(body))
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
	msgReq, _ := http.NewRequestWithContext(ctx, "POST", msgURL, bytes.NewReader(msgPayloadBytes))
	h.setWebHeaders(msgReq, account)
	msgReq.Header.Set("Content-Type", "application/json")
	msgReq.Header.Set("Accept", "text/event-stream")

	var msgResp *http.Response
	if h.pool != nil {
		msgResp, err = h.pool.Do(msgReq, accountID)
	} else {
		client := &http.Client{Timeout: 10 * time.Minute}
		msgResp, err = client.Do(msgReq)
	}

	if err != nil {
		h.recordAccountError(accountID)
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	if msgResp.StatusCode != http.StatusOK {
		h.recordAccountError(accountID)
	} else {
		h.recordAccountSuccess(accountID)
	}

	return msgResp, nil
}

func (h *EnhancedProxyHandler) recordAccountError(accountID string) {
	if h.circuit != nil {
		h.circuit.RecordFailure(accountID)
	}
	if h.metrics != nil {
		h.metrics.RecordAccountError(accountID)
	}
	go h.store.IncrementAccountError(accountID)
}

func (h *EnhancedProxyHandler) recordAccountSuccess(accountID string) {
	if h.circuit != nil {
		h.circuit.RecordSuccess(accountID)
	}
	go h.store.IncrementAccountSuccess(accountID)
}

func (h *EnhancedProxyHandler) convertToAnthropic(req *OpenAIChatRequest) *AnthropicRequest {
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

func (h *EnhancedProxyHandler) buildPromptFromMessages(messages []OpenAIMessage) string {
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

func (h *EnhancedProxyHandler) setWebHeaders(req *http.Request, account *store.Account) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Sec-Ch-Ua", `"Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"macOS"`)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Origin", h.webURL)
	req.Header.Set("Referer", h.webURL+"/")

	if account.IsOAuth() {
		req.Header.Set("Authorization", "Bearer "+account.Credentials.AccessToken)
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	} else {
		req.Header.Set("Cookie", fmt.Sprintf("sessionKey=%s", account.Credentials.SessionKey))
	}
}

func (h *EnhancedProxyHandler) handleAPIResponseEnhanced(c *gin.Context, resp *http.Response, model string) {
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

	openaiResp := h.convertToOpenAI(&anthropicResp, model)
	c.JSON(http.StatusOK, openaiResp)
}

func (h *EnhancedProxyHandler) convertToOpenAI(resp *AnthropicResponse, model string) *OpenAIChatResponse {
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

func (h *EnhancedProxyHandler) streamAPIResponseEnhanced(c *gin.Context, resp *http.Response, model string, tracker *metrics.RequestTracker) {
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
	firstToken := true

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

		switch event.Type {
		case "content_block_delta":
			if event.Delta != nil && event.Delta.Text != "" {
				if firstToken {
					tracker.RecordTTFT()
					firstToken = false
				}
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

func (h *EnhancedProxyHandler) handleWebResponseEnhanced(c *gin.Context, resp *http.Response, model string) {
	var content strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	stopReason := "stop"

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var event map[string]interface{}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			if completion, ok := event["completion"].(string); ok && completion != "" {
				content.WriteString(completion)
			}

			if reason, ok := event["stop_reason"].(string); ok && reason != "" {
				stopReason = reason
				if reason == "max_tokens" {
					stopReason = "length"
				}
			}
		}
	}

	if content.Len() == 0 {
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

func (h *EnhancedProxyHandler) streamWebResponseEnhanced(c *gin.Context, resp *http.Response, model string, tracker *metrics.RequestTracker) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	responseID := "chatcmpl-" + uuid.New().String()[:8]
	firstToken := true

	defer func() {
		fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
		c.Writer.Flush()
	}()

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if completion, ok := event["completion"].(string); ok && completion != "" {
			if firstToken {
				tracker.RecordTTFT()
				firstToken = false
			}
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
}

// ListModels returns available models (OpenAI-compatible)
func (h *EnhancedProxyHandler) ListModels(c *gin.Context) {
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
