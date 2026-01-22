package store

import (
	"database/sql"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

type Token struct {
	ID         string     `json:"id"`
	UserName   string     `json:"user_name"`
	Mode       string     `json:"mode"` // "web", "api", or "both"
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type Session struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	SessionKey     string     `json:"session_key"`
	OrganizationID string     `json:"organization_id"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	IsActive       bool       `json:"is_active"`
}

func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS tokens (
			id TEXT PRIMARY KEY,
			user_name TEXT NOT NULL,
			mode TEXT NOT NULL DEFAULT 'both',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			revoked_at DATETIME,
			last_used_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tokens_expires_at ON tokens(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_tokens_revoked_at ON tokens(revoked_at)`,

		// New accounts table for OAuth support
		`CREATE TABLE IF NOT EXISTS accounts (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			credentials TEXT NOT NULL,
			organization_id TEXT,
			expires_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME,
			is_active BOOLEAN DEFAULT 1,
			last_check_at DATETIME,
			health_status TEXT DEFAULT 'unknown',
			error_count INTEGER DEFAULT 0,
			success_count INTEGER DEFAULT 0,
			max_concurrency INTEGER DEFAULT 5,
			priority INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_accounts_is_active ON accounts(is_active)`,
		`CREATE INDEX IF NOT EXISTS idx_accounts_type ON accounts(type)`,
		`CREATE INDEX IF NOT EXISTS idx_accounts_health ON accounts(health_status)`,
		`CREATE INDEX IF NOT EXISTS idx_accounts_priority ON accounts(priority)`,
	}

	for _, query := range queries {
		if _, err := s.db.Exec(query); err != nil {
			return err
		}
	}

	// Migrate data from sessions table to accounts table if sessions exist
	if err := s.migrateSessionsToAccounts(); err != nil {
		return err
	}

	return nil
}

// migrateSessionsToAccounts migrates legacy sessions to accounts
func (s *Store) migrateSessionsToAccounts() error {
	// Check if sessions table exists and has data
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sessions'`).Scan(&count)
	if err != nil || count == 0 {
		return nil // Sessions table doesn't exist, skip migration
	}

	// Check if we've already migrated (accounts table has data)
	var accountCount int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&accountCount)
	if err != nil {
		return err
	}
	if accountCount > 0 {
		return nil // Already migrated
	}

	// Migrate sessions to accounts
	rows, err := s.db.Query(`SELECT id, name, session_key, organization_id, created_at, expires_at, last_used_at, is_active FROM sessions`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var session Session
		if err := rows.Scan(&session.ID, &session.Name, &session.SessionKey, &session.OrganizationID, &session.CreatedAt, &session.ExpiresAt, &session.LastUsedAt, &session.IsActive); err != nil {
			return err
		}

		// Convert to account
		account := &Account{
			ID:             session.ID,
			Name:           session.Name,
			Type:           AccountTypeSessionKey,
			OrganizationID: session.OrganizationID,
			Credentials: Credentials{
				SessionKey: session.SessionKey,
			},
			CreatedAt:    session.CreatedAt,
			ExpiresAt:    session.ExpiresAt,
			LastUsedAt:   session.LastUsedAt,
			IsActive:     session.IsActive,
			HealthStatus: "unknown",
		}

		if err := s.CreateAccount(account); err != nil {
			return err
		}
	}

	return rows.Err()
}

// Token operations

func (s *Store) CreateToken(token *Token) error {
	query := `INSERT INTO tokens (id, user_name, mode, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`
	_, err := s.db.Exec(query, token.ID, token.UserName, token.Mode, token.CreatedAt, token.ExpiresAt)
	return err
}

func (s *Store) GetToken(id string) (*Token, error) {
	query := `SELECT id, user_name, mode, created_at, expires_at, revoked_at, last_used_at FROM tokens WHERE id = ?`
	row := s.db.QueryRow(query, id)

	var token Token
	err := row.Scan(&token.ID, &token.UserName, &token.Mode, &token.CreatedAt, &token.ExpiresAt, &token.RevokedAt, &token.LastUsedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &token, nil
}

func (s *Store) ValidateToken(id string) (*Token, error) {
	query := `SELECT id, user_name, mode, created_at, expires_at, revoked_at, last_used_at
		FROM tokens
		WHERE id = ? AND revoked_at IS NULL AND expires_at > datetime('now')`
	row := s.db.QueryRow(query, id)

	var token Token
	err := row.Scan(&token.ID, &token.UserName, &token.Mode, &token.CreatedAt, &token.ExpiresAt, &token.RevokedAt, &token.LastUsedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &token, nil
}

func (s *Store) UpdateTokenLastUsed(id string) error {
	query := `UPDATE tokens SET last_used_at = datetime('now') WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

func (s *Store) RevokeToken(id string) error {
	query := `UPDATE tokens SET revoked_at = datetime('now') WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

func (s *Store) ListTokens() ([]*Token, error) {
	query := `SELECT id, user_name, mode, created_at, expires_at, revoked_at, last_used_at FROM tokens ORDER BY created_at DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []*Token
	for rows.Next() {
		var token Token
		if err := rows.Scan(&token.ID, &token.UserName, &token.Mode, &token.CreatedAt, &token.ExpiresAt, &token.RevokedAt, &token.LastUsedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, &token)
	}

	return tokens, rows.Err()
}

func (s *Store) CleanupExpiredTokens() (int64, error) {
	query := `DELETE FROM tokens WHERE expires_at < datetime('now', '-30 days')`
	result, err := s.db.Exec(query)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// Session operations

func (s *Store) CreateSession(session *Session) error {
	query := `INSERT INTO sessions (id, name, session_key, organization_id, created_at, expires_at, is_active) VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.Exec(query, session.ID, session.Name, session.SessionKey, session.OrganizationID, session.CreatedAt, session.ExpiresAt, session.IsActive)
	return err
}

func (s *Store) GetSession(id string) (*Session, error) {
	query := `SELECT id, name, session_key, organization_id, created_at, expires_at, last_used_at, is_active FROM sessions WHERE id = ?`
	row := s.db.QueryRow(query, id)

	var session Session
	err := row.Scan(&session.ID, &session.Name, &session.SessionKey, &session.OrganizationID, &session.CreatedAt, &session.ExpiresAt, &session.LastUsedAt, &session.IsActive)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &session, nil
}

func (s *Store) GetActiveSession() (*Session, error) {
	query := `SELECT id, name, session_key, organization_id, created_at, expires_at, last_used_at, is_active
		FROM sessions
		WHERE is_active = 1 AND (expires_at IS NULL OR expires_at > datetime('now'))
		ORDER BY last_used_at DESC, created_at DESC
		LIMIT 1`
	row := s.db.QueryRow(query)

	var session Session
	err := row.Scan(&session.ID, &session.Name, &session.SessionKey, &session.OrganizationID, &session.CreatedAt, &session.ExpiresAt, &session.LastUsedAt, &session.IsActive)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &session, nil
}

func (s *Store) ListSessions() ([]*Session, error) {
	query := `SELECT id, name, session_key, organization_id, created_at, expires_at, last_used_at, is_active FROM sessions ORDER BY created_at DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var session Session
		if err := rows.Scan(&session.ID, &session.Name, &session.SessionKey, &session.OrganizationID, &session.CreatedAt, &session.ExpiresAt, &session.LastUsedAt, &session.IsActive); err != nil {
			return nil, err
		}
		sessions = append(sessions, &session)
	}

	return sessions, rows.Err()
}

func (s *Store) UpdateSessionLastUsed(id string) error {
	query := `UPDATE sessions SET last_used_at = datetime('now') WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

func (s *Store) DeactivateSession(id string) error {
	query := `UPDATE sessions SET is_active = 0 WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

func (s *Store) DeleteSession(id string) error {
	query := `DELETE FROM sessions WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}
