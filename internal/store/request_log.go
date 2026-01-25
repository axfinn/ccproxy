package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type RequestLog struct {
	ID               string
	TokenID          string
	AccountID        sql.NullString
	UserName         string
	Mode             string
	Model            string
	Stream           bool
	RequestAt        time.Time
	ResponseAt       sql.NullTime
	DurationMs       sql.NullInt64
	TTFTMs           sql.NullInt64
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	StatusCode       int
	Success          bool
	ErrorMessage     sql.NullString
	ConversationID   sql.NullString
}

type RequestLogFilter struct {
	TokenID   string
	AccountID string
	UserName  string
	Mode      string
	Model     string
	Success   *bool
	FromDate  *time.Time
	ToDate    *time.Time
	Page      int
	Limit     int
}

// CreateRequestLog creates a new request log entry
func (s *Store) CreateRequestLog(log *RequestLog) error {
	query := `INSERT INTO request_logs (
		id, token_id, account_id, user_name, mode, model, stream,
		request_at, response_at, duration_ms, ttft_ms,
		prompt_tokens, completion_tokens, total_tokens,
		status_code, success, error_message, conversation_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.Exec(query,
		log.ID, log.TokenID, log.AccountID, log.UserName, log.Mode, log.Model, log.Stream,
		log.RequestAt, log.ResponseAt, log.DurationMs, log.TTFTMs,
		log.PromptTokens, log.CompletionTokens, log.TotalTokens,
		log.StatusCode, log.Success, log.ErrorMessage, log.ConversationID,
	)
	return err
}

// GetRequestLog retrieves a request log by ID
func (s *Store) GetRequestLog(id string) (*RequestLog, error) {
	query := `SELECT
		id, token_id, account_id, user_name, mode, model, stream,
		request_at, response_at, duration_ms, ttft_ms,
		prompt_tokens, completion_tokens, total_tokens,
		status_code, success, error_message, conversation_id
		FROM request_logs WHERE id = ?`

	row := s.db.QueryRow(query, id)

	var log RequestLog
	err := row.Scan(
		&log.ID, &log.TokenID, &log.AccountID, &log.UserName, &log.Mode, &log.Model, &log.Stream,
		&log.RequestAt, &log.ResponseAt, &log.DurationMs, &log.TTFTMs,
		&log.PromptTokens, &log.CompletionTokens, &log.TotalTokens,
		&log.StatusCode, &log.Success, &log.ErrorMessage, &log.ConversationID,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &log, nil
}

// ListRequestLogs lists request logs with filtering and pagination
func (s *Store) ListRequestLogs(filter RequestLogFilter) ([]*RequestLog, int, error) {
	// Build WHERE clause
	var conditions []string
	var args []interface{}

	if filter.TokenID != "" {
		conditions = append(conditions, "token_id = ?")
		args = append(args, filter.TokenID)
	}
	if filter.AccountID != "" {
		conditions = append(conditions, "account_id = ?")
		args = append(args, filter.AccountID)
	}
	if filter.UserName != "" {
		conditions = append(conditions, "user_name = ?")
		args = append(args, filter.UserName)
	}
	if filter.Mode != "" {
		conditions = append(conditions, "mode = ?")
		args = append(args, filter.Mode)
	}
	if filter.Model != "" {
		conditions = append(conditions, "model = ?")
		args = append(args, filter.Model)
	}
	if filter.Success != nil {
		conditions = append(conditions, "success = ?")
		args = append(args, *filter.Success)
	}
	if filter.FromDate != nil {
		conditions = append(conditions, "request_at >= ?")
		args = append(args, *filter.FromDate)
	}
	if filter.ToDate != nil {
		conditions = append(conditions, "request_at <= ?")
		args = append(args, *filter.ToDate)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM request_logs %s", whereClause)
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

	// Get logs
	query := fmt.Sprintf(`SELECT
		id, token_id, account_id, user_name, mode, model, stream,
		request_at, response_at, duration_ms, ttft_ms,
		prompt_tokens, completion_tokens, total_tokens,
		status_code, success, error_message, conversation_id
		FROM request_logs %s
		ORDER BY request_at DESC
		LIMIT ? OFFSET ?`, whereClause)

	args = append(args, filter.Limit, offset)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []*RequestLog
	for rows.Next() {
		var log RequestLog
		err := rows.Scan(
			&log.ID, &log.TokenID, &log.AccountID, &log.UserName, &log.Mode, &log.Model, &log.Stream,
			&log.RequestAt, &log.ResponseAt, &log.DurationMs, &log.TTFTMs,
			&log.PromptTokens, &log.CompletionTokens, &log.TotalTokens,
			&log.StatusCode, &log.Success, &log.ErrorMessage, &log.ConversationID,
		)
		if err != nil {
			return nil, 0, err
		}
		logs = append(logs, &log)
	}

	return logs, total, rows.Err()
}

// DeleteOldRequestLogs deletes request logs older than the specified number of days
func (s *Store) DeleteOldRequestLogs(daysToKeep int) (int64, error) {
	query := `DELETE FROM request_logs WHERE request_at < datetime('now', '-' || ? || ' days')`
	result, err := s.db.Exec(query, daysToKeep)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
