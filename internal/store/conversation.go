package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type ConversationContent struct {
	ID            string
	RequestLogID  string
	TokenID       string
	SystemPrompt  sql.NullString
	MessagesJSON  string // JSON encoded []Message
	Prompt        string
	Completion    string
	CreatedAt     time.Time
	IsCompressed  bool
}

type ConversationFilter struct {
	TokenID  string
	FromDate *time.Time
	ToDate   *time.Time
	Page     int
	Limit    int
}

// CreateConversation creates a new conversation content record
func (s *Store) CreateConversation(conv *ConversationContent) error {
	query := `INSERT INTO conversation_contents (
		id, request_log_id, token_id, system_prompt, messages_json,
		prompt, completion, created_at, is_compressed
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.Exec(query,
		conv.ID, conv.RequestLogID, conv.TokenID, conv.SystemPrompt, conv.MessagesJSON,
		conv.Prompt, conv.Completion, conv.CreatedAt, conv.IsCompressed,
	)

	// Also update FTS index
	if err == nil {
		_, _ = s.db.Exec(`INSERT INTO conversation_search (id, prompt, completion) VALUES (?, ?, ?)`,
			conv.ID, conv.Prompt, conv.Completion)
	}

	return err
}

// GetConversation retrieves a conversation by ID
func (s *Store) GetConversation(id string) (*ConversationContent, error) {
	query := `SELECT
		id, request_log_id, token_id, system_prompt, messages_json,
		prompt, completion, created_at, is_compressed
		FROM conversation_contents WHERE id = ?`

	row := s.db.QueryRow(query, id)

	var conv ConversationContent
	err := row.Scan(
		&conv.ID, &conv.RequestLogID, &conv.TokenID, &conv.SystemPrompt, &conv.MessagesJSON,
		&conv.Prompt, &conv.Completion, &conv.CreatedAt, &conv.IsCompressed,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &conv, nil
}

// ListConversations lists conversations with filtering and pagination
func (s *Store) ListConversations(filter ConversationFilter) ([]*ConversationContent, int, error) {
	// Build WHERE clause
	var conditions []string
	var args []interface{}

	if filter.TokenID != "" {
		conditions = append(conditions, "token_id = ?")
		args = append(args, filter.TokenID)
	}
	if filter.FromDate != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, *filter.FromDate)
	}
	if filter.ToDate != nil {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, *filter.ToDate)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM conversation_contents %s", whereClause)
	var total int
	err := s.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Set defaults for pagination
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Page < 0 {
		filter.Page = 0
	}
	offset := filter.Page * filter.Limit

	// Get conversations
	query := fmt.Sprintf(`SELECT
		id, request_log_id, token_id, system_prompt, messages_json,
		prompt, completion, created_at, is_compressed
		FROM conversation_contents %s
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`, whereClause)

	args = append(args, filter.Limit, offset)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var conversations []*ConversationContent
	for rows.Next() {
		var conv ConversationContent
		err := rows.Scan(
			&conv.ID, &conv.RequestLogID, &conv.TokenID, &conv.SystemPrompt, &conv.MessagesJSON,
			&conv.Prompt, &conv.Completion, &conv.CreatedAt, &conv.IsCompressed,
		)
		if err != nil {
			return nil, 0, err
		}
		conversations = append(conversations, &conv)
	}

	return conversations, total, rows.Err()
}

// SearchConversations performs full-text search on conversations
func (s *Store) SearchConversations(tokenID string, query string, limit int) ([]*ConversationContent, error) {
	if limit <= 0 {
		limit = 20
	}

	// Use FTS5 for full-text search
	searchQuery := `SELECT c.id, c.request_log_id, c.token_id, c.system_prompt, c.messages_json,
		c.prompt, c.completion, c.created_at, c.is_compressed
		FROM conversation_contents c
		INNER JOIN conversation_search s ON c.rowid = s.rowid
		WHERE c.token_id = ? AND conversation_search MATCH ?
		ORDER BY rank
		LIMIT ?`

	rows, err := s.db.Query(searchQuery, tokenID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conversations []*ConversationContent
	for rows.Next() {
		var conv ConversationContent
		err := rows.Scan(
			&conv.ID, &conv.RequestLogID, &conv.TokenID, &conv.SystemPrompt, &conv.MessagesJSON,
			&conv.Prompt, &conv.Completion, &conv.CreatedAt, &conv.IsCompressed,
		)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, &conv)
	}

	return conversations, rows.Err()
}

// DeleteConversation deletes a conversation by ID
func (s *Store) DeleteConversation(id string) error {
	// Delete from FTS index first
	_, _ = s.db.Exec(`DELETE FROM conversation_search WHERE id = ?`, id)

	// Delete from main table
	query := `DELETE FROM conversation_contents WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

// DeleteOldConversations deletes conversations older than the specified number of days
func (s *Store) DeleteOldConversations(daysToKeep int) (int64, error) {
	query := `DELETE FROM conversation_contents WHERE created_at < datetime('now', '-' || ? || ' days')`
	result, err := s.db.Exec(query, daysToKeep)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// MarkConversationAsCompressed marks a conversation as compressed
func (s *Store) MarkConversationAsCompressed(id string) error {
	query := `UPDATE conversation_contents SET is_compressed = 1 WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

// GetUncompressedConversations retrieves conversations that need compression
func (s *Store) GetUncompressedConversations(olderThanDays int, limit int) ([]*ConversationContent, error) {
	query := `SELECT
		id, request_log_id, token_id, system_prompt, messages_json,
		prompt, completion, created_at, is_compressed
		FROM conversation_contents
		WHERE is_compressed = 0 AND created_at < datetime('now', '-' || ? || ' days')
		LIMIT ?`

	rows, err := s.db.Query(query, olderThanDays, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conversations []*ConversationContent
	for rows.Next() {
		var conv ConversationContent
		err := rows.Scan(
			&conv.ID, &conv.RequestLogID, &conv.TokenID, &conv.SystemPrompt, &conv.MessagesJSON,
			&conv.Prompt, &conv.Completion, &conv.CreatedAt, &conv.IsCompressed,
		)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, &conv)
	}

	return conversations, rows.Err()
}
