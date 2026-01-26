package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"ccproxy/internal/store"
)

// Sub2APIProxyHandler handles proxy requests with sub2api-style account selection
type Sub2APIProxyHandler struct {
	store           *store.Store
	webURL          string
	errorClassifier *ErrorClassifier
}

// NewSub2APIProxyHandler creates a new sub2api-style proxy handler
func NewSub2APIProxyHandler(st *store.Store, webURL string) *Sub2APIProxyHandler {
	return &Sub2APIProxyHandler{
		store:           st,
		webURL:          webURL,
		errorClassifier: NewErrorClassifier(st),
	}
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
	setWebHeaders(createReq, account, h.webURL)
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
	setWebHeaders(msgReq, account, h.webURL)
	msgReq.Header.Set("Content-Type", "application/json")
	msgReq.Header.Set("Accept", "text/event-stream")

	msgResp, err := client.Do(msgReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	return msgResp, nil
}

// setWebHeaders sets request headers for claude.ai requests
func setWebHeaders(r *http.Request, account *store.Account, webURL string) {
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

	// Set authentication
	if account.IsOAuth() {
		r.Header.Set("Authorization", "Bearer "+account.Credentials.AccessToken)
		r.Header.Set("anthropic-beta", "oauth-2025-04-20")
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
