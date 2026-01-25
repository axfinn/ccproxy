package handler

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"ccproxy/internal/store"
)

type RequestLogsHandler struct {
	store *store.Store
}

func NewRequestLogsHandler(store *store.Store) *RequestLogsHandler {
	return &RequestLogsHandler{
		store: store,
	}
}

type ListRequestLogsRequest struct {
	TokenID   string `form:"token_id"`
	AccountID string `form:"account_id"`
	UserName  string `form:"user_name"`
	Mode      string `form:"mode"`
	Model     string `form:"model"`
	Success   *bool  `form:"success"`
	FromDate  string `form:"from_date"`
	ToDate    string `form:"to_date"`
	Page      int    `form:"page"`
	Limit     int    `form:"limit"`
}

type ListRequestLogsResponse struct {
	Logs  []*RequestLogDTO `json:"logs"`
	Total int              `json:"total"`
	Page  int              `json:"page"`
	Limit int              `json:"limit"`
}

type RequestLogDTO struct {
	ID               string  `json:"id"`
	TokenID          string  `json:"token_id"`
	AccountID        *string `json:"account_id,omitempty"`
	UserName         string  `json:"user_name"`
	Mode             string  `json:"mode"`
	Model            string  `json:"model"`
	Stream           bool    `json:"stream"`
	RequestAt        string  `json:"request_at"`
	ResponseAt       *string `json:"response_at,omitempty"`
	DurationMs       *int64  `json:"duration_ms,omitempty"`
	TTFTMs           *int64  `json:"ttft_ms,omitempty"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	StatusCode       int     `json:"status_code"`
	Success          bool    `json:"success"`
	ErrorMessage     *string `json:"error_message,omitempty"`
	ConversationID   *string `json:"conversation_id,omitempty"`
}

// ListRequestLogs lists request logs with filtering and pagination
func (h *RequestLogsHandler) ListRequestLogs(c *gin.Context) {
	var req ListRequestLogsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Build filter
	filter := store.RequestLogFilter{
		TokenID:   req.TokenID,
		AccountID: req.AccountID,
		UserName:  req.UserName,
		Mode:      req.Mode,
		Model:     req.Model,
		Success:   req.Success,
		Page:      req.Page,
		Limit:     req.Limit,
	}

	// Parse dates
	if req.FromDate != "" {
		if t, err := time.Parse(time.RFC3339, req.FromDate); err == nil {
			filter.FromDate = &t
		}
	}
	if req.ToDate != "" {
		if t, err := time.Parse(time.RFC3339, req.ToDate); err == nil {
			filter.ToDate = &t
		}
	}

	// Get logs from store
	logs, total, err := h.store.ListRequestLogs(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list request logs"})
		return
	}

	// Convert to DTOs
	logDTOs := make([]*RequestLogDTO, len(logs))
	for i, log := range logs {
		logDTOs[i] = h.toRequestLogDTO(log)
	}

	c.JSON(http.StatusOK, ListRequestLogsResponse{
		Logs:  logDTOs,
		Total: total,
		Page:  filter.Page,
		Limit: filter.Limit,
	})
}

// GetRequestLog retrieves a single request log by ID
func (h *RequestLogsHandler) GetRequestLog(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	log, err := h.store.GetRequestLog(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get request log"})
		return
	}

	if log == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "request log not found"})
		return
	}

	c.JSON(http.StatusOK, h.toRequestLogDTO(log))
}

// toRequestLogDTO converts a store.RequestLog to a RequestLogDTO
func (h *RequestLogsHandler) toRequestLogDTO(log *store.RequestLog) *RequestLogDTO {
	dto := &RequestLogDTO{
		ID:               log.ID,
		TokenID:          log.TokenID,
		UserName:         log.UserName,
		Mode:             log.Mode,
		Model:            log.Model,
		Stream:           log.Stream,
		RequestAt:        log.RequestAt.Format(time.RFC3339),
		PromptTokens:     log.PromptTokens,
		CompletionTokens: log.CompletionTokens,
		TotalTokens:      log.TotalTokens,
		StatusCode:       log.StatusCode,
		Success:          log.Success,
	}

	if log.AccountID.Valid {
		accountID := log.AccountID.String
		dto.AccountID = &accountID
	}

	if log.ResponseAt.Valid {
		responseAt := log.ResponseAt.Time.Format(time.RFC3339)
		dto.ResponseAt = &responseAt
	}

	if log.DurationMs.Valid {
		durationMs := log.DurationMs.Int64
		dto.DurationMs = &durationMs
	}

	if log.TTFTMs.Valid {
		ttftMs := log.TTFTMs.Int64
		dto.TTFTMs = &ttftMs
	}

	if log.ErrorMessage.Valid {
		errorMsg := log.ErrorMessage.String
		dto.ErrorMessage = &errorMsg
	}

	if log.ConversationID.Valid {
		convID := log.ConversationID.String
		dto.ConversationID = &convID
	}

	return dto
}

// DeleteOldRequestLogs deletes request logs older than specified days
func (h *RequestLogsHandler) DeleteOldRequestLogs(c *gin.Context) {
	daysStr := c.DefaultQuery("days", "90")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid days parameter"})
		return
	}

	count, err := h.store.DeleteOldRequestLogs(days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete old logs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted": count,
		"days":    days,
	})
}

// ExportRequestLogs exports request logs to CSV or JSON format
func (h *RequestLogsHandler) ExportRequestLogs(c *gin.Context) {
	format := c.DefaultQuery("format", "csv")
	if format != "csv" && format != "json" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "format must be 'csv' or 'json'"})
		return
	}

	var req ListRequestLogsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Build filter (no pagination for export)
	filter := store.RequestLogFilter{
		TokenID:   req.TokenID,
		AccountID: req.AccountID,
		UserName:  req.UserName,
		Mode:      req.Mode,
		Model:     req.Model,
		Success:   req.Success,
		Limit:     10000, // Max export limit
	}

	// Parse dates
	if req.FromDate != "" {
		if t, err := time.Parse(time.RFC3339, req.FromDate); err == nil {
			filter.FromDate = &t
		}
	}
	if req.ToDate != "" {
		if t, err := time.Parse(time.RFC3339, req.ToDate); err == nil {
			filter.ToDate = &t
		}
	}

	// Get logs from store
	logs, _, err := h.store.ListRequestLogs(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list request logs"})
		return
	}

	if format == "csv" {
		h.exportCSV(c, logs)
	} else {
		h.exportJSON(c, logs)
	}
}

// exportCSV exports logs as CSV
func (h *RequestLogsHandler) exportCSV(c *gin.Context, logs []*store.RequestLog) {
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=request_logs_%s.csv", time.Now().Format("20060102_150405")))

	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Write header
	header := []string{
		"ID", "TokenID", "AccountID", "UserName", "Mode", "Model", "Stream",
		"RequestAt", "ResponseAt", "DurationMs", "TTFTMs",
		"PromptTokens", "CompletionTokens", "TotalTokens",
		"StatusCode", "Success", "ErrorMessage", "ConversationID",
	}
	writer.Write(header)

	// Write data
	for _, log := range logs {
		row := []string{
			log.ID,
			log.TokenID,
			log.AccountID.String,
			log.UserName,
			log.Mode,
			log.Model,
			fmt.Sprintf("%t", log.Stream),
			log.RequestAt.Format(time.RFC3339),
			formatNullTime(log.ResponseAt),
			formatNullInt64(log.DurationMs),
			formatNullInt64(log.TTFTMs),
			fmt.Sprintf("%d", log.PromptTokens),
			fmt.Sprintf("%d", log.CompletionTokens),
			fmt.Sprintf("%d", log.TotalTokens),
			fmt.Sprintf("%d", log.StatusCode),
			fmt.Sprintf("%t", log.Success),
			log.ErrorMessage.String,
			log.ConversationID.String,
		}
		writer.Write(row)
	}
}

// exportJSON exports logs as JSON
func (h *RequestLogsHandler) exportJSON(c *gin.Context, logs []*store.RequestLog) {
	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=request_logs_%s.json", time.Now().Format("20060102_150405")))

	// Convert to DTOs
	logDTOs := make([]*RequestLogDTO, len(logs))
	for i, log := range logs {
		logDTOs[i] = h.toRequestLogDTO(log)
	}

	encoder := json.NewEncoder(c.Writer)
	encoder.SetIndent("", "  ")
	encoder.Encode(logDTOs)
}

// Helper functions
func formatNullTime(nt sql.NullTime) string {
	if nt.Valid {
		return nt.Time.Format(time.RFC3339)
	}
	return ""
}

func formatNullInt64(ni sql.NullInt64) string {
	if ni.Valid {
		return fmt.Sprintf("%d", ni.Int64)
	}
	return ""
}
