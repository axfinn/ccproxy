package service

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	"ccproxy/internal/store"
)

type OAuthService struct {
	webURL     string
	apiURL     string
	httpClient *http.Client
	store      *store.Store
}

func NewOAuthService(webURL, apiURL string, s *store.Store) *OAuthService {
	return &OAuthService{
		webURL: webURL,
		apiURL: apiURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		store: s,
	}
}

// LoginRequest represents the OAuth login request
type LoginRequest struct {
	SessionKey string `json:"session_key"`
	Name       string `json:"name"`
}

// LoginResult represents the OAuth login result
type LoginResult struct {
	AccountID      string    `json:"account_id"`
	OrganizationID string    `json:"organization_id"`
	AccessToken    string    `json:"access_token"`
	RefreshToken   string    `json:"refresh_token"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// Step 1: Get Organization UUID
func (s *OAuthService) getOrganizationUUID(sessionKey string) (string, error) {
	url := s.webURL + "/api/organizations"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	s.setWebHeaders(req, sessionKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get organizations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to get organizations: status %d, body: %s", resp.StatusCode, body)
	}

	var orgs []struct {
		UUID string `json:"uuid"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return "", fmt.Errorf("failed to decode organizations: %w", err)
	}

	if len(orgs) == 0 {
		return "", fmt.Errorf("no organizations found")
	}

	return orgs[0].UUID, nil
}

// Step 2: Get Authorization Code with PKCE
func (s *OAuthService) getAuthorizationCode(sessionKey, orgUUID string) (string, string, error) {
	// Generate PKCE verifier and challenge
	verifier := generateCodeVerifier()
	challenge := generateCodeChallenge(verifier)

	url := fmt.Sprintf("%s/v1/oauth/%s/authorize", s.webURL, orgUUID)
	payload := map[string]interface{}{
		"response_type":         "code",
		"client_id":             "claude-web-oauth-pkce",
		"code_challenge":        challenge,
		"code_challenge_method": "S256",
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}

	s.setWebHeaders(req, sessionKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("failed to get authorization code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("failed to get authorization code: status %d, body: %s", resp.StatusCode, body)
	}

	var result struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("failed to decode authorization code: %w", err)
	}

	return result.Code, verifier, nil
}

// Step 3: Exchange code for access token
func (s *OAuthService) exchangeToken(code, verifier string) (*LoginResult, error) {
	url := s.apiURL + "/v1/oauth/token"
	payload := map[string]interface{}{
		"grant_type":    "authorization_code",
		"code":          code,
		"code_verifier": verifier,
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to exchange token: status %d, body: %s", resp.StatusCode, body)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return &LoginResult{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// Login performs the complete OAuth login flow
func (s *OAuthService) Login(req LoginRequest) (*LoginResult, error) {
	log.Info().Str("name", req.Name).Msg("starting OAuth login")

	// Step 1: Get organization UUID
	orgUUID, err := s.getOrganizationUUID(req.SessionKey)
	if err != nil {
		return nil, fmt.Errorf("step 1 failed: %w", err)
	}
	log.Info().Str("org_uuid", orgUUID).Msg("got organization UUID")

	// Step 2: Get authorization code
	code, verifier, err := s.getAuthorizationCode(req.SessionKey, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("step 2 failed: %w", err)
	}
	log.Info().Msg("got authorization code")

	// Step 3: Exchange for access token
	result, err := s.exchangeToken(code, verifier)
	if err != nil {
		return nil, fmt.Errorf("step 3 failed: %w", err)
	}
	log.Info().Time("expires_at", result.ExpiresAt).Msg("got access token")

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

// RefreshToken refreshes an expired OAuth token
func (s *OAuthService) RefreshToken(account *store.Account) error {
	if !account.IsOAuth() {
		return fmt.Errorf("account is not an OAuth account")
	}

	url := s.apiURL + "/v1/oauth/token"
	payload := map[string]interface{}{
		"grant_type":    "refresh_token",
		"refresh_token": account.Credentials.RefreshToken,
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to refresh token: status %d, body: %s", resp.StatusCode, body)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
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

// CheckHealth performs a health check on the account
func (s *OAuthService) CheckHealth(account *store.Account) error {
	// Try a simple API call to check if the token is valid
	url := s.apiURL + "/v1/messages"
	payload := map[string]interface{}{
		"model":      "claude-3-5-haiku-20241022",
		"max_tokens": 10,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if account.IsOAuth() {
		req.Header.Set("Authorization", "Bearer "+account.Credentials.AccessToken)
	} else {
		req.Header.Set("x-api-key", account.Credentials.APIKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.store.UpdateAccountHealth(account.ID, "unhealthy")
		s.store.IncrementAccountError(account.ID)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		s.store.UpdateAccountHealth(account.ID, "healthy")
		s.store.IncrementAccountSuccess(account.ID)
		return nil
	}

	// Check if token needs refresh
	if resp.StatusCode == 401 && account.IsOAuth() {
		log.Info().Str("account_id", account.ID).Msg("token expired, attempting refresh")
		if err := s.RefreshToken(account); err != nil {
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

func (s *OAuthService) setWebHeaders(req *http.Request, sessionKey string) {
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
	req.Header.Set("Origin", s.webURL)
	req.Header.Set("Referer", s.webURL+"/")
	req.Header.Set("Cookie", fmt.Sprintf("sessionKey=%s", sessionKey))
}

// PKCE helpers

func generateCodeVerifier() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func generateAccountID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("acc_%x", b)
}
