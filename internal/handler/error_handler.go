package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"ccproxy/internal/store"
)

// ErrorClassifier classifies errors and updates account status accordingly (sub2api style)
type ErrorClassifier struct {
	store *store.Store
}

// NewErrorClassifier creates a new error classifier
func NewErrorClassifier(st *store.Store) *ErrorClassifier {
	return &ErrorClassifier{store: st}
}

// ClassifyAndHandleError classifies an HTTP error and updates account status
// Returns true if the error should trigger account switching (for retry logic)
func (e *ErrorClassifier) ClassifyAndHandleError(resp *http.Response, accountID string) bool {
	if resp == nil {
		// Network error - mark as temporary issue
		log.Warn().Str("account_id", accountID).Msg("network error, marking account as temporarily unavailable")
		until := time.Now().Add(10 * time.Second)
		e.store.SetAccountTempUnschedulable(accountID, until, "network_error")
		return true // Should switch account
	}

	statusCode := resp.StatusCode

	switch {
	case statusCode == http.StatusTooManyRequests: // 429 Rate Limited
		e.handleRateLimit(resp, accountID)
		return true // Should switch to another account

	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden: // 401/403
		e.handleAuthError(statusCode, accountID)
		return true // Should switch to another account

	case statusCode == http.StatusServiceUnavailable: // 503
		e.handleServiceUnavailable(accountID)
		return true // Should switch to another account

	case statusCode >= 500: // 500, 502, 504, etc
		e.handleServerError(statusCode, accountID)
		return false // Don't switch, just retry (server issue, not account issue)

	case statusCode >= 400 && statusCode < 500: // 4xx client errors (except 401/403/429)
		// Client error (bad request, etc) - don't affect account status
		log.Debug().Int("status_code", statusCode).Str("account_id", accountID).Msg("client error, not affecting account")
		return false // Don't switch, don't retry

	default:
		// Success or unknown status
		return false
	}
}

// handleRateLimit handles 429 rate limit errors
func (e *ErrorClassifier) handleRateLimit(resp *http.Response, accountID string) {
	// Try to parse Retry-After header
	retryAfter := 60 // Default 60 seconds
	if retryHeader := resp.Header.Get("Retry-After"); retryHeader != "" {
		if seconds, err := strconv.Atoi(retryHeader); err == nil {
			retryAfter = seconds
		}
	}

	resetAt := time.Now().Add(time.Duration(retryAfter) * time.Second)

	log.Warn().
		Str("account_id", accountID).
		Int("retry_after_seconds", retryAfter).
		Time("reset_at", resetAt).
		Msg("account rate limited, temporarily unscheduling")

	// Set rate limit with auto-recovery
	if err := e.store.SetAccountRateLimit(accountID, resetAt, "rate_limited"); err != nil {
		log.Error().Err(err).Str("account_id", accountID).Msg("failed to set rate limit")
	}
}

// handleAuthError handles 401/403 authentication errors
func (e *ErrorClassifier) handleAuthError(statusCode int, accountID string) {
	log.Error().
		Str("account_id", accountID).
		Int("status_code", statusCode).
		Msg("authentication failed, marking account as error")

	// Mark account as error - requires manual intervention
	if err := e.store.UpdateAccountStatus(accountID, store.AccountStatusError, "authentication failed"); err != nil {
		log.Error().Err(err).Str("account_id", accountID).Msg("failed to update account status")
	}

	// Also deactivate for safety
	if err := e.store.DeactivateAccount(accountID); err != nil {
		log.Error().Err(err).Str("account_id", accountID).Msg("failed to deactivate account")
	}
}

// handleServiceUnavailable handles 503 service unavailable errors
func (e *ErrorClassifier) handleServiceUnavailable(accountID string) {
	// Temporary overload, retry after 10 seconds
	overloadUntil := time.Now().Add(10 * time.Second)

	log.Warn().
		Str("account_id", accountID).
		Time("overload_until", overloadUntil).
		Msg("service unavailable, setting overload protection")

	if err := e.store.SetAccountOverload(accountID, overloadUntil); err != nil {
		log.Error().Err(err).Str("account_id", accountID).Msg("failed to set overload")
	}
}

// handleServerError handles 5xx server errors
func (e *ErrorClassifier) handleServerError(statusCode int, accountID string) {
	// Server error - don't mark account as failed, just log
	// The retry logic will handle switching if needed
	log.Warn().
		Str("account_id", accountID).
		Int("status_code", statusCode).
		Msg("server error, will retry without marking account")

	// Increment error count for monitoring
	e.store.IncrementAccountError(accountID)
}

// RecordSuccess records a successful request
func (e *ErrorClassifier) RecordSuccess(accountID string) {
	// Clear any temporary flags if they exist
	e.store.ClearAccountTempFlags(accountID)

	// Increment success count
	e.store.IncrementAccountSuccess(accountID)

	// If account was in error state, recover it
	account, err := e.store.GetAccount(accountID)
	if err == nil && account != nil && account.Status == store.AccountStatusError {
		log.Info().Str("account_id", accountID).Msg("account recovered from error state")
		e.store.UpdateAccountStatus(accountID, store.AccountStatusActive, "")
	}
}
