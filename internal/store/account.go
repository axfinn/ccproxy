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
	IsActive   bool       `json:"is_active"`
	// Health check
	LastCheckAt   *time.Time `json:"last_check_at,omitempty"`
	HealthStatus  string     `json:"health_status,omitempty"` // "healthy", "unhealthy", "unknown"
	ErrorCount    int        `json:"error_count"`
	SuccessCount  int        `json:"success_count"`
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
