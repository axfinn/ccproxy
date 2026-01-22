package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"ccproxy/internal/circuit"
	"ccproxy/internal/store"
)

// HealthConfig holds health monitor configuration
type HealthConfig struct {
	Enabled            bool          `mapstructure:"enabled"`
	CheckInterval      time.Duration `mapstructure:"check_interval"`
	TokenRefreshBefore time.Duration `mapstructure:"token_refresh_before"`
	Timeout            time.Duration `mapstructure:"timeout"`
}

// DefaultHealthConfig returns the default health configuration
func DefaultHealthConfig() HealthConfig {
	return HealthConfig{
		Enabled:            true,
		CheckInterval:      5 * time.Minute,
		TokenRefreshBefore: 30 * time.Minute,
		Timeout:            30 * time.Second,
	}
}

// CheckResult contains the result of a health check
type CheckResult struct {
	AccountID string        `json:"account_id"`
	Healthy   bool          `json:"healthy"`
	Latency   time.Duration `json:"latency"`
	Error     string        `json:"error,omitempty"`
	CheckedAt time.Time     `json:"checked_at"`
}

// Monitor monitors account health
type Monitor interface {
	// Start starts the health monitor
	Start(ctx context.Context) error
	// Stop stops the health monitor
	Stop()
	// CheckAccount performs a health check on an account
	CheckAccount(ctx context.Context, accountID string) (*CheckResult, error)
	// CheckAll performs health checks on all accounts
	CheckAll(ctx context.Context) ([]*CheckResult, error)
	// Stats returns monitor statistics
	Stats() MonitorStats
}

// MonitorStats contains monitor statistics
type MonitorStats struct {
	TotalChecks     int64 `json:"total_checks"`
	HealthyAccounts int   `json:"healthy_accounts"`
	UnhealthyAccounts int `json:"unhealthy_accounts"`
	LastCheckAt     time.Time `json:"last_check_at,omitempty"`
}

// AccountChecker defines the interface for checking account health
type AccountChecker interface {
	// Check performs a health check on an account
	Check(ctx context.Context, account *store.Account) error
}

// TokenRefresher defines the interface for refreshing OAuth tokens
type TokenRefresher interface {
	// RefreshToken refreshes the OAuth token for an account
	RefreshToken(ctx context.Context, accountID string) error
	// NeedsRefresh returns true if the account needs token refresh
	NeedsRefresh(account *store.Account) bool
}

// monitor implements Monitor
type monitor struct {
	config     HealthConfig
	store      *store.Store
	circuitMgr circuit.Manager
	checker    AccountChecker
	refresher  TokenRefresher
	httpClient *http.Client

	totalChecks       int64
	healthyAccounts   map[string]bool
	lastCheckAt       time.Time
	mu                sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewMonitor creates a new health monitor
func NewMonitor(config HealthConfig, st *store.Store, circuitMgr circuit.Manager, refresher TokenRefresher) Monitor {
	return &monitor{
		config:          config,
		store:           st,
		circuitMgr:      circuitMgr,
		refresher:       refresher,
		healthyAccounts: make(map[string]bool),
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

// Start starts the health monitor
func (m *monitor) Start(ctx context.Context) error {
	if !m.config.Enabled {
		log.Info().Msg("health monitor disabled")
		return nil
	}

	m.ctx, m.cancel = context.WithCancel(ctx)

	// Start background check goroutine
	m.wg.Add(1)
	go m.backgroundCheck()

	// Start token refresh goroutine
	m.wg.Add(1)
	go m.backgroundRefresh()

	log.Info().
		Dur("check_interval", m.config.CheckInterval).
		Dur("refresh_before", m.config.TokenRefreshBefore).
		Msg("health monitor started")

	return nil
}

// Stop stops the health monitor
func (m *monitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	log.Info().Msg("health monitor stopped")
}

// CheckAccount performs a health check on an account
func (m *monitor) CheckAccount(ctx context.Context, accountID string) (*CheckResult, error) {
	account, err := m.store.GetAccount(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}
	if account == nil {
		return nil, fmt.Errorf("account not found: %s", accountID)
	}

	return m.checkAccountHealth(ctx, account), nil
}

// CheckAll performs health checks on all accounts
func (m *monitor) CheckAll(ctx context.Context) ([]*CheckResult, error) {
	accounts, err := m.store.ListAccounts()
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	results := make([]*CheckResult, 0, len(accounts))
	for _, account := range accounts {
		if !account.IsActive {
			continue
		}

		result := m.checkAccountHealth(ctx, account)
		results = append(results, result)

		// Small delay between checks to avoid overwhelming services
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return results, ctx.Err()
		}
	}

	return results, nil
}

// Stats returns monitor statistics
func (m *monitor) Stats() MonitorStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	healthy := 0
	unhealthy := 0
	for _, isHealthy := range m.healthyAccounts {
		if isHealthy {
			healthy++
		} else {
			unhealthy++
		}
	}

	return MonitorStats{
		TotalChecks:       m.totalChecks,
		HealthyAccounts:   healthy,
		UnhealthyAccounts: unhealthy,
		LastCheckAt:       m.lastCheckAt,
	}
}

// checkAccountHealth performs the actual health check
func (m *monitor) checkAccountHealth(ctx context.Context, account *store.Account) *CheckResult {
	start := time.Now()
	result := &CheckResult{
		AccountID: account.ID,
		CheckedAt: start,
	}

	m.mu.Lock()
	m.totalChecks++
	m.mu.Unlock()

	// Check based on account type
	var err error
	switch account.Type {
	case store.AccountTypeOAuth:
		err = m.checkOAuthAccount(ctx, account)
	case store.AccountTypeSessionKey:
		err = m.checkSessionKeyAccount(ctx, account)
	case store.AccountTypeAPIKey:
		err = m.checkAPIKeyAccount(ctx, account)
	default:
		err = fmt.Errorf("unknown account type: %s", account.Type)
	}

	result.Latency = time.Since(start)
	result.Healthy = err == nil

	if err != nil {
		result.Error = err.Error()
		log.Warn().
			Str("account_id", account.ID).
			Err(err).
			Dur("latency", result.Latency).
			Msg("account health check failed")

		// Update circuit breaker
		if m.circuitMgr != nil {
			m.circuitMgr.RecordFailure(account.ID)
		}

		// Update store
		_ = m.store.UpdateAccountHealth(account.ID, "unhealthy")
		_ = m.store.IncrementAccountError(account.ID)
	} else {
		log.Debug().
			Str("account_id", account.ID).
			Dur("latency", result.Latency).
			Msg("account health check passed")

		// Update circuit breaker
		if m.circuitMgr != nil {
			m.circuitMgr.RecordSuccess(account.ID)
		}

		// Update store
		_ = m.store.UpdateAccountHealth(account.ID, "healthy")
		_ = m.store.IncrementAccountSuccess(account.ID)
	}

	// Update local cache
	m.mu.Lock()
	m.healthyAccounts[account.ID] = result.Healthy
	m.mu.Unlock()

	return result
}

// checkOAuthAccount checks an OAuth account
func (m *monitor) checkOAuthAccount(ctx context.Context, account *store.Account) error {
	if account.Credentials.AccessToken == "" {
		return fmt.Errorf("no access token")
	}

	// Check if token is expired
	if account.IsExpired() {
		return fmt.Errorf("access token expired")
	}

	// Make a simple API call to verify the token
	req, err := http.NewRequestWithContext(ctx, "GET", "https://claude.ai/api/organizations", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+account.Credentials.AccessToken)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("authentication failed: status %d", resp.StatusCode)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	return nil
}

// checkSessionKeyAccount checks a session key account
func (m *monitor) checkSessionKeyAccount(ctx context.Context, account *store.Account) error {
	if account.Credentials.SessionKey == "" {
		return fmt.Errorf("no session key")
	}

	// Make a simple API call to verify the session
	req, err := http.NewRequestWithContext(ctx, "GET", "https://claude.ai/api/organizations", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Cookie", fmt.Sprintf("sessionKey=%s", account.Credentials.SessionKey))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("authentication failed: status %d", resp.StatusCode)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	return nil
}

// checkAPIKeyAccount checks an API key account
func (m *monitor) checkAPIKeyAccount(ctx context.Context, account *store.Account) error {
	if account.Credentials.APIKey == "" {
		return fmt.Errorf("no API key")
	}

	// For API keys, we can't easily check without making a billable request
	// Just verify the key format
	if len(account.Credentials.APIKey) < 10 {
		return fmt.Errorf("invalid API key format")
	}

	return nil
}

// backgroundCheck runs periodic health checks
func (m *monitor) backgroundCheck() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			results, err := m.CheckAll(m.ctx)
			if err != nil {
				log.Error().Err(err).Msg("background health check failed")
				continue
			}

			healthy := 0
			for _, r := range results {
				if r.Healthy {
					healthy++
				}
			}

			m.mu.Lock()
			m.lastCheckAt = time.Now()
			m.mu.Unlock()

			log.Info().
				Int("total", len(results)).
				Int("healthy", healthy).
				Int("unhealthy", len(results)-healthy).
				Msg("background health check completed")

		case <-m.ctx.Done():
			return
		}
	}
}

// backgroundRefresh runs periodic token refresh
func (m *monitor) backgroundRefresh() {
	defer m.wg.Done()

	// Check more frequently than the refresh window
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.refreshExpiringSoon()
		case <-m.ctx.Done():
			return
		}
	}
}

// refreshExpiringSoon refreshes tokens that are about to expire
func (m *monitor) refreshExpiringSoon() {
	if m.refresher == nil {
		return
	}

	accounts, err := m.store.ListAccounts()
	if err != nil {
		log.Error().Err(err).Msg("failed to list accounts for refresh")
		return
	}

	for _, account := range accounts {
		if !account.IsActive || !account.IsOAuth() {
			continue
		}

		// Check if needs refresh
		if account.ExpiresAt == nil {
			continue
		}

		expiresIn := time.Until(*account.ExpiresAt)
		if expiresIn > m.config.TokenRefreshBefore {
			continue
		}

		log.Info().
			Str("account_id", account.ID).
			Dur("expires_in", expiresIn).
			Msg("refreshing expiring token")

		if err := m.refresher.RefreshToken(m.ctx, account.ID); err != nil {
			log.Error().
				Str("account_id", account.ID).
				Err(err).
				Msg("failed to refresh token")
		}
	}
}
