package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

// AccountType represents the type of account
type AccountType string

const (
	AccountTypeOAuth      AccountType = "oauth"       // OAuth account from claude.ai
	AccountTypeSessionKey AccountType = "session_key" // Legacy session key
	AccountTypeAPIKey     AccountType = "api_key"     // Direct API key (for future use)
)

// AccountStatus represents the account status
type AccountStatus string

const (
	AccountStatusActive   AccountStatus = "active"   // Account is active and healthy
	AccountStatusError    AccountStatus = "error"    // Account has authentication or config errors
	AccountStatusDisabled AccountStatus = "disabled" // Account is manually disabled
	AccountStatusPaused   AccountStatus = "paused"   // Account is temporarily paused
)

// Account represents a Claude account with credentials
type Account struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Type        AccountType `json:"type"`
	Credentials Credentials `json:"credentials"`

	// OAuth specific fields
	OrganizationID string     `json:"organization_id,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`

	// Metadata
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`

	// Status management (sub2api style)
	Status       AccountStatus `json:"status"`        // active, error, disabled, paused
	IsActive     bool          `json:"is_active"`     // Legacy field, kept for compatibility
	Schedulable  bool          `json:"schedulable"`   // Can this account be scheduled for requests?
	ErrorMessage string        `json:"error_message"` // Detailed error message

	// Health check
	LastCheckAt  *time.Time `json:"last_check_at,omitempty"`
	HealthStatus string     `json:"health_status,omitempty"` // "healthy", "unhealthy", "unknown"
	ErrorCount   int        `json:"error_count"`
	SuccessCount int        `json:"success_count"`

	// Time-based scheduling controls (sub2api style)
	RateLimitedAt    *time.Time `json:"rate_limited_at,omitempty"`     // When rate limiting started
	RateLimitResetAt *time.Time `json:"rate_limit_reset_at,omitempty"` // When rate limit will be reset
	OverloadUntil    *time.Time `json:"overload_until,omitempty"`      // Overload protection until this time

	// Temporary unschedulable (sub2api style) - auto-recovers when time expires
	TempUnschedulableUntil  *time.Time `json:"temp_unschedulable_until,omitempty"`  // Temporarily disable until this time
	TempUnschedulableReason string     `json:"temp_unschedulable_reason,omitempty"` // Reason for temp disable

	// Enhanced features
	MaxConcurrency int `json:"max_concurrency"` // Max concurrent requests for this account
	Priority       int `json:"priority"`        // Priority for scheduling (lower = higher priority)
}

// Credentials holds account authentication data
type Credentials struct {
	// For OAuth accounts
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	// For session key accounts
	SessionKey string `json:"session_key,omitempty"`
	// For API key accounts
	APIKey string `json:"api_key,omitempty"`
}

// IsOAuth returns true if the account is an OAuth account
func (a *Account) IsOAuth() bool {
	return a.Type == AccountTypeOAuth
}

// IsExpired returns true if the account has expired
func (a *Account) IsExpired() bool {
	if a.ExpiresAt == nil {
		return false
	}
	return a.ExpiresAt.Before(time.Now())
}

// NeedsRefresh returns true if the OAuth token needs to be refreshed
func (a *Account) NeedsRefresh() bool {
	if !a.IsOAuth() || a.ExpiresAt == nil {
		return false
	}
	// Refresh if less than 5 minutes remaining
	return a.ExpiresAt.Before(time.Now().Add(5 * time.Minute))
}

// IsSchedulable returns true if the account can be scheduled for requests (sub2api logic)
func (a *Account) IsSchedulable() bool {
	// 1. Basic status check
	if a.Status != AccountStatusActive || !a.Schedulable {
		return false
	}

	now := time.Now()

	// 2. Expiration check
	if a.ExpiresAt != nil && now.After(*a.ExpiresAt) {
		return false
	}

	// 3. Overload check - don't schedule if overloaded
	if a.OverloadUntil != nil && now.Before(*a.OverloadUntil) {
		return false
	}

	// 4. Rate limit check - auto-recovers when reset time passes
	if a.RateLimitResetAt != nil && now.Before(*a.RateLimitResetAt) {
		return false
	}

	// 5. Temporary unschedulable check - auto-recovers when time expires
	if a.TempUnschedulableUntil != nil && now.Before(*a.TempUnschedulableUntil) {
		return false
	}

	return true
}

// IsRateLimited returns true if the account is currently rate limited
func (a *Account) IsRateLimited() bool {
	if a.RateLimitResetAt == nil {
		return false
	}
	return time.Now().Before(*a.RateLimitResetAt)
}

// IsOverloaded returns true if the account is currently overloaded
func (a *Account) IsOverloaded() bool {
	if a.OverloadUntil == nil {
		return false
	}
	return time.Now().Before(*a.OverloadUntil)
}

// IsTempUnschedulable returns true if the account is temporarily unschedulable
func (a *Account) IsTempUnschedulable() bool {
	if a.TempUnschedulableUntil == nil {
		return false
	}
	return time.Now().Before(*a.TempUnschedulableUntil)
}

// Account database operations

func (s *Store) CreateAccount(account *Account) error {
	credBytes, err := json.Marshal(account.Credentials)
	if err != nil {
		return err
	}

	query := `INSERT INTO accounts (id, name, type, credentials, organization_id, expires_at, created_at, is_active, health_status, error_count, success_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = s.db.Exec(query,
		account.ID,
		account.Name,
		account.Type,
		credBytes,
		account.OrganizationID,
		account.ExpiresAt,
		account.CreatedAt,
		account.IsActive,
		account.HealthStatus,
		account.ErrorCount,
		account.SuccessCount,
	)
	return err
}

func (s *Store) GetAccount(id string) (*Account, error) {
	query := `SELECT id, name, type, credentials, organization_id, expires_at, created_at, last_used_at, is_active, last_check_at, health_status, error_count, success_count
		FROM accounts WHERE id = ?`
	row := s.db.QueryRow(query, id)

	var account Account
	var credBytes []byte
	err := row.Scan(
		&account.ID,
		&account.Name,
		&account.Type,
		&credBytes,
		&account.OrganizationID,
		&account.ExpiresAt,
		&account.CreatedAt,
		&account.LastUsedAt,
		&account.IsActive,
		&account.LastCheckAt,
		&account.HealthStatus,
		&account.ErrorCount,
		&account.SuccessCount,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(credBytes, &account.Credentials); err != nil {
		return nil, err
	}

	return &account, nil
}

func (s *Store) GetActiveAccount() (*Account, error) {
	query := `SELECT id, name, type, credentials, organization_id, expires_at, created_at, last_used_at, is_active, last_check_at, health_status, error_count, success_count
		FROM accounts
		WHERE is_active = 1 AND (expires_at IS NULL OR expires_at > datetime('now'))
		ORDER BY last_used_at DESC, created_at DESC
		LIMIT 1`
	row := s.db.QueryRow(query)

	var account Account
	var credBytes []byte
	err := row.Scan(
		&account.ID,
		&account.Name,
		&account.Type,
		&credBytes,
		&account.OrganizationID,
		&account.ExpiresAt,
		&account.CreatedAt,
		&account.LastUsedAt,
		&account.IsActive,
		&account.LastCheckAt,
		&account.HealthStatus,
		&account.ErrorCount,
		&account.SuccessCount,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(credBytes, &account.Credentials); err != nil {
		return nil, err
	}

	return &account, nil
}

func (s *Store) ListAccounts() ([]*Account, error) {
	query := `SELECT id, name, type, credentials, organization_id, expires_at, created_at, last_used_at, is_active, last_check_at, health_status, error_count, success_count
		FROM accounts ORDER BY created_at DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []*Account
	for rows.Next() {
		var account Account
		var credBytes []byte
		if err := rows.Scan(
			&account.ID,
			&account.Name,
			&account.Type,
			&credBytes,
			&account.OrganizationID,
			&account.ExpiresAt,
			&account.CreatedAt,
			&account.LastUsedAt,
			&account.IsActive,
			&account.LastCheckAt,
			&account.HealthStatus,
			&account.ErrorCount,
			&account.SuccessCount,
		); err != nil {
			return nil, err
		}

		if err := json.Unmarshal(credBytes, &account.Credentials); err != nil {
			return nil, err
		}

		accounts = append(accounts, &account)
	}

	return accounts, rows.Err()
}

func (s *Store) UpdateAccount(account *Account) error {
	credBytes, err := json.Marshal(account.Credentials)
	if err != nil {
		return err
	}

	query := `UPDATE accounts SET
		name = ?,
		type = ?,
		credentials = ?,
		organization_id = ?,
		expires_at = ?,
		is_active = ?,
		health_status = ?,
		error_count = ?,
		success_count = ?
		WHERE id = ?`
	_, err = s.db.Exec(query,
		account.Name,
		account.Type,
		credBytes,
		account.OrganizationID,
		account.ExpiresAt,
		account.IsActive,
		account.HealthStatus,
		account.ErrorCount,
		account.SuccessCount,
		account.ID,
	)
	return err
}

func (s *Store) UpdateAccountLastUsed(id string) error {
	query := `UPDATE accounts SET last_used_at = datetime('now') WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

func (s *Store) UpdateAccountHealth(id string, status string) error {
	query := `UPDATE accounts SET last_check_at = datetime('now'), health_status = ? WHERE id = ?`
	_, err := s.db.Exec(query, status, id)
	return err
}

func (s *Store) IncrementAccountError(id string) error {
	query := `UPDATE accounts SET error_count = error_count + 1 WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

func (s *Store) IncrementAccountSuccess(id string) error {
	query := `UPDATE accounts SET success_count = success_count + 1 WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

func (s *Store) DeactivateAccount(id string) error {
	query := `UPDATE accounts SET is_active = 0 WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

func (s *Store) DeleteAccount(id string) error {
	query := `DELETE FROM accounts WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

// GetSchedulableAccounts returns all accounts that can be scheduled (sub2api style)
func (s *Store) GetSchedulableAccounts() ([]*Account, error) {
	// Query for accounts that are:
	// 1. status = 'active'
	// 2. schedulable = true
	// 3. not expired
	query := `SELECT id, name, type, credentials, organization_id, expires_at, created_at, last_used_at,
		status, is_active, schedulable, error_message,
		last_check_at, health_status, error_count, success_count,
		rate_limited_at, rate_limit_reset_at, overload_until,
		temp_unschedulable_until, temp_unschedulable_reason,
		max_concurrency, priority
		FROM accounts
		WHERE status = 'active'
		AND schedulable = 1
		AND (expires_at IS NULL OR expires_at > datetime('now'))
		ORDER BY priority ASC, last_used_at ASC`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []*Account
	for rows.Next() {
		account, err := scanAccountRow(rows)
		if err != nil {
			return nil, err
		}

		// Double-check schedulability with time-based filters
		if account.IsSchedulable() {
			accounts = append(accounts, account)
		}
	}

	return accounts, rows.Err()
}

// scanAccountRow scans a database row into an Account struct
func scanAccountRow(rows *sql.Rows) (*Account, error) {
	var account Account
	var credBytes []byte

	err := rows.Scan(
		&account.ID,
		&account.Name,
		&account.Type,
		&credBytes,
		&account.OrganizationID,
		&account.ExpiresAt,
		&account.CreatedAt,
		&account.LastUsedAt,
		&account.Status,
		&account.IsActive,
		&account.Schedulable,
		&account.ErrorMessage,
		&account.LastCheckAt,
		&account.HealthStatus,
		&account.ErrorCount,
		&account.SuccessCount,
		&account.RateLimitedAt,
		&account.RateLimitResetAt,
		&account.OverloadUntil,
		&account.TempUnschedulableUntil,
		&account.TempUnschedulableReason,
		&account.MaxConcurrency,
		&account.Priority,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(credBytes, &account.Credentials); err != nil {
		return nil, err
	}

	return &account, nil
}

// UpdateAccountStatus updates the account status and related fields (sub2api style)
func (s *Store) UpdateAccountStatus(id string, status AccountStatus, errorMessage string) error {
	query := `UPDATE accounts SET status = ?, error_message = ?, schedulable = ?
		WHERE id = ?`
	schedulable := status == AccountStatusActive
	_, err := s.db.Exec(query, status, errorMessage, schedulable, id)
	return err
}

// SetAccountRateLimit sets rate limit information for an account
func (s *Store) SetAccountRateLimit(id string, resetAt time.Time, reason string) error {
	query := `UPDATE accounts SET
		rate_limited_at = datetime('now'),
		rate_limit_reset_at = ?,
		temp_unschedulable_until = ?,
		temp_unschedulable_reason = ?,
		schedulable = 0
		WHERE id = ?`
	_, err := s.db.Exec(query, resetAt, resetAt, reason, id)
	return err
}

// SetAccountOverload sets overload protection for an account
func (s *Store) SetAccountOverload(id string, overloadUntil time.Time) error {
	query := `UPDATE accounts SET overload_until = ? WHERE id = ?`
	_, err := s.db.Exec(query, overloadUntil, id)
	return err
}

// SetAccountTempUnschedulable temporarily marks an account as unschedulable
func (s *Store) SetAccountTempUnschedulable(id string, until time.Time, reason string) error {
	query := `UPDATE accounts SET
		temp_unschedulable_until = ?,
		temp_unschedulable_reason = ?,
		schedulable = 0
		WHERE id = ?`
	_, err := s.db.Exec(query, until, reason, id)
	return err
}

// ClearAccountTempFlags clears temporary scheduling flags for an account
func (s *Store) ClearAccountTempFlags(id string) error {
	query := `UPDATE accounts SET
		rate_limited_at = NULL,
		rate_limit_reset_at = NULL,
		overload_until = NULL,
		temp_unschedulable_until = NULL,
		temp_unschedulable_reason = '',
		schedulable = 1
		WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}
