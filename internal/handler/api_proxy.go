package handler

import (
	"bufio"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/imroc/req/v3"
	"github.com/rs/zerolog/log"

	"ccproxy/internal/httpclient"
	"ccproxy/internal/loadbalancer"
)

type APIProxyHandler struct {
	keyPool   *loadbalancer.KeyPool
	apiURL    string
	reqClient *req.Client
}

func NewAPIProxyHandler(keyPool *loadbalancer.KeyPool, apiURL string) *APIProxyHandler {
	return &APIProxyHandler{
		keyPool:   keyPool,
		apiURL:    apiURL,
		reqClient: httpclient.GetClient(),
	}
}

// Messages proxies the /v1/messages endpoint (Anthropic Messages API)
func (h *APIProxyHandler) Messages(c *gin.Context) {
	h.proxyRequest(c, "/v1/messages")
}

// CountTokens proxies the /v1/messages/count_tokens endpoint
func (h *APIProxyHandler) CountTokens(c *gin.Context) {
	h.proxyRequest(c, "/v1/messages/count_tokens")
}

// ProxyAny proxies any request to the Anthropic API
func (h *APIProxyHandler) ProxyAny(c *gin.Context) {
	path := c.Param("path")
	if path == "" {
		path = c.Request.URL.Path
	}
	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	h.proxyRequest(c, path)
}

func (h *APIProxyHandler) proxyRequest(c *gin.Context, path string) {
	apiKey := h.keyPool.Get()
	if apiKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no API keys available"})
		return
	}

	// Build target URL
	targetURL := h.apiURL + path
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

	// Set authentication header
	r.SetHeader("x-api-key", apiKey)

	// Whitelist headers to forward (similar to sub2api)
	allowedHeaders := map[string]bool{
		"accept":                      true,
		"accept-encoding":             true,
		"content-type":                true,
		"user-agent":                  true,
		"anthropic-beta":              true,
		"anthropic-version":           true,
		"x-stainless-lang":            true,
		"x-stainless-package-version": true,
		"x-stainless-os":              true,
		"x-stainless-arch":            true,
		"x-stainless-runtime":         true,
		"x-stainless-runtime-version": true,
	}

	// Copy allowed headers from client request
	for key, values := range c.Request.Header {
		lowerKey := strings.ToLower(key)
		if allowedHeaders[lowerKey] {
			for _, value := range values {
				r.SetHeader(key, value)
			}
		}
	}

	// Ensure required headers are set (matches sub2api's Claude Code defaults)
	if c.Request.Header.Get("Content-Type") == "" {
		r.SetHeader("Content-Type", "application/json")
	}
	if c.Request.Header.Get("anthropic-version") == "" {
		r.SetHeader("anthropic-version", "2023-06-01")
	}
	if c.Request.Header.Get("User-Agent") == "" {
		r.SetHeader("User-Agent", "claude-cli/2.0.62 (external, cli)")
	}
	if c.Request.Header.Get("Accept-Encoding") == "" {
		r.SetHeader("Accept-Encoding", "gzip, deflate, br")
	}
	// Set default anthropic-beta if not provided (for API-key accounts)
	// Matches sub2api's APIKeyBetaHeader constant
	if c.Request.Header.Get("anthropic-beta") == "" {
		r.SetHeader("anthropic-beta", "claude-code-20250219,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14")
	}

	// Enable streaming response
	r.DisableAutoReadResponse()

	// Execute request based on method
	var resp *req.Response
	switch c.Request.Method {
	case "POST":
		resp, err = r.Post(targetURL)
	case "GET":
		resp, err = r.Get(targetURL)
	case "PUT":
		resp, err = r.Put(targetURL)
	case "DELETE":
		resp, err = r.Delete(targetURL)
	default:
		resp, err = r.Send(c.Request.Method, targetURL)
	}

	if err != nil {
		h.keyPool.ReportError(apiKey)
		log.Error().Err(err).Str("url", targetURL).Msg("failed to proxy request")
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to connect to Anthropic API"})
		return
	}
	defer resp.Body.Close()

	// Report success/failure to key pool
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		h.keyPool.ReportSuccess(apiKey)
	} else if resp.StatusCode == 401 || resp.StatusCode == 403 {
		h.keyPool.ReportError(apiKey)
	}

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}

	// Check if it's a streaming response
	contentType := resp.GetHeader("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Status(resp.StatusCode)

		// Stream SSE response
		c.Stream(func(w io.Writer) bool {
			scanner := bufio.NewScanner(resp.Body)
			// Increase buffer size for large chunks
			buf := make([]byte, 0, 64*1024)
			scanner.Buffer(buf, 1024*1024)

			for scanner.Scan() {
				line := scanner.Text()
				w.Write([]byte(line + "\n"))
				c.Writer.Flush()
			}
			return false
		})
	} else {
		// Non-streaming response
		c.Status(resp.StatusCode)
		io.Copy(c.Writer, resp.Body)
	}
}

// GetKeyStats returns statistics about API key usage
func (h *APIProxyHandler) GetKeyStats(c *gin.Context) {
	stats := h.keyPool.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"total_keys":   h.keyPool.Size(),
		"healthy_keys": h.keyPool.HealthyCount(),
		"keys":         stats,
	})
}
