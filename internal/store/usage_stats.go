package store

import (
	"database/sql"
	"time"
)

type UsageStats struct {
	StatDate              time.Time
	TokenID               sql.NullString
	AccountID             sql.NullString
	Mode                  string
	Model                 string
	RequestCount          int
	SuccessCount          int
	ErrorCount            int
	TotalPromptTokens     int
	TotalCompletionTokens int
	TotalTokens           int
	AvgDurationMs         int
	AvgTTFTMs             int
}

type AggregatedStats struct {
	RequestCount          int     `json:"request_count"`
	SuccessCount          int     `json:"success_count"`
	ErrorCount            int     `json:"error_count"`
	TotalPromptTokens     int     `json:"total_prompt_tokens"`
	TotalCompletionTokens int     `json:"total_completion_tokens"`
	TotalTokens           int     `json:"total_tokens"`
	AvgDurationMs         int     `json:"avg_duration_ms"`
	AvgTTFTMs             int     `json:"avg_ttft_ms"`
	SuccessRate           float64 `json:"success_rate"`
}

type DailyStats struct {
	Date         string `json:"date"`
	RequestCount int    `json:"request_count"`
	SuccessCount int    `json:"success_count"`
	TotalTokens  int    `json:"total_tokens"`
}

type GlobalStats struct {
	TotalTokens          int                       `json:"total_tokens"`
	TotalRequests        int                       `json:"total_requests"`
	TotalUsers           int                       `json:"total_users"`
	ActiveTokens         int                       `json:"active_tokens"`
	ByMode               map[string]*AggregatedStats `json:"by_mode"`
	ByModel              map[string]*AggregatedStats `json:"by_model"`
}

// GetTokenStats retrieves aggregated statistics for a token
func (s *Store) GetTokenStats(tokenID string, from, to time.Time) (*AggregatedStats, error) {
	query := `SELECT
		COALESCE(SUM(request_count), 0) as request_count,
		COALESCE(SUM(success_count), 0) as success_count,
		COALESCE(SUM(error_count), 0) as error_count,
		COALESCE(SUM(total_prompt_tokens), 0) as total_prompt_tokens,
		COALESCE(SUM(total_completion_tokens), 0) as total_completion_tokens,
		COALESCE(SUM(total_tokens), 0) as total_tokens,
		COALESCE(AVG(avg_duration_ms), 0) as avg_duration_ms,
		COALESCE(AVG(avg_ttft_ms), 0) as avg_ttft_ms
		FROM usage_stats_daily
		WHERE token_id = ? AND stat_date >= ? AND stat_date <= ?`

	row := s.db.QueryRow(query, tokenID, from.Format("2006-01-02"), to.Format("2006-01-02"))

	var stats AggregatedStats
	err := row.Scan(
		&stats.RequestCount, &stats.SuccessCount, &stats.ErrorCount,
		&stats.TotalPromptTokens, &stats.TotalCompletionTokens, &stats.TotalTokens,
		&stats.AvgDurationMs, &stats.AvgTTFTMs,
	)
	if err != nil {
		return nil, err
	}

	// Calculate success rate
	if stats.RequestCount > 0 {
		stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.RequestCount) * 100
	}

	return &stats, nil
}

// GetTokenTrend retrieves daily trend for a token
func (s *Store) GetTokenTrend(tokenID string, days int) ([]*DailyStats, error) {
	query := `SELECT
		stat_date,
		SUM(request_count) as request_count,
		SUM(success_count) as success_count,
		SUM(total_tokens) as total_tokens
		FROM usage_stats_daily
		WHERE token_id = ? AND stat_date >= date('now', '-' || ? || ' days')
		GROUP BY stat_date
		ORDER BY stat_date ASC`

	rows, err := s.db.Query(query, tokenID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trends []*DailyStats
	for rows.Next() {
		var trend DailyStats
		err := rows.Scan(&trend.Date, &trend.RequestCount, &trend.SuccessCount, &trend.TotalTokens)
		if err != nil {
			return nil, err
		}
		trends = append(trends, &trend)
	}

	return trends, rows.Err()
}

// GetAccountStats retrieves aggregated statistics for an account
func (s *Store) GetAccountStats(accountID string, from, to time.Time) (*AggregatedStats, error) {
	query := `SELECT
		COALESCE(SUM(request_count), 0) as request_count,
		COALESCE(SUM(success_count), 0) as success_count,
		COALESCE(SUM(error_count), 0) as error_count,
		COALESCE(SUM(total_prompt_tokens), 0) as total_prompt_tokens,
		COALESCE(SUM(total_completion_tokens), 0) as total_completion_tokens,
		COALESCE(SUM(total_tokens), 0) as total_tokens,
		COALESCE(AVG(avg_duration_ms), 0) as avg_duration_ms,
		COALESCE(AVG(avg_ttft_ms), 0) as avg_ttft_ms
		FROM usage_stats_daily
		WHERE account_id = ? AND stat_date >= ? AND stat_date <= ?`

	row := s.db.QueryRow(query, accountID, from.Format("2006-01-02"), to.Format("2006-01-02"))

	var stats AggregatedStats
	err := row.Scan(
		&stats.RequestCount, &stats.SuccessCount, &stats.ErrorCount,
		&stats.TotalPromptTokens, &stats.TotalCompletionTokens, &stats.TotalTokens,
		&stats.AvgDurationMs, &stats.AvgTTFTMs,
	)
	if err != nil {
		return nil, err
	}

	if stats.RequestCount > 0 {
		stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.RequestCount) * 100
	}

	return &stats, nil
}

// GetAccountTrend retrieves daily trend for an account
func (s *Store) GetAccountTrend(accountID string, days int) ([]*DailyStats, error) {
	query := `SELECT
		stat_date,
		SUM(request_count) as request_count,
		SUM(success_count) as success_count,
		SUM(total_tokens) as total_tokens
		FROM usage_stats_daily
		WHERE account_id = ? AND stat_date >= date('now', '-' || ? || ' days')
		GROUP BY stat_date
		ORDER BY stat_date ASC`

	rows, err := s.db.Query(query, accountID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trends []*DailyStats
	for rows.Next() {
		var trend DailyStats
		err := rows.Scan(&trend.Date, &trend.RequestCount, &trend.SuccessCount, &trend.TotalTokens)
		if err != nil {
			return nil, err
		}
		trends = append(trends, &trend)
	}

	return trends, rows.Err()
}

// GetGlobalOverview retrieves global statistics
func (s *Store) GetGlobalOverview(from, to time.Time) (*GlobalStats, error) {
	stats := &GlobalStats{
		ByMode:  make(map[string]*AggregatedStats),
		ByModel: make(map[string]*AggregatedStats),
	}

	// Get overall stats
	query := `SELECT
		COALESCE(SUM(total_tokens), 0) as total_tokens,
		COALESCE(SUM(request_count), 0) as total_requests,
		COUNT(DISTINCT token_id) as total_users
		FROM usage_stats_daily
		WHERE stat_date >= ? AND stat_date <= ?`

	err := s.db.QueryRow(query, from.Format("2006-01-02"), to.Format("2006-01-02")).Scan(
		&stats.TotalTokens, &stats.TotalRequests, &stats.TotalUsers,
	)
	if err != nil {
		return nil, err
	}

	// Get active tokens count
	activeQuery := `SELECT COUNT(*) FROM tokens WHERE revoked_at IS NULL AND expires_at > datetime('now')`
	err = s.db.QueryRow(activeQuery).Scan(&stats.ActiveTokens)
	if err != nil {
		return nil, err
	}

	// Get stats by mode
	modeQuery := `SELECT
		mode,
		SUM(request_count) as request_count,
		SUM(success_count) as success_count,
		SUM(error_count) as error_count,
		SUM(total_prompt_tokens) as total_prompt_tokens,
		SUM(total_completion_tokens) as total_completion_tokens,
		SUM(total_tokens) as total_tokens,
		AVG(avg_duration_ms) as avg_duration_ms,
		AVG(avg_ttft_ms) as avg_ttft_ms
		FROM usage_stats_daily
		WHERE stat_date >= ? AND stat_date <= ?
		GROUP BY mode`

	rows, err := s.db.Query(modeQuery, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var mode string
		var modeStats AggregatedStats
		err := rows.Scan(&mode, &modeStats.RequestCount, &modeStats.SuccessCount, &modeStats.ErrorCount,
			&modeStats.TotalPromptTokens, &modeStats.TotalCompletionTokens, &modeStats.TotalTokens,
			&modeStats.AvgDurationMs, &modeStats.AvgTTFTMs)
		if err != nil {
			return nil, err
		}
		if modeStats.RequestCount > 0 {
			modeStats.SuccessRate = float64(modeStats.SuccessCount) / float64(modeStats.RequestCount) * 100
		}
		stats.ByMode[mode] = &modeStats
	}

	// Get stats by model
	modelQuery := `SELECT
		model,
		SUM(request_count) as request_count,
		SUM(success_count) as success_count,
		SUM(error_count) as error_count,
		SUM(total_prompt_tokens) as total_prompt_tokens,
		SUM(total_completion_tokens) as total_completion_tokens,
		SUM(total_tokens) as total_tokens,
		AVG(avg_duration_ms) as avg_duration_ms,
		AVG(avg_ttft_ms) as avg_ttft_ms
		FROM usage_stats_daily
		WHERE stat_date >= ? AND stat_date <= ?
		GROUP BY model`

	rows2, err := s.db.Query(modelQuery, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows2.Close()

	for rows2.Next() {
		var model string
		var modelStats AggregatedStats
		err := rows2.Scan(&model, &modelStats.RequestCount, &modelStats.SuccessCount, &modelStats.ErrorCount,
			&modelStats.TotalPromptTokens, &modelStats.TotalCompletionTokens, &modelStats.TotalTokens,
			&modelStats.AvgDurationMs, &modelStats.AvgTTFTMs)
		if err != nil {
			return nil, err
		}
		if modelStats.RequestCount > 0 {
			modelStats.SuccessRate = float64(modelStats.SuccessCount) / float64(modelStats.RequestCount) * 100
		}
		stats.ByModel[model] = &modelStats
	}

	return stats, nil
}
