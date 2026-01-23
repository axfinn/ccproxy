package httpclient

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/imroc/req/v3"
)

var (
	defaultClient *req.Client
	once          sync.Once
)

// GetClient returns a shared HTTP client with Chrome TLS fingerprint
// This client bypasses Cloudflare detection by simulating Chrome browser
func GetClient() *req.Client {
	once.Do(func() {
		defaultClient = NewClient("")
	})
	return defaultClient
}

// NewClient creates a new HTTP client with Chrome TLS fingerprint
// proxyURL: optional proxy URL, if empty uses system proxy
func NewClient(proxyURL string) *req.Client {
	client := req.C().
		SetTimeout(10 * time.Minute). // Support slow models (Opus) and large documents
		ImpersonateChrome().          // Chrome TLS fingerprint to bypass Cloudflare
		SetCookieJar(nil)             // Don't persist cookies between requests

	// Use provided proxy or detect system proxy
	proxy := strings.TrimSpace(proxyURL)
	if proxy == "" {
		proxy = GetSystemProxy()
	}
	if proxy != "" {
		client.SetProxyURL(proxy)
	}

	return client
}

// GetSystemProxy returns the system proxy URL from environment variables
func GetSystemProxy() string {
	envVars := []string{
		"HTTPS_PROXY", "https_proxy",
		"HTTP_PROXY", "http_proxy",
		"ALL_PROXY", "all_proxy",
	}
	for _, env := range envVars {
		if proxy := os.Getenv(env); proxy != "" {
			return proxy
		}
	}
	return ""
}
