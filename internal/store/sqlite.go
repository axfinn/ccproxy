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
	ID                         string     `json:"id"`
	UserName                   string     `json:"user_name"`
	Mode                       string     `json:"mode"` // "web", "api", or "both"
	CreatedAt                  time.Time  `json:"created_at"`
	ExpiresAt                  time.Time  `json:"expires_at"`
	RevokedAt                  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt                 *time.Time `json:"last_used_at,omitempty"`
	EnableConversationLogging  bool       `json:"enable_conversation_logging"`
	TotalRequests              int        `json:"total_requests"`
	TotalTokensUsed            int        `json:"total_tokens_used"`
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
	// Enable WAL mode and optimizations for better performance
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=-64000")
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

		// Legacy sessions table (kept for backward compatibility)
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			session_key TEXT NOT NULL,
			organization_id TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME,
			last_used_at DATETIME,
			is_active BOOLEAN DEFAULT 1
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_is_active ON sessions(is_active)`,

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

		// Request logs table for detailed logging
		`CREATE TABLE IF NOT EXISTS request_logs (
			id TEXT PRIMARY KEY,
			token_id TEXT NOT NULL,
			account_id TEXT,
			user_name TEXT,
			mode TEXT NOT NULL,
			model TEXT NOT NULL,
			stream BOOLEAN NOT NULL,
			request_at DATETIME NOT NULL,
			response_at DATETIME,
			duration_ms INTEGER,
			ttft_ms INTEGER,
			prompt_tokens INTEGER DEFAULT 0,
			completion_tokens INTEGER DEFAULT 0,
			total_tokens INTEGER DEFAULT 0,
			status_code INTEGER NOT NULL,
			success BOOLEAN NOT NULL,
			error_message TEXT,
			conversation_id TEXT,
			FOREIGN KEY (token_id) REFERENCES tokens(id) ON DELETE CASCADE,
			FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE SET NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_token_id ON request_logs(token_id, request_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_account_id ON request_logs(account_id, request_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_request_at ON request_logs(request_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_status ON request_logs(success, status_code)`,

		// Conversation contents table for dialogue recording
		`CREATE TABLE IF NOT EXISTS conversation_contents (
			id TEXT PRIMARY KEY,
			request_log_id TEXT NOT NULL,
			token_id TEXT NOT NULL,
			system_prompt TEXT,
			messages_json TEXT NOT NULL,
			prompt TEXT NOT NULL,
			completion TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			is_compressed BOOLEAN DEFAULT 0,
			FOREIGN KEY (request_log_id) REFERENCES request_logs(id) ON DELETE CASCADE,
			FOREIGN KEY (token_id) REFERENCES tokens(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_token_id ON conversation_contents(token_id, created_at DESC)`,

		// Usage stats daily aggregation table
		`CREATE TABLE IF NOT EXISTS usage_stats_daily (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			stat_date DATE NOT NULL,
			token_id TEXT,
			account_id TEXT,
			mode TEXT,
			model TEXT,
			request_count INTEGER DEFAULT 0,
			success_count INTEGER DEFAULT 0,
			error_count INTEGER DEFAULT 0,
			total_prompt_tokens INTEGER DEFAULT 0,
			total_completion_tokens INTEGER DEFAULT 0,
			total_tokens INTEGER DEFAULT 0,
			avg_duration_ms INTEGER DEFAULT 0,
			avg_ttft_ms INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(stat_date, token_id, account_id, mode, model)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_stats_date ON usage_stats_daily(stat_date DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_stats_token ON usage_stats_daily(token_id, stat_date DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_stats_account ON usage_stats_daily(account_id, stat_date DESC)`,
	}

	for _, query := range queries {
		if _, err := s.db.Exec(query); err != nil {
			return err
		}
	}

	// Add new columns to tokens table (ignore errors if columns already exist)
	_ = s.addColumnIfNotExists("tokens", "enable_conversation_logging", "BOOLEAN DEFAULT 0")
	_ = s.addColumnIfNotExists("tokens", "total_requests", "INTEGER DEFAULT 0")
	_ = s.addColumnIfNotExists("tokens", "total_tokens_used", "INTEGER DEFAULT 0")

	// Create FTS5 virtual table for conversation search
	_, _ = s.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS conversation_search USING fts5(
		id UNINDEXED,
		prompt,
		completion,
		content='conversation_contents',
		content_rowid='rowid'
	)`)

	// Migrate data from sessions table to accounts table if sessions exist
	if err := s.migrateSessionsToAccounts(); err != nil {
		return err
	}

	// Migrate accounts table to sub2api style
	if err := s.MigrateAccountsToSub2APIStyle(); err != nil {
		return err
	}

	return nil
}

// addColumnIfNotExists adds a column to a table if it doesn't exist
func (s *Store) addColumnIfNotExists(table, column, definition string) error {
	query := `ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition
	_, err := s.db.Exec(query)
	// Ignore "duplicate column name" error
	if err != nil && err.Error() != "duplicate column name: "+column {
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
	query := `SELECT id, user_name, mode, created_at, expires_at, revoked_at, last_used_at,
		COALESCE(enable_conversation_logging, 0),
		COALESCE(total_requests, 0),
		COALESCE(total_tokens_used, 0)
		FROM tokens WHERE id = ?`
	row := s.db.QueryRow(query, id)

	var token Token
	err := row.Scan(&token.ID, &token.UserName, &token.Mode, &token.CreatedAt, &token.ExpiresAt,
		&token.RevokedAt, &token.LastUsedAt, &token.EnableConversationLogging,
		&token.TotalRequests, &token.TotalTokensUsed)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &token, nil
}

func (s *Store) ValidateToken(id string) (*Token, error) {
	query := `SELECT id, user_name, mode, created_at, expires_at, revoked_at, last_used_at,
		COALESCE(enable_conversation_logging, 0),
		COALESCE(total_requests, 0),
		COALESCE(total_tokens_used, 0)
		FROM tokens
		WHERE id = ? AND revoked_at IS NULL AND expires_at > datetime('now')`
	row := s.db.QueryRow(query, id)

	var token Token
	err := row.Scan(&token.ID, &token.UserName, &token.Mode, &token.CreatedAt, &token.ExpiresAt,
		&token.RevokedAt, &token.LastUsedAt, &token.EnableConversationLogging,
		&token.TotalRequests, &token.TotalTokensUsed)
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
	query := `SELECT id, user_name, mode, created_at, expires_at, revoked_at, last_used_at,
		COALESCE(enable_conversation_logging, 0),
		COALESCE(total_requests, 0),
		COALESCE(total_tokens_used, 0)
		FROM tokens ORDER BY created_at DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []*Token
	for rows.Next() {
		var token Token
		if err := rows.Scan(&token.ID, &token.UserName, &token.Mode, &token.CreatedAt,
			&token.ExpiresAt, &token.RevokedAt, &token.LastUsedAt,
			&token.EnableConversationLogging, &token.TotalRequests, &token.TotalTokensUsed); err != nil {
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

func (s *Store) UpdateTokenSettings(id string, enableConvLogging bool) error {
	query := `UPDATE tokens SET enable_conversation_logging = ? WHERE id = ?`
	_, err := s.db.Exec(query, enableConvLogging)
	return err
}

func (s *Store) IncrementTokenUsage(id string, tokensUsed int) error {
	query := `UPDATE tokens SET
		total_requests = total_requests + 1,
		total_tokens_used = total_tokens_used + ?,
		last_used_at = datetime('now')
		WHERE id = ?`
	_, err := s.db.Exec(query, tokensUsed, id)
	return err
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

func (s *Store) GetDB() *sql.DB {
	return s.db
}
