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
	"github.com/rs/zerolog/log"

	"ccproxy/internal/store"
)

type WebProxyHandler struct {
	store      *store.Store
	webURL     string
	httpClient *http.Client
}

func NewWebProxyHandler(store *store.Store, webURL string) *WebProxyHandler {
	return &WebProxyHandler{
		store:  store,
		webURL: webURL,
		httpClient: &http.Client{
			// 增加超时以支持长时间对话和大文档处理
			Timeout: 10 * time.Minute,
		},
	}
}

// Claude.ai conversation structures
type WebConversation struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type WebMessage struct {
	UUID    string `json:"uuid"`
	Text    string `json:"text"`
	Sender  string `json:"sender"` // "human" or "assistant"
	Index   int    `json:"index"`
}

type WebCompletionRequest struct {
	Prompt           string   `json:"prompt"`
	Timezone         string   `json:"timezone"`
	Attachments      []any    `json:"attachments"`
	Files            []any    `json:"files"`
}

// CreateConversation creates a new conversation on claude.ai
func (h *WebProxyHandler) CreateConversation(c *gin.Context) {
	account, err := h.store.GetActiveAccount()
	if err != nil || account == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no active account available"})
		return
	}

	var reqBody struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		reqBody.Name = ""
	}

	convUUID := uuid.New().String()
	payload := map[string]interface{}{
		"uuid": convUUID,
		"name": reqBody.Name,
	}
	payloadBytes, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/api/organizations/%s/chat_conversations", h.webURL, account.OrganizationID)
	req, err := http.NewRequestWithContext(c.Request.Context(), "POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}

	h.setWebHeaders(req, account)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to claude.ai"})
		return
	}
	defer resp.Body.Close()

	// Update account last used
	go h.store.UpdateAccountLastUsed(account.ID)

	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
}

// SendMessage sends a message to a conversation and streams the response
func (h *WebProxyHandler) SendMessage(c *gin.Context) {
	account, err := h.store.GetActiveAccount()
	if err != nil || account == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no active account available"})
		return
	}

	conversationID := c.Param("conversation_id")
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversation_id is required"})
		return
	}

	var reqBody WebCompletionRequest
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if reqBody.Timezone == "" {
		reqBody.Timezone = "UTC"
	}
	if reqBody.Attachments == nil {
		reqBody.Attachments = []any{}
	}
	if reqBody.Files == nil {
		reqBody.Files = []any{}
	}

	payloadBytes, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/api/organizations/%s/chat_conversations/%s/completion",
		h.webURL, account.OrganizationID, conversationID)
	req, err := http.NewRequestWithContext(c.Request.Context(), "POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}

	h.setWebHeaders(req, account)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to claude.ai"})
		return
	}
	defer resp.Body.Close()

	// Update account last used
	go h.store.UpdateAccountLastUsed(account.ID)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
		return
	}

	// Stream the SSE response
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")

	c.Stream(func(w io.Writer) bool {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintf(w, "%s\n", line)
			c.Writer.Flush()
		}
		return false
	})
}

// ListConversations lists all conversations
func (h *WebProxyHandler) ListConversations(c *gin.Context) {
	account, err := h.store.GetActiveAccount()
	if err != nil || account == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no active account available"})
		return
	}

	url := fmt.Sprintf("%s/api/organizations/%s/chat_conversations", h.webURL, account.OrganizationID)
	req, err := http.NewRequestWithContext(c.Request.Context(), "GET", url, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}

	h.setWebHeaders(req, account)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to claude.ai"})
		return
	}
	defer resp.Body.Close()

	// Update account last used
	go h.store.UpdateAccountLastUsed(account.ID)

	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
}

// GetConversation gets a specific conversation
func (h *WebProxyHandler) GetConversation(c *gin.Context) {
	account, err := h.store.GetActiveAccount()
	if err != nil || account == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no active account available"})
		return
	}

	conversationID := c.Param("conversation_id")
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversation_id is required"})
		return
	}

	url := fmt.Sprintf("%s/api/organizations/%s/chat_conversations/%s", h.webURL, account.OrganizationID, conversationID)
	req, err := http.NewRequestWithContext(c.Request.Context(), "GET", url, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}

	h.setWebHeaders(req, account)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to claude.ai"})
		return
	}
	defer resp.Body.Close()

	// Update account last used
	go h.store.UpdateAccountLastUsed(account.ID)

	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
}

// DeleteConversation deletes a conversation
func (h *WebProxyHandler) DeleteConversation(c *gin.Context) {
	account, err := h.store.GetActiveAccount()
	if err != nil || account == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no active account available"})
		return
	}

	conversationID := c.Param("conversation_id")
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversation_id is required"})
		return
	}

	url := fmt.Sprintf("%s/api/organizations/%s/chat_conversations/%s", h.webURL, account.OrganizationID, conversationID)
	req, err := http.NewRequestWithContext(c.Request.Context(), "DELETE", url, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}

	h.setWebHeaders(req, account)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to claude.ai"})
		return
	}
	defer resp.Body.Close()

	// Update account last used
	go h.store.UpdateAccountLastUsed(account.ID)

	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
}

func (h *WebProxyHandler) setWebHeaders(req *http.Request, account *store.Account) {
	// 使用最新的 Chrome User-Agent (2026)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	// 现代浏览器的 Client Hints
	req.Header.Set("Sec-Ch-Ua", `"Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"macOS"`)

	// 安全相关头
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")

	// 标准请求头
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7")
	// Don't request compression for SSE streams - we need to parse line by line
	// req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")

	// Origin 和 Referer
	req.Header.Set("Origin", h.webURL)
	req.Header.Set("Referer", h.webURL+"/")

	// Set authentication based on account type
	if account.IsOAuth() {
		// OAuth accounts use Bearer token
		req.Header.Set("Authorization", "Bearer "+account.Credentials.AccessToken)
		// Add OAuth beta flag if available
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	} else {
		// Session key accounts use Cookie
		req.Header.Set("Cookie", fmt.Sprintf("sessionKey=%s", account.Credentials.SessionKey))
	}
}

// ProxyGeneric proxies any request to claude.ai (for unsupported endpoints)
func (h *WebProxyHandler) ProxyGeneric(c *gin.Context) {
	account, err := h.store.GetActiveAccount()
	if err != nil || account == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no active account available"})
		return
	}

	// Build target URL
	path := c.Param("path")
	if path == "" {
		path = c.Request.URL.Path
	}

	targetURL := h.webURL + path
	if c.Request.URL.RawQuery != "" {
		targetURL += "?" + c.Request.URL.RawQuery
	}

	// Read request body
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	// Create proxy request
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}

	// Copy headers
	for key, values := range c.Request.Header {
		for _, value := range values {
			// Skip some headers that should be set by us
			if strings.ToLower(key) == "host" || strings.ToLower(key) == "cookie" {
				continue
			}
			req.Header.Add(key, value)
		}
	}

	h.setWebHeaders(req, account)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Error().Err(err).Str("url", targetURL).Msg("failed to proxy request")
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to claude.ai"})
		return
	}
	defer resp.Body.Close()

	// Update account last used
	go h.store.UpdateAccountLastUsed(account.ID)

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}

	// Stream response
	c.Status(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}
