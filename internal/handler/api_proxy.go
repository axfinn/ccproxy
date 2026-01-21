package handler

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"ccproxy/internal/loadbalancer"
)

type APIProxyHandler struct {
	keyPool    *loadbalancer.KeyPool
	apiURL     string
	httpClient *http.Client
}

func NewAPIProxyHandler(keyPool *loadbalancer.KeyPool, apiURL string) *APIProxyHandler {
	return &APIProxyHandler{
		keyPool: keyPool,
		apiURL:  apiURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// Messages proxies the /v1/messages endpoint (Anthropic Messages API)
func (h *APIProxyHandler) Messages(c *gin.Context) {
	h.proxyRequest(c, "/v1/messages")
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

	// Create proxy request
	req, err := http.NewRequest(c.Request.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}

	// Copy headers (excluding auth-related ones)
	for key, values := range c.Request.Header {
		lowerKey := strings.ToLower(key)
		if lowerKey == "authorization" || lowerKey == "x-api-key" || lowerKey == "host" {
			continue
		}
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// Set Anthropic API headers
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// Execute request
	resp, err := h.httpClient.Do(req)
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
	contentType := resp.Header.Get("Content-Type")
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
