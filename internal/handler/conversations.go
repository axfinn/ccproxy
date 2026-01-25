package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"ccproxy/internal/store"
)

type ConversationsHandler struct {
	store *store.Store
}

func NewConversationsHandler(store *store.Store) *ConversationsHandler {
	return &ConversationsHandler{
		store: store,
	}
}

type ListConversationsRequest struct {
	TokenID  string `form:"token_id"`
	FromDate string `form:"from_date"`
	ToDate   string `form:"to_date"`
	Page     int    `form:"page"`
	Limit    int    `form:"limit"`
}

type ConversationDTO struct {
	ID            string  `json:"id"`
	RequestLogID  string  `json:"request_log_id"`
	TokenID       string  `json:"token_id"`
	SystemPrompt  *string `json:"system_prompt,omitempty"`
	MessagesJSON  string  `json:"messages_json"`
	Prompt        string  `json:"prompt"`
	Completion    string  `json:"completion"`
	CreatedAt     string  `json:"created_at"`
	IsCompressed  bool    `json:"is_compressed"`
}

type ListConversationsResponse struct {
	Conversations []*ConversationDTO `json:"conversations"`
	Total         int                `json:"total"`
	Page          int                `json:"page"`
	Limit         int                `json:"limit"`
}

// ListConversations lists conversations with filtering and pagination
func (h *ConversationsHandler) ListConversations(c *gin.Context) {
	var req ListConversationsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Build filter
	filter := store.ConversationFilter{
		TokenID: req.TokenID,
		Page:    req.Page,
		Limit:   req.Limit,
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

	// Get conversations from store
	conversations, total, err := h.store.ListConversations(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list conversations"})
		return
	}

	// Convert to DTOs
	convDTOs := make([]*ConversationDTO, len(conversations))
	for i, conv := range conversations {
		convDTOs[i] = h.toConversationDTO(conv)
	}

	c.JSON(http.StatusOK, ListConversationsResponse{
		Conversations: convDTOs,
		Total:         total,
		Page:          filter.Page,
		Limit:         filter.Limit,
	})
}

// GetConversation retrieves a single conversation by ID
func (h *ConversationsHandler) GetConversation(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	conv, err := h.store.GetConversation(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get conversation"})
		return
	}

	if conv == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
		return
	}

	c.JSON(http.StatusOK, h.toConversationDTO(conv))
}

// SearchConversations performs full-text search on conversations
func (h *ConversationsHandler) SearchConversations(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'q' is required"})
		return
	}

	tokenID := c.Query("token_id")
	if tokenID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token_id is required"})
		return
	}

	limit := 20
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			limit = parsedLimit
		}
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	conversations, err := h.store.SearchConversations(tokenID, query, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search conversations"})
		return
	}

	// Convert to DTOs
	convDTOs := make([]*ConversationDTO, len(conversations))
	for i, conv := range conversations {
		convDTOs[i] = h.toConversationDTO(conv)
	}

	c.JSON(http.StatusOK, gin.H{
		"query":         query,
		"token_id":      tokenID,
		"conversations": convDTOs,
	})
}

// DeleteConversation deletes a conversation by ID
func (h *ConversationsHandler) DeleteConversation(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	err := h.store.DeleteConversation(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete conversation"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "conversation deleted successfully"})
}

// toConversationDTO converts a store.ConversationContent to a ConversationDTO
func (h *ConversationsHandler) toConversationDTO(conv *store.ConversationContent) *ConversationDTO {
	dto := &ConversationDTO{
		ID:           conv.ID,
		RequestLogID: conv.RequestLogID,
		TokenID:      conv.TokenID,
		MessagesJSON: conv.MessagesJSON,
		Prompt:       conv.Prompt,
		Completion:   conv.Completion,
		CreatedAt:    conv.CreatedAt.Format(time.RFC3339),
		IsCompressed: conv.IsCompressed,
	}

	if conv.SystemPrompt.Valid {
		systemPrompt := conv.SystemPrompt.String
		dto.SystemPrompt = &systemPrompt
	}

	return dto
}

// ExportConversations exports conversations to JSON or JSONL format
func (h *ConversationsHandler) ExportConversations(c *gin.Context) {
	format := c.DefaultQuery("format", "json")
	if format != "json" && format != "jsonl" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "format must be 'json' or 'jsonl'"})
		return
	}

	var req ListConversationsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Build filter (no pagination for export)
	filter := store.ConversationFilter{
		TokenID: req.TokenID,
		Limit:   10000, // Max export limit
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

	// Get conversations from store
	conversations, _, err := h.store.ListConversations(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list conversations"})
		return
	}

	if format == "jsonl" {
		h.exportJSONL(c, conversations)
	} else {
		h.exportJSON(c, conversations)
	}
}

// exportJSON exports conversations as JSON array
func (h *ConversationsHandler) exportJSON(c *gin.Context, conversations []*store.ConversationContent) {
	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=conversations_%s.json", time.Now().Format("20060102_150405")))

	// Convert to DTOs
	convDTOs := make([]*ConversationDTO, len(conversations))
	for i, conv := range conversations {
		convDTOs[i] = h.toConversationDTO(conv)
	}

	encoder := json.NewEncoder(c.Writer)
	encoder.SetIndent("", "  ")
	encoder.Encode(convDTOs)
}

// exportJSONL exports conversations as JSONL (one JSON object per line)
func (h *ConversationsHandler) exportJSONL(c *gin.Context, conversations []*store.ConversationContent) {
	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=conversations_%s.jsonl", time.Now().Format("20060102_150405")))

	encoder := json.NewEncoder(c.Writer)

	for _, conv := range conversations {
		dto := h.toConversationDTO(conv)
		encoder.Encode(dto)
	}
}
