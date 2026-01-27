package handler

import (
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

	"ccproxy/internal/service"
	"ccproxy/internal/store"
)

// Sub2APIProxyHandler handles proxy requests with sub2api-style account selection
type Sub2APIProxyHandler struct {
	store           *store.Store
	webURL          string
	errorClassifier *ErrorClassifier
	oauthService    *service.OAuthService // For token refresh (matches sub2api's ClaudeTokenProvider)
}

// NewSub2APIProxyHandler creates a new sub2api-style proxy handler
func NewSub2APIProxyHandler(st *store.Store, webURL string, oauthService *service.OAuthService) *Sub2APIProxyHandler {
	return &Sub2APIProxyHandler{
		store:           st,
		webURL:          webURL,
		errorClassifier: NewErrorClassifier(st),
		oauthService:    oauthService,
	}
}

// getValidAccessToken gets a valid access token for OAuth account, refreshing if needed
// Matches sub2api's ClaudeTokenProvider.GetAccessToken behavior
func (h *Sub2APIProxyHandler) getValidAccessToken(account *store.Account) (string, error) {
	if !account.IsOAuth() {
		return "", fmt.Errorf("account is not an OAuth account")
	}

	// Check if token needs refresh (matches sub2api's claudeTokenRefreshSkew = 3 minutes)
	if account.ExpiresAt != nil && time.Until(*account.ExpiresAt) <= 3*time.Minute {
		log.Info().
			Str("account_id", account.ID).
			Time("expires_at", *account.ExpiresAt).
			Msg("token expiring soon, refreshing")

		// Refresh token
		if err := h.oauthService.RefreshAccountToken(account); err != nil {
			log.Error().Err(err).Str("account_id", account.ID).Msg("failed to refresh token")
			// Don't fail immediately - try using existing token
			// This matches sub2api's behavior of using short TTL cache on refresh failure
		} else {
			log.Info().
				Str("account_id", account.ID).
				Time("new_expires_at", *account.ExpiresAt).
				Msg("token refreshed successfully")
		}
	}

	if account.Credentials.AccessToken == "" {
		return "", fmt.Errorf("access_token not found in credentials")
	}

	return account.Credentials.AccessToken, nil
}

// ChatCompletions handles OpenAI-compatible chat completion requests
func (h *Sub2APIProxyHandler) ChatCompletions(c *gin.Context) {
	var req OpenAIChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messages cannot be empty"})
		return
	}

	ctx := c.Request.Context()

	// Select account with retry logic (sub2api style)
	maxRetries := 3
	var excludedAccountIDs []string

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Get schedulable accounts
		accounts, err := h.store.GetSchedulableAccounts()
		if err != nil {
			log.Error().Err(err).Msg("failed to get schedulable accounts")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query accounts"})
			return
		}

		// Filter out excluded accounts
		var availableAccounts []*store.Account
		for _, acc := range accounts {
			excluded := false
			for _, exID := range excludedAccountIDs {
				if acc.ID == exID {
					excluded = true
					break
				}
			}
			if !excluded && acc.IsSchedulable() {
				availableAccounts = append(availableAccounts, acc)
			}
		}

		if len(availableAccounts) == 0 {
			log.Warn().
				Int("attempt", attempt+1).
				Int("excluded", len(excludedAccountIDs)).
				Msg("no schedulable accounts available")
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":   "no available accounts",
				"details": fmt.Sprintf("excluded=%d, attempt=%d/%d", len(excludedAccountIDs), attempt+1, maxRetries),
			})
			return
		}

		// Select best account (lowest priority, least recently used)
		account := selectBestAccount(availableAccounts)

		log.Info().
			Str("account_id", account.ID).
			Str("account_name", account.Name).
			Int("attempt", attempt+1).
			Int("available", len(availableAccounts)).
			Msg("selected account for request")

		// Execute request
		resp, err := h.executeWebRequest(ctx, account, &req)

		// Handle errors
		if err != nil {
			log.Error().
				Err(err).
				Str("account_id", account.ID).
				Int("attempt", attempt+1).
				Msg("request execution failed")

			// Classify error and update account status
			shouldSwitch := h.errorClassifier.ClassifyAndHandleError(nil, account.ID)

			if shouldSwitch && attempt < maxRetries-1 {
				excludedAccountIDs = append(excludedAccountIDs, account.ID)
				log.Info().Str("account_id", account.ID).Msg("switching to next account")
				continue
			}

			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}

		// Check response status
		if resp.StatusCode >= 400 {
			// Error response, classify and handle
			shouldSwitch := h.errorClassifier.ClassifyAndHandleError(resp, account.ID)

			// For non-success responses, read body and return
			if resp.StatusCode >= 400 {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				// If should switch and we have retries left, try next account
				if shouldSwitch && attempt < maxRetries-1 && (resp.StatusCode == 429 || resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 503) {
					excludedAccountIDs = append(excludedAccountIDs, account.ID)
					log.Info().
						Str("account_id", account.ID).
						Int("status_code", resp.StatusCode).
						Msg("switching account due to error")
					continue
				}

				// Return error to client
				c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
				return
			}
		}

		// Success! Record it
		h.errorClassifier.RecordSuccess(account.ID)
		go h.store.UpdateAccountLastUsed(account.ID)

		// Stream or return response
		if req.Stream {
			h.streamResponse(c, resp, account.ID)
		} else {
			h.returnResponse(c, resp)
		}
		return
	}

	// All retries exhausted
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error": "all retry attempts exhausted",
	})
}

// selectBestAccount selects the best account from available accounts
// Priority: lower priority value > least recently used
func selectBestAccount(accounts []*store.Account) *store.Account {
	if len(accounts) == 0 {
		return nil
	}

	best := accounts[0]
	for _, acc := range accounts[1:] {
		// Lower priority number = higher priority
		if acc.Priority < best.Priority {
			best = acc
			continue
		}

		// Same priority, prefer least recently used
		if acc.Priority == best.Priority {
			if acc.LastUsedAt == nil {
				best = acc // Never used is best
			} else if best.LastUsedAt != nil && acc.LastUsedAt.Before(*best.LastUsedAt) {
				best = acc
			}
		}
	}

	return best
}

// executeWebRequest executes a request to claude.ai
func (h *Sub2APIProxyHandler) executeWebRequest(ctx context.Context, account *store.Account, req *OpenAIChatRequest) (*http.Response, error) {
	// Get valid access token for OAuth accounts (auto-refresh if needed)
	var accessToken string
	if account.IsOAuth() {
		var err error
		accessToken, err = h.getValidAccessToken(account)
		if err != nil {
			return nil, fmt.Errorf("failed to get valid access token: %w", err)
		}
	}

	// Build prompt from messages
	prompt := buildPromptFromMessages(req.Messages)

	// Create conversation
	convUUID := uuid.New().String()
	createPayload := map[string]interface{}{
		"uuid": convUUID,
		"name": "",
	}
	createPayloadBytes, _ := json.Marshal(createPayload)

	createURL := fmt.Sprintf("%s/api/organizations/%s/chat_conversations", h.webURL, account.OrganizationID)
	createReq, _ := http.NewRequestWithContext(ctx, "POST", createURL, bytes.NewReader(createPayloadBytes))
	setWebHeaders(createReq, account, h.webURL, accessToken)
	createReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	createResp, err := client.Do(createReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create conversation: %w", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusOK && createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		return createResp, fmt.Errorf("failed to create conversation: %s", string(body))
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
	setWebHeaders(msgReq, account, h.webURL, accessToken)
	msgReq.Header.Set("Content-Type", "application/json")
	msgReq.Header.Set("Accept", "text/event-stream")

	msgResp, err := client.Do(msgReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	return msgResp, nil
}

// setWebHeaders sets request headers for claude.ai requests
// accessToken parameter is used for OAuth accounts (empty for session_key accounts)
func setWebHeaders(r *http.Request, account *store.Account, webURL string, accessToken string) {
	// Modern browser Client Hints
	r.Header.Set("Sec-Ch-Ua", `"Chromium";v="131", "Not_A Brand";v="24"`)
	r.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	r.Header.Set("Sec-Ch-Ua-Platform", `"macOS"`)

	// Security headers
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Sec-Fetch-Mode", "cors")
	r.Header.Set("Sec-Fetch-Dest", "empty")

	// Standard headers
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7")
	r.Header.Set("Cache-Control", "no-cache")
	r.Header.Set("Pragma", "no-cache")

	// Origin and Referer
	r.Header.Set("Origin", webURL)
	r.Header.Set("Referer", webURL+"/")

	// Set authentication (matches sub2api's complete beta header for OAuth)
	if account.IsOAuth() {
		r.Header.Set("Authorization", "Bearer "+accessToken)
		// Complete OAuth beta flags (matches sub2api's DefaultBetaHeader)
		r.Header.Set("anthropic-beta", "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14")
	} else {
		r.Header.Set("Cookie", fmt.Sprintf("sessionKey=%s", account.Credentials.SessionKey))
	}
}

// buildPromptFromMessages builds a prompt string from OpenAI messages
func buildPromptFromMessages(messages []OpenAIMessage) string {
	var prompt string
	for _, msg := range messages {
		if msg.Role == "user" {
			if str, ok := msg.Content.(string); ok {
				prompt = str
				break
			}
		}
	}
	return prompt
}

// streamResponse streams the response back to the client
func (h *Sub2APIProxyHandler) streamResponse(c *gin.Context, resp *http.Response, _ string) {
	defer resp.Body.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	c.Stream(func(w io.Writer) bool {
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
				c.Writer.Flush()
			}
			if err != nil {
				return false
			}
		}
	})
}

// returnResponse returns the full response to the client
func (h *Sub2APIProxyHandler) returnResponse(c *gin.Context, resp *http.Response) {
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
}

// Messages handles native Anthropic API /v1/messages requests using OAuth tokens
func (h *Sub2APIProxyHandler) Messages(c *gin.Context) {
	var req AnthropicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messages cannot be empty"})
		return
	}

	ctx := c.Request.Context()

	// Select account with retry logic (sub2api style)
	maxRetries := 3
	var excludedAccountIDs []string

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Get schedulable accounts
		accounts, err := h.store.GetSchedulableAccounts()
		if err != nil {
			log.Error().Err(err).Msg("failed to get schedulable accounts")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query accounts"})
			return
		}

		// Filter out excluded accounts
		var availableAccounts []*store.Account
		for _, acc := range accounts {
			excluded := false
			for _, exID := range excludedAccountIDs {
				if acc.ID == exID {
					excluded = true
					break
				}
			}
			if !excluded && acc.IsSchedulable() {
				availableAccounts = append(availableAccounts, acc)
			}
		}

		if len(availableAccounts) == 0 {
			log.Warn().
				Int("attempt", attempt+1).
				Int("excluded", len(excludedAccountIDs)).
				Msg("no schedulable accounts available")
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":   "no available accounts",
				"details": fmt.Sprintf("excluded=%d, attempt=%d/%d", len(excludedAccountIDs), attempt+1, maxRetries),
			})
			return
		}

		// Select best account (lowest priority, least recently used)
		account := selectBestAccount(availableAccounts)

		log.Info().
			Str("account_id", account.ID).
			Str("account_name", account.Name).
			Int("attempt", attempt+1).
			Int("available", len(availableAccounts)).
			Msg("selected account for /v1/messages request")

		// Execute request using Anthropic API
		resp, err := h.executeAnthropicAPIRequest(ctx, account, &req, c)

		// Handle errors
		if err != nil {
			log.Error().
				Err(err).
				Str("account_id", account.ID).
				Int("attempt", attempt+1).
				Msg("Anthropic API request execution failed")

			// Classify error and update account status
			shouldSwitch := h.errorClassifier.ClassifyAndHandleError(nil, account.ID)

			if shouldSwitch && attempt < maxRetries-1 {
				excludedAccountIDs = append(excludedAccountIDs, account.ID)
				log.Info().Str("account_id", account.ID).Msg("switching to next account")
				continue
			}

			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}

		// Check response status
		if resp.StatusCode >= 400 {
			// Error response, classify and handle
			shouldSwitch := h.errorClassifier.ClassifyAndHandleError(resp, account.ID)

			// For error responses, read body and return
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			// If should switch and we have retries left, try next account
			if shouldSwitch && attempt < maxRetries-1 && (resp.StatusCode == 429 || resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 503) {
				excludedAccountIDs = append(excludedAccountIDs, account.ID)
				log.Info().
					Str("account_id", account.ID).
					Int("status_code", resp.StatusCode).
					Msg("switching account due to error")
				continue
			}

			// Return error to client
			c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
			return
		}

		// Success! Record it
		h.errorClassifier.RecordSuccess(account.ID)
		go h.store.UpdateAccountLastUsed(account.ID)

		// Stream response directly (Anthropic native format)
		if req.Stream {
			h.streamAnthropicResponse(c, resp)
		} else {
			h.returnAnthropicResponse(c, resp)
		}
		return
	}

	// All retries exhausted
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error": "all retry attempts exhausted",
	})
}

// executeAnthropicAPIRequest executes a request to Anthropic API using OAuth token
func (h *Sub2APIProxyHandler) executeAnthropicAPIRequest(ctx context.Context, account *store.Account, req *AnthropicRequest, c *gin.Context) (*http.Response, error) {
	// Get valid access token for OAuth accounts (auto-refresh if needed)
	var accessToken string
	if account.IsOAuth() {
		var err error
		accessToken, err = h.getValidAccessToken(account)
		if err != nil {
			return nil, fmt.Errorf("failed to get valid access token: %w", err)
		}
	} else {
		return nil, fmt.Errorf("only OAuth accounts are supported for /v1/messages")
	}

	// Marshal request body
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request to Anthropic API
	apiURL := "https://api.anthropic.com/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication header
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	// Set anthropic-beta header (matches sub2api's getBetaHeader logic)
	betaHeader := c.GetHeader("anthropic-beta")
	if betaHeader == "" {
		// Determine beta header based on model
		if strings.Contains(strings.ToLower(req.Model), "haiku") {
			// Haiku models don't need claude-code beta
			betaHeader = "oauth-2025-04-20,interleaved-thinking-2025-05-14"
		} else {
			// Default for non-Haiku models
			betaHeader = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14"
		}
	} else {
		// Client provided beta header - ensure oauth beta is included
		if !strings.Contains(betaHeader, "oauth-2025-04-20") {
			if strings.Contains(betaHeader, "claude-code-20250219") {
				betaHeader = strings.Replace(betaHeader, "claude-code-20250219", "claude-code-20250219,oauth-2025-04-20", 1)
			} else {
				betaHeader = "oauth-2025-04-20," + betaHeader
			}
		}
	}
	httpReq.Header.Set("anthropic-beta", betaHeader)

	// Add Claude Code client headers
	httpReq.Header.Set("User-Agent", "claude-cli/2.0.62 (external, cli)")
	httpReq.Header.Set("X-Stainless-Lang", "js")
	httpReq.Header.Set("X-Stainless-Package-Version", "0.52.0")
	httpReq.Header.Set("X-Stainless-OS", "Linux")
	httpReq.Header.Set("X-Stainless-Arch", "x64")
	httpReq.Header.Set("X-Stainless-Runtime", "node")
	httpReq.Header.Set("X-Stainless-Runtime-Version", "v22.14.0")
	httpReq.Header.Set("X-App", "cli")
	httpReq.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")

	// Execute request
	client := &http.Client{Timeout: 10 * time.Minute}
	return client.Do(httpReq)
}

// streamAnthropicResponse streams the Anthropic API response to client
func (h *Sub2APIProxyHandler) streamAnthropicResponse(c *gin.Context, resp *http.Response) {
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}

	c.Status(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}

// returnAnthropicResponse returns the full Anthropic API response to client
func (h *Sub2APIProxyHandler) returnAnthropicResponse(c *gin.Context, resp *http.Response) {
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
}

// CountTokens handles the count_tokens endpoint using Anthropic API
func (h *Sub2APIProxyHandler) CountTokens(c *gin.Context) {
	// Get schedulable accounts
	accounts, err := h.store.GetSchedulableAccounts()
	if err != nil {
		log.Error().Err(err).Msg("failed to get schedulable accounts")
		h.countTokensError(c, http.StatusInternalServerError, "api_error", "Failed to query accounts")
		return
	}

	if len(accounts) == 0 {
		h.countTokensError(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
		return
	}

	// Use first available account for token counting
	account := accounts[0]

	// Read request body
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.countTokensError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	if len(bodyBytes) == 0 {
		h.countTokensError(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	// Use Anthropic API endpoint (not web API)
	countURL := "https://api.anthropic.com/v1/messages/count_tokens?beta=true"

	// Create request
	req, err := http.NewRequestWithContext(c.Request.Context(), "POST", countURL, bytes.NewReader(bodyBytes))
	if err != nil {
		h.countTokensError(c, http.StatusInternalServerError, "api_error", "Failed to create request")
		return
	}

	// Set authentication header based on account type
	if account.IsOAuth() {
		// Get valid access token (auto-refresh if needed, matches sub2api's ClaudeTokenProvider)
		accessToken, err := h.getValidAccessToken(account)
		if err != nil {
			log.Error().Err(err).Str("account_id", account.ID).Msg("failed to get valid access token")
			h.countTokensError(c, http.StatusUnauthorized, "authentication_error", "Failed to get valid access token")
			return
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
	} else if account.Credentials.SessionKey != "" {
		// For sessionKey accounts, we can't use count_tokens API directly
		// Return a mock response
		c.JSON(http.StatusOK, gin.H{"input_tokens": 0})
		return
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	// Set anthropic-beta header for OAuth accounts (matches sub2api's getBetaHeader logic)
	betaHeader := c.GetHeader("anthropic-beta")
	if betaHeader == "" {
		// Extract model from request body to determine appropriate beta header
		var reqBody map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &reqBody); err == nil {
			if modelID, ok := reqBody["model"].(string); ok && strings.Contains(strings.ToLower(modelID), "haiku") {
				// Haiku models don't need claude-code beta (matches sub2api's HaikuBetaHeader)
				betaHeader = "oauth-2025-04-20,interleaved-thinking-2025-05-14"
			}
		}

		// Default for non-Haiku models (matches sub2api's DefaultBetaHeader)
		if betaHeader == "" {
			betaHeader = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14"
		}
	} else {
		// Client provided beta header - ensure oauth beta is included (matches sub2api's getBetaHeader)
		if !strings.Contains(betaHeader, "oauth-2025-04-20") {
			// Insert oauth beta after claude-code if present
			if strings.Contains(betaHeader, "claude-code-20250219") {
				betaHeader = strings.Replace(betaHeader, "claude-code-20250219", "claude-code-20250219,oauth-2025-04-20", 1)
			} else {
				// No claude-code, put oauth first
				betaHeader = "oauth-2025-04-20," + betaHeader
			}
		}
	}
	req.Header.Set("anthropic-beta", betaHeader)

	// Add Claude Code client headers (matches sub2api defaults)
	req.Header.Set("User-Agent", "claude-cli/2.0.62 (external, cli)")
	req.Header.Set("X-Stainless-Lang", "js")
	req.Header.Set("X-Stainless-Package-Version", "0.52.0")
	req.Header.Set("X-Stainless-OS", "Linux")
	req.Header.Set("X-Stainless-Arch", "x64")
	req.Header.Set("X-Stainless-Runtime", "node")
	req.Header.Set("X-Stainless-Runtime-Version", "v22.14.0")
	req.Header.Set("X-App", "cli")
	req.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	// Execute request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Error().Err(err).Str("account_id", account.ID).Msg("failed to count tokens")
		h.countTokensError(c, http.StatusBadGateway, "upstream_error", "Request failed")
		return
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.countTokensError(c, http.StatusBadGateway, "upstream_error", "Failed to read response")
		return
	}

	// Handle error responses
	if resp.StatusCode >= 400 {
		log.Error().
			Int("status", resp.StatusCode).
			Str("account_id", account.ID).
			Str("response", string(respBody)).
			Msg("count_tokens upstream error")

		// Return error in Anthropic API format
		var errMsg string
		switch resp.StatusCode {
		case 400:
			errMsg = "Invalid request"
		case 401:
			errMsg = "Authentication failed"
		case 403:
			errMsg = "Access forbidden"
		case 429:
			errMsg = "Rate limit exceeded"
		case 529:
			errMsg = "Service overloaded"
		default:
			errMsg = "Upstream request failed"
		}
		h.countTokensError(c, resp.StatusCode, "upstream_error", errMsg)
		return
	}

	// Return successful response
	c.Data(resp.StatusCode, "application/json", respBody)
}

// countTokensError returns count_tokens error in Anthropic API format
func (h *Sub2APIProxyHandler) countTokensError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}
