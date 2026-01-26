package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/imroc/req/v3"
	"github.com/rs/zerolog/log"

	"ccproxy/internal/store"
)

// OAuth Constants (from sub2api)
const (
	OAuthClientID    = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	OAuthTokenURL    = "https://platform.claude.com/v1/oauth/token"
	OAuthRedirectURI = "https://platform.claude.com/oauth/code/callback"
	// OAuthScope - Internal API call (org:create_api_key not supported in API, matches sub2api ScopeAPI)
	OAuthScope = "user:profile user:inference user:sessions:claude_code"

	// Code Verifier character set (RFC 7636 compliant)
	codeVerifierCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
)

type OAuthService struct {
	webURL string
	apiURL string
	store  *store.Store
}

func NewOAuthService(webURL, apiURL string, s *store.Store) *OAuthService {
	return &OAuthService{
		webURL: webURL,
		apiURL: apiURL,
		store:  s,
	}
}

// LoginRequest represents the OAuth login request
type LoginRequest struct {
	SessionKey string `json:"session_key"`
	Name       string `json:"name"`
	ProxyURL   string `json:"proxy_url,omitempty"` // Optional proxy URL
}

// LoginResult represents the OAuth login result
type LoginResult struct {
	AccountID      string    `json:"account_id"`
	OrganizationID string    `json:"organization_id"`
	AccessToken    string    `json:"access_token"`
	RefreshToken   string    `json:"refresh_token"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// createReqClient creates a req client with Chrome impersonation
func createReqClient(proxyURL string) *req.Client {
	client := req.C().
		SetTimeout(60 * time.Second).
		ImpersonateChrome().
		SetCookieJar(nil) // Disable CookieJar for clean sessions

	// Use provided proxy or detect from environment
	proxy := strings.TrimSpace(proxyURL)
	if proxy == "" {
		proxy = getSystemProxy()
	}
	if proxy != "" {
		log.Debug().Str("proxy", proxy).Msg("Using proxy for OAuth request")
		client.SetProxyURL(proxy)
	}

	return client
}

// getSystemProxy detects proxy from environment variables
func getSystemProxy() string {
	// Check common proxy environment variables
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

// Step 1: Get Organization UUID
func (s *OAuthService) getOrganizationUUID(sessionKey, proxyURL string) (string, error) {
	client := createReqClient(proxyURL)

	var orgs []struct {
		UUID string `json:"uuid"`
	}

	targetURL := s.webURL + "/api/organizations"
	log.Info().Str("url", targetURL).Msg("[OAuth] Step 1: Getting organization UUID")

	resp, err := client.R().
		SetContext(context.Background()).
		SetCookies(&http.Cookie{
			Name:  "sessionKey",
			Value: sessionKey,
		}).
		SetSuccessResult(&orgs).
		Get(targetURL)

	if err != nil {
		log.Error().Err(err).Msg("[OAuth] Step 1 FAILED - Request error")
		return "", fmt.Errorf("request failed: %w", err)
	}

	log.Info().Int("status", resp.StatusCode).Msg("[OAuth] Step 1 Response")

	if !resp.IsSuccessState() {
		body := resp.String()
		// Check if it's a Cloudflare challenge
		if strings.Contains(body, "Just a moment") || strings.Contains(body, "cloudflare") {
			log.Error().Msg("[OAuth] Cloudflare challenge detected - try using a proxy_url or check if sessionKey is valid")
			return "", fmt.Errorf("blocked by Cloudflare - use proxy_url parameter or verify sessionKey is fresh from browser")
		}
		return "", fmt.Errorf("failed to get organizations: status %d, body: %s", resp.StatusCode, resp.String())
	}

	if len(orgs) == 0 {
		return "", fmt.Errorf("no organizations found")
	}

	log.Info().Str("org_uuid", orgs[0].UUID).Msg("[OAuth] Step 1 SUCCESS")
	return orgs[0].UUID, nil
}

// Step 2: Get Authorization Code with PKCE
func (s *OAuthService) getAuthorizationCode(sessionKey, orgUUID, proxyURL string) (string, string, string, error) {
	client := createReqClient(proxyURL)

	// Generate PKCE verifier and challenge
	verifier, err := generateCodeVerifier()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate code verifier: %w", err)
	}
	challenge := generateCodeChallenge(verifier)

	// Generate state
	state, err := generateState()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate state: %w", err)
	}

	authURL := fmt.Sprintf("%s/v1/oauth/%s/authorize", s.webURL, orgUUID)

	reqBody := map[string]interface{}{
		"response_type":         "code",
		"client_id":             OAuthClientID,
		"organization_uuid":     orgUUID,
		"redirect_uri":          OAuthRedirectURI,
		"scope":                 OAuthScope,
		"state":                 state,
		"code_challenge":        challenge,
		"code_challenge_method": "S256",
	}

	log.Info().Str("url", authURL).Msg("[OAuth] Step 2: Getting authorization code")

	var result struct {
		RedirectURI string `json:"redirect_uri"`
	}

	resp, err := client.R().
		SetContext(context.Background()).
		SetCookies(&http.Cookie{
			Name:  "sessionKey",
			Value: sessionKey,
		}).
		SetHeader("Accept", "application/json").
		SetHeader("Accept-Language", "en-US,en;q=0.9").
		SetHeader("Cache-Control", "no-cache").
		SetHeader("Origin", "https://claude.ai").
		SetHeader("Referer", "https://claude.ai/new").
		SetHeader("Content-Type", "application/json").
		SetBody(reqBody).
		SetSuccessResult(&result).
		Post(authURL)

	if err != nil {
		log.Error().Err(err).Msg("[OAuth] Step 2 FAILED - Request error")
		return "", "", "", fmt.Errorf("request failed: %w", err)
	}

	log.Info().Int("status", resp.StatusCode).Msg("[OAuth] Step 2 Response")

	if !resp.IsSuccessState() {
		return "", "", "", fmt.Errorf("failed to get authorization code: status %d, body: %s", resp.StatusCode, resp.String())
	}

	if result.RedirectURI == "" {
		return "", "", "", fmt.Errorf("no redirect_uri in response")
	}

	// Parse redirect URI to extract code
	parsedURL, err := url.Parse(result.RedirectURI)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse redirect_uri: %w", err)
	}

	queryParams := parsedURL.Query()
	authCode := queryParams.Get("code")
	responseState := queryParams.Get("state")

	if authCode == "" {
		return "", "", "", fmt.Errorf("no authorization code in redirect_uri")
	}

	// Combine code with state if present
	fullCode := authCode
	if responseState != "" {
		fullCode = authCode + "#" + responseState
	}

	log.Info().Msg("[OAuth] Step 2 SUCCESS - Got authorization code")
	return fullCode, verifier, state, nil
}

// Step 3: Exchange code for access token
func (s *OAuthService) exchangeToken(code, verifier, state, proxyURL string) (*LoginResult, error) {
	client := createReqClient(proxyURL)

	// Parse code which may contain state in format "authCode#state"
	authCode := code
	codeState := ""
	if idx := strings.Index(code, "#"); idx != -1 {
		authCode = code[:idx]
		codeState = code[idx+1:]
	}

	reqBody := map[string]interface{}{
		"code":          authCode,
		"grant_type":    "authorization_code",
		"client_id":     OAuthClientID,
		"redirect_uri":  OAuthRedirectURI,
		"code_verifier": verifier,
	}

	if codeState != "" {
		reqBody["state"] = codeState
	}

	log.Info().Str("url", OAuthTokenURL).Msg("[OAuth] Step 3: Exchanging code for token")

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}

	resp, err := client.R().
		SetHeader("Accept", "application/json, text/plain, */*").
		SetHeader("Content-Type", "application/json").
		SetHeader("User-Agent", "axios/1.8.4").
		SetBody(reqBody).
		SetSuccessResult(&tokenResp).
		Post(OAuthTokenURL)

	if err != nil {
		log.Error().Err(err).Msg("[OAuth] Step 3 FAILED - Request error")
		return nil, fmt.Errorf("request failed: %w", err)
	}

	log.Info().Int("status", resp.StatusCode).Msg("[OAuth] Step 3 Response")

	if !resp.IsSuccessState() {
		return nil, fmt.Errorf("token exchange failed: status %d, body: %s", resp.StatusCode, resp.String())
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	log.Info().Time("expires_at", expiresAt).Msg("[OAuth] Step 3 SUCCESS - Got access token")

	return &LoginResult{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// Login performs the complete OAuth login flow
func (s *OAuthService) Login(req LoginRequest) (*LoginResult, error) {
	log.Info().Str("name", req.Name).Msg("starting OAuth login")

	proxyURL := req.ProxyURL

	// Step 1: Get organization UUID
	orgUUID, err := s.getOrganizationUUID(req.SessionKey, proxyURL)
	if err != nil {
		return nil, fmt.Errorf("step 1 failed: %w", err)
	}

	// Step 2: Get authorization code
	code, verifier, state, err := s.getAuthorizationCode(req.SessionKey, orgUUID, proxyURL)
	if err != nil {
		return nil, fmt.Errorf("step 2 failed: %w", err)
	}

	// Step 3: Exchange for access token
	result, err := s.exchangeToken(code, verifier, state, proxyURL)
	if err != nil {
		return nil, fmt.Errorf("step 3 failed: %w", err)
	}

	result.OrganizationID = orgUUID

	// Generate account ID
	accountID := generateAccountID()
	result.AccountID = accountID

	// Save to database
	account := &store.Account{
		ID:             accountID,
		Name:           req.Name,
		Type:           store.AccountTypeOAuth,
		OrganizationID: orgUUID,
		Credentials: store.Credentials{
			AccessToken:  result.AccessToken,
			RefreshToken: result.RefreshToken,
		},
		CreatedAt:    time.Now(),
		ExpiresAt:    &result.ExpiresAt,
		IsActive:     true,
		HealthStatus: "healthy",
	}

	if err := s.store.CreateAccount(account); err != nil {
		return nil, fmt.Errorf("failed to save account: %w", err)
	}

	log.Info().Str("account_id", accountID).Msg("OAuth login completed")
	return result, nil
}

// RefreshAccountToken refreshes an expired OAuth token for a specific account
func (s *OAuthService) RefreshAccountToken(account *store.Account) error {
	if !account.IsOAuth() {
		return fmt.Errorf("account is not an OAuth account")
	}

	client := createReqClient("")

	reqBody := map[string]interface{}{
		"grant_type":    "refresh_token",
		"refresh_token": account.Credentials.RefreshToken,
		"client_id":     OAuthClientID,
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}

	resp, err := client.R().
		SetHeader("Accept", "application/json, text/plain, */*").
		SetHeader("Content-Type", "application/json").
		SetHeader("User-Agent", "axios/1.8.4").
		SetBody(reqBody).
		SetSuccessResult(&tokenResp).
		Post(OAuthTokenURL)

	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	if !resp.IsSuccessState() {
		return fmt.Errorf("token refresh failed: status %d, body: %s", resp.StatusCode, resp.String())
	}

	// Update account credentials
	account.Credentials.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		account.Credentials.RefreshToken = tokenResp.RefreshToken
	}
	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	account.ExpiresAt = &expiresAt

	if err := s.store.UpdateAccount(account); err != nil {
		return fmt.Errorf("failed to update account: %w", err)
	}

	log.Info().Str("account_id", account.ID).Time("expires_at", expiresAt).Msg("token refreshed successfully")
	return nil
}

// RefreshToken refreshes an OAuth token by account ID (implements health.TokenRefresher)
func (s *OAuthService) RefreshToken(ctx context.Context, accountID string) error {
	account, err := s.store.GetAccount(accountID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}
	if account == nil {
		return fmt.Errorf("account not found: %s", accountID)
	}
	return s.RefreshAccountToken(account)
}

// NeedsRefresh returns true if the account needs token refresh (implements health.TokenRefresher)
func (s *OAuthService) NeedsRefresh(account *store.Account) bool {
	return account.NeedsRefresh()
}

// CheckHealth performs a health check on the account
func (s *OAuthService) CheckHealth(account *store.Account) error {
	client := createReqClient("")

	// Try a simple API call to check if the token is valid
	payload := map[string]interface{}{
		"model":      "claude-3-5-haiku-20241022",
		"max_tokens": 10,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}

	req := client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("anthropic-version", "2023-06-01").
		SetBody(payload)

	if account.IsOAuth() {
		req.SetHeader("Authorization", "Bearer "+account.Credentials.AccessToken)
	} else {
		req.SetHeader("x-api-key", account.Credentials.APIKey)
	}

	resp, err := req.Post(s.apiURL + "/v1/messages")
	if err != nil {
		s.store.UpdateAccountHealth(account.ID, "unhealthy")
		s.store.IncrementAccountError(account.ID)
		return err
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		s.store.UpdateAccountHealth(account.ID, "healthy")
		s.store.IncrementAccountSuccess(account.ID)
		return nil
	}

	// Check if token needs refresh
	if resp.StatusCode == 401 && account.IsOAuth() {
		log.Info().Str("account_id", account.ID).Msg("token expired, attempting refresh")
		if err := s.RefreshAccountToken(account); err != nil {
			s.store.UpdateAccountHealth(account.ID, "unhealthy")
			return fmt.Errorf("token refresh failed: %w", err)
		}
		s.store.UpdateAccountHealth(account.ID, "healthy")
		return nil
	}

	s.store.UpdateAccountHealth(account.ID, "unhealthy")
	s.store.IncrementAccountError(account.ID)
	return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
}

// PKCE helpers

// generateCodeVerifier generates a PKCE code verifier using character set method
func generateCodeVerifier() (string, error) {
	const targetLen = 32
	charsetLen := len(codeVerifierCharset)
	limit := 256 - (256 % charsetLen)

	result := make([]byte, 0, targetLen)
	randBuf := make([]byte, targetLen*2)

	for len(result) < targetLen {
		if _, err := rand.Read(randBuf); err != nil {
			return "", err
		}
		for _, b := range randBuf {
			if int(b) < limit {
				result = append(result, codeVerifierCharset[int(b)%charsetLen])
				if len(result) >= targetLen {
					break
				}
			}
		}
	}

	return base64URLEncode(result), nil
}

// generateCodeChallenge generates a PKCE code challenge using S256 method
func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64URLEncode(hash[:])
}

// generateState generates a random state string for OAuth
func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64URLEncode(b), nil
}

// base64URLEncode encodes bytes to base64url without padding
func base64URLEncode(data []byte) string {
	encoded := base64.URLEncoding.EncodeToString(data)
	return strings.TrimRight(encoded, "=")
}

func generateAccountID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("acc_%s", hex.EncodeToString(b))
}
