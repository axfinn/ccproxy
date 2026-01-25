package handler

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"ccproxy/internal/service"
	"ccproxy/internal/store"
)

// RequestLogContext holds information for logging a request
type RequestLogContext struct {
	RequestID             string
	TokenID               string
	AccountID             string
	UserName              string
	Model                 string
	Mode                  string
	Stream                bool
	RequestAt             time.Time
	ResponseAt            time.Time
	EnableConvLogging     bool
	SystemPrompt          string
	Messages              []OpenAIMessage
	Prompt                string
	Completion            string
	PromptTokens          int
	CompletionTokens      int
	TotalTokens           int
	StatusCode            int
	ErrorMessage          string
	ConversationID        string
}

// extractSystemPrompt extracts system prompt from messages
func extractSystemPrompt(messages []OpenAIMessage) string {
	for _, msg := range messages {
		if msg.Role == "system" {
			if str, ok := msg.Content.(string); ok {
				return str
			}
		}
	}
	return ""
}

// extractPrompt extracts the last user message as prompt
func extractPrompt(messages []OpenAIMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			if str, ok := messages[i].Content.(string); ok {
				return str
			}
		}
	}
	return ""
}

// buildLogEntry builds a log entry from the request context
func buildLogEntry(logCtx *RequestLogContext) *service.LogEntry {
	if logCtx == nil {
		return nil
	}

	entry := &service.LogEntry{
		Log: &store.RequestLog{
			ID:         logCtx.RequestID,
			TokenID:    logCtx.TokenID,
			UserName:   logCtx.UserName,
			Mode:       logCtx.Mode,
			Model:      logCtx.Model,
			Stream:     logCtx.Stream,
			RequestAt:  logCtx.RequestAt,
			StatusCode: logCtx.StatusCode,
		},
	}

	// Set account ID if present
	if logCtx.AccountID != "" {
		entry.Log.AccountID = sql.NullString{String: logCtx.AccountID, Valid: true}
	}

	// Set response time and duration if response completed
	if !logCtx.ResponseAt.IsZero() {
		entry.Log.ResponseAt = sql.NullTime{Time: logCtx.ResponseAt, Valid: true}
		durationMs := logCtx.ResponseAt.Sub(logCtx.RequestAt).Milliseconds()
		entry.Log.DurationMs = sql.NullInt64{Int64: durationMs, Valid: true}
	}

	// Set token usage
	entry.Log.PromptTokens = logCtx.PromptTokens
	entry.Log.CompletionTokens = logCtx.CompletionTokens
	entry.Log.TotalTokens = logCtx.TotalTokens

	// Set success status
	entry.Log.Success = logCtx.StatusCode >= 200 && logCtx.StatusCode < 400

	// Set error message if present
	if logCtx.ErrorMessage != "" {
		entry.Log.ErrorMessage = sql.NullString{String: logCtx.ErrorMessage, Valid: true}
	}

	// Set conversation ID if present
	if logCtx.ConversationID != "" {
		entry.Log.ConversationID = sql.NullString{String: logCtx.ConversationID, Valid: true}
	}

	// Build conversation content if enabled
	if logCtx.EnableConvLogging && logCtx.Prompt != "" && logCtx.Completion != "" {
		messagesJSON, err := json.Marshal(logCtx.Messages)
		if err != nil {
			log.Error().Err(err).Msg("Failed to marshal messages for conversation logging")
		} else {
			conv := &store.ConversationContent{
				ID:           uuid.New().String(),
				RequestLogID: logCtx.RequestID,
				TokenID:      logCtx.TokenID,
				Prompt:       logCtx.Prompt,
				Completion:   logCtx.Completion,
				MessagesJSON: string(messagesJSON),
				CreatedAt:    logCtx.RequestAt,
				IsCompressed: false,
			}

			if logCtx.SystemPrompt != "" {
				conv.SystemPrompt = sql.NullString{String: logCtx.SystemPrompt, Valid: true}
			}

			entry.Conversation = conv
		}
	}

	return entry
}

// logRequest asynchronously logs a request
func (h *EnhancedProxyHandler) logRequest(logCtx *RequestLogContext) {
	if logCtx == nil {
		return
	}

	// Build log entry
	entry := buildLogEntry(logCtx)
	if entry == nil {
		return
	}

	// Queue for async logging
	if h.requestLogger != nil {
		if err := h.requestLogger.LogRequest(entry); err != nil {
			log.Error().Err(err).Msg("Failed to queue request log")
		}
	}

	// Update token usage statistics
	if logCtx.TotalTokens > 0 {
		if err := h.store.IncrementTokenUsage(logCtx.TokenID, logCtx.TotalTokens); err != nil {
			log.Error().Err(err).Str("token_id", logCtx.TokenID).Msg("Failed to update token usage")
		}
	}
}

// createRequestLogContext creates a new request log context
func createRequestLogContext(tokenID, accountID, userName, mode, model string, stream bool, enableConvLogging bool, messages []OpenAIMessage) *RequestLogContext {
	return &RequestLogContext{
		RequestID:         uuid.New().String(),
		TokenID:           tokenID,
		AccountID:         accountID,
		UserName:          userName,
		Model:             model,
		Mode:              mode,
		Stream:            stream,
		RequestAt:         time.Now(),
		EnableConvLogging: enableConvLogging,
		SystemPrompt:      extractSystemPrompt(messages),
		Messages:          messages,
		Prompt:            extractPrompt(messages),
	}
}
