package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"ccproxy/internal/store"
)

type StatsHandler struct {
	store *store.Store
}

func NewStatsHandler(store *store.Store) *StatsHandler {
	return &StatsHandler{
		store: store,
	}
}

type GetStatsRequest struct {
	FromDate string `form:"from_date"`
	ToDate   string `form:"to_date"`
	Days     int    `form:"days"` // Alternative to from_date/to_date
}

// GetTokenStats retrieves aggregated statistics for a specific token
func (h *StatsHandler) GetTokenStats(c *gin.Context) {
	tokenID := c.Param("id")
	if tokenID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token_id is required"})
		return
	}

	// Parse request
	var req GetStatsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Determine date range
	from, to := h.getDateRange(req)

	// Get stats
	stats, err := h.store.GetTokenStats(tokenID, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get token stats"})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetTokenTrend retrieves daily trend for a specific token
func (h *StatsHandler) GetTokenTrend(c *gin.Context) {
	tokenID := c.Param("id")
	if tokenID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token_id is required"})
		return
	}

	daysStr := c.DefaultQuery("days", "30")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days <= 0 || days > 365 {
		days = 30
	}

	trend, err := h.store.GetTokenTrend(tokenID, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get token trend"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token_id": tokenID,
		"days":     days,
		"trend":    trend,
	})
}

// GetAccountStats retrieves aggregated statistics for a specific account
func (h *StatsHandler) GetAccountStats(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account_id is required"})
		return
	}

	// Parse request
	var req GetStatsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Determine date range
	from, to := h.getDateRange(req)

	// Get stats
	stats, err := h.store.GetAccountStats(accountID, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get account stats"})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetAccountTrend retrieves daily trend for a specific account
func (h *StatsHandler) GetAccountTrend(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account_id is required"})
		return
	}

	daysStr := c.DefaultQuery("days", "30")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days <= 0 || days > 365 {
		days = 30
	}

	trend, err := h.store.GetAccountTrend(accountID, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get account trend"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"account_id": accountID,
		"days":       days,
		"trend":      trend,
	})
}

// GetOverview retrieves global statistics overview
func (h *StatsHandler) GetOverview(c *gin.Context) {
	// Parse request
	var req GetStatsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Determine date range (default to today)
	from, to := h.getDateRange(req)
	if req.Days == 0 && req.FromDate == "" && req.ToDate == "" {
		// Default to today
		today := time.Now().Truncate(24 * time.Hour)
		from = today
		to = today.Add(24*time.Hour - time.Second)
	}

	// Get overview
	overview, err := h.store.GetGlobalOverview(from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get overview"})
		return
	}

	c.JSON(http.StatusOK, overview)
}

// GetRealtimeStats retrieves real-time statistics from request_logs (not aggregated)
func (h *StatsHandler) GetRealtimeStats(c *gin.Context) {
	// Get today's stats directly from request_logs for real-time data
	query := `
		SELECT
			COUNT(*) as total_requests,
			SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as success_count,
			SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) as error_count,
			SUM(total_tokens) as total_tokens,
			AVG(duration_ms) as avg_duration_ms
		FROM request_logs
		WHERE DATE(request_at) = DATE('now')
	`

	row := h.store.GetDB().QueryRow(query)

	var stats struct {
		TotalRequests  int     `json:"total_requests"`
		SuccessCount   int     `json:"success_count"`
		ErrorCount     int     `json:"error_count"`
		TotalTokens    int     `json:"total_tokens"`
		AvgDurationMs  float64 `json:"avg_duration_ms"`
		SuccessRate    float64 `json:"success_rate"`
	}

	err := row.Scan(&stats.TotalRequests, &stats.SuccessCount, &stats.ErrorCount,
		&stats.TotalTokens, &stats.AvgDurationMs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get realtime stats"})
		return
	}

	if stats.TotalRequests > 0 {
		stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TotalRequests) * 100
	}

	c.JSON(http.StatusOK, stats)
}

// GetTopTokens retrieves top tokens by usage
func (h *StatsHandler) GetTopTokens(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 10
	}

	daysStr := c.DefaultQuery("days", "7")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days <= 0 || days > 365 {
		days = 7
	}

	query := `
		SELECT
			token_id,
			SUM(request_count) as total_requests,
			SUM(total_tokens) as total_tokens,
			SUM(success_count) as success_count,
			SUM(error_count) as error_count
		FROM usage_stats_daily
		WHERE stat_date >= DATE('now', '-' || ? || ' days')
		GROUP BY token_id
		ORDER BY total_requests DESC
		LIMIT ?
	`

	rows, err := h.store.GetDB().Query(query, days, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get top tokens"})
		return
	}
	defer rows.Close()

	type TopToken struct {
		TokenID       string  `json:"token_id"`
		TotalRequests int     `json:"total_requests"`
		TotalTokens   int     `json:"total_tokens"`
		SuccessCount  int     `json:"success_count"`
		ErrorCount    int     `json:"error_count"`
		SuccessRate   float64 `json:"success_rate"`
	}

	var topTokens []TopToken
	for rows.Next() {
		var t TopToken
		if err := rows.Scan(&t.TokenID, &t.TotalRequests, &t.TotalTokens, &t.SuccessCount, &t.ErrorCount); err != nil {
			continue
		}
		if t.TotalRequests > 0 {
			t.SuccessRate = float64(t.SuccessCount) / float64(t.TotalRequests) * 100
		}
		topTokens = append(topTokens, t)
	}

	c.JSON(http.StatusOK, gin.H{
		"days":   days,
		"limit":  limit,
		"tokens": topTokens,
	})
}

// GetTopModels retrieves top models by usage
func (h *StatsHandler) GetTopModels(c *gin.Context) {
	daysStr := c.DefaultQuery("days", "7")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days <= 0 || days > 365 {
		days = 7
	}

	query := `
		SELECT
			model,
			SUM(request_count) as total_requests,
			SUM(total_tokens) as total_tokens,
			SUM(success_count) as success_count
		FROM usage_stats_daily
		WHERE stat_date >= DATE('now', '-' || ? || ' days')
		GROUP BY model
		ORDER BY total_requests DESC
	`

	rows, err := h.store.GetDB().Query(query, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get top models"})
		return
	}
	defer rows.Close()

	type ModelStats struct {
		Model         string  `json:"model"`
		TotalRequests int     `json:"total_requests"`
		TotalTokens   int     `json:"total_tokens"`
		SuccessCount  int     `json:"success_count"`
		SuccessRate   float64 `json:"success_rate"`
	}

	var modelStats []ModelStats
	for rows.Next() {
		var m ModelStats
		if err := rows.Scan(&m.Model, &m.TotalRequests, &m.TotalTokens, &m.SuccessCount); err != nil {
			continue
		}
		if m.TotalRequests > 0 {
			m.SuccessRate = float64(m.SuccessCount) / float64(m.TotalRequests) * 100
		}
		modelStats = append(modelStats, m)
	}

	c.JSON(http.StatusOK, gin.H{
		"days":   days,
		"models": modelStats,
	})
}

// getDateRange parses the date range from request parameters
func (h *StatsHandler) getDateRange(req GetStatsRequest) (time.Time, time.Time) {
	var from, to time.Time

	if req.Days > 0 {
		// Use days parameter
		to = time.Now()
		from = to.AddDate(0, 0, -req.Days)
	} else if req.FromDate != "" && req.ToDate != "" {
		// Use from_date and to_date
		from, _ = time.Parse("2006-01-02", req.FromDate)
		to, _ = time.Parse("2006-01-02", req.ToDate)
	} else if req.FromDate != "" {
		// Only from_date, use until now
		from, _ = time.Parse("2006-01-02", req.FromDate)
		to = time.Now()
	} else if req.ToDate != "" {
		// Only to_date, use last 7 days
		to, _ = time.Parse("2006-01-02", req.ToDate)
		from = to.AddDate(0, 0, -7)
	} else {
		// Default: last 7 days
		to = time.Now()
		from = to.AddDate(0, 0, -7)
	}

	return from, to
}
