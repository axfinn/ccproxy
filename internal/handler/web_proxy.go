package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/imroc/req/v3"
	"github.com/rs/zerolog/log"

	"ccproxy/internal/httpclient"
	"ccproxy/internal/store"
)

type WebProxyHandler struct {
	store     *store.Store
	webURL    string
	reqClient *req.Client
}

func NewWebProxyHandler(store *store.Store, webURL string) *WebProxyHandler {
	return &WebProxyHandler{
		store:     store,
		webURL:    webURL,
		reqClient: httpclient.GetClient(),
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

	r := h.reqClient.R().
		SetContext(c.Request.Context()).
		SetBodyBytes(payloadBytes)
	h.setReqHeaders(r, account)
	r.SetHeader("Content-Type", "application/json")

	resp, err := r.Post(url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to claude.ai"})
		return
	}

	// Update account last used
	go h.store.UpdateAccountLastUsed(account.ID)

	c.Data(resp.StatusCode, resp.GetHeader("Content-Type"), resp.Bytes())
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

	r := h.reqClient.R().
		SetContext(c.Request.Context()).
		SetBodyBytes(payloadBytes)
	h.setReqHeaders(r, account)
	r.SetHeader("Content-Type", "application/json")
	r.SetHeader("Accept", "text/event-stream")

	// Enable streaming response
	r.DisableAutoReadResponse()

	resp, err := r.Post(url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to claude.ai"})
		return
	}
	defer resp.Body.Close()

	// Update account last used
	go h.store.UpdateAccountLastUsed(account.ID)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, resp.GetHeader("Content-Type"), body)
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

	r := h.reqClient.R().SetContext(c.Request.Context())
	h.setReqHeaders(r, account)

	resp, err := r.Get(url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to claude.ai"})
		return
	}

	// Update account last used
	go h.store.UpdateAccountLastUsed(account.ID)

	c.Data(resp.StatusCode, resp.GetHeader("Content-Type"), resp.Bytes())
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

	r := h.reqClient.R().SetContext(c.Request.Context())
	h.setReqHeaders(r, account)

	resp, err := r.Get(url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to claude.ai"})
		return
	}

	// Update account last used
	go h.store.UpdateAccountLastUsed(account.ID)

	c.Data(resp.StatusCode, resp.GetHeader("Content-Type"), resp.Bytes())
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

	r := h.reqClient.R().SetContext(c.Request.Context())
	h.setReqHeaders(r, account)

	resp, err := r.Delete(url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to claude.ai"})
		return
	}

	// Update account last used
	go h.store.UpdateAccountLastUsed(account.ID)

	c.Data(resp.StatusCode, resp.GetHeader("Content-Type"), resp.Bytes())
}

func (h *WebProxyHandler) setReqHeaders(r *req.Request, account *store.Account) {
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

	r := h.reqClient.R().
		SetContext(c.Request.Context()).
		SetBodyBytes(bodyBytes)

	// Copy headers (skip Host and Cookie)
	for key, values := range c.Request.Header {
		lowerKey := strings.ToLower(key)
		if lowerKey == "host" || lowerKey == "cookie" {
			continue
		}
		for _, value := range values {
			r.SetHeader(key, value)
		}
	}

	h.setReqHeaders(r, account)

	// Enable streaming for response
	r.DisableAutoReadResponse()

	var resp *req.Response
	switch c.Request.Method {
	case "GET":
		resp, err = r.Get(targetURL)
	case "POST":
		resp, err = r.Post(targetURL)
	case "PUT":
		resp, err = r.Put(targetURL)
	case "DELETE":
		resp, err = r.Delete(targetURL)
	case "PATCH":
		resp, err = r.Patch(targetURL)
	default:
		resp, err = r.Send(c.Request.Method, targetURL)
	}

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
