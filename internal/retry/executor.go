package retry

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// SelectAccountFunc selects an account, optionally excluding certain IDs
type SelectAccountFunc func(ctx context.Context, excludeIDs []string) (string, error)

// OperationFunc performs the actual operation
type OperationFunc func(ctx context.Context, accountID string) (*http.Response, error)

// ExecuteResult contains the result of execution
type ExecuteResult struct {
	Response        *http.Response
	AccountID       string
	Attempts        int
	AccountSwitches int
	TotalTime       time.Duration
}

// Executor handles retry logic with account switching
type Executor interface {
	// Execute executes an operation with retry and account switching
	Execute(ctx context.Context, selectFn SelectAccountFunc, opFn OperationFunc) (*ExecuteResult, error)
	// Stats returns executor statistics
	Stats() ExecutorStats
}

// ExecutorStats contains executor statistics
type ExecutorStats struct {
	TotalExecutions   int64 `json:"total_executions"`
	TotalRetries      int64 `json:"total_retries"`
	TotalSwitches     int64 `json:"total_switches"`
	SuccessfulRetries int64 `json:"successful_retries"`
	FailedExecutions  int64 `json:"failed_executions"`
}

// executor implements Executor
type executor struct {
	policy Policy

	totalExecutions   int64
	totalRetries      int64
	totalSwitches     int64
	successfulRetries int64
	failedExecutions  int64
}

// NewExecutor creates a new retry executor
func NewExecutor(policy Policy) Executor {
	return &executor{
		policy: policy,
	}
}

// Execute executes an operation with retry and account switching
func (e *executor) Execute(ctx context.Context, selectFn SelectAccountFunc, opFn OperationFunc) (*ExecuteResult, error) {
	start := time.Now()
	atomic.AddInt64(&e.totalExecutions, 1)

	result := &ExecuteResult{}
	var excludeIDs []string
	var lastErr error
	var lastResp *http.Response

	for result.AccountSwitches <= e.policy.MaxAccountSwitches() {
		// Select account
		accountID, err := selectFn(ctx, excludeIDs)
		if err != nil {
			atomic.AddInt64(&e.failedExecutions, 1)
			result.TotalTime = time.Since(start)
			return result, fmt.Errorf("failed to select account: %w", err)
		}

		result.AccountID = accountID

		// Try with this account
		resp, err := e.executeWithRetry(ctx, accountID, opFn, result)
		if err == nil && resp != nil && resp.StatusCode < 400 {
			// Success
			result.Response = resp
			result.TotalTime = time.Since(start)
			return result, nil
		}

		lastErr = err
		lastResp = resp

		// Check if we should switch accounts
		if !e.policy.ShouldSwitchAccount(err, resp) {
			break
		}

		// Switch account
		excludeIDs = append(excludeIDs, accountID)
		result.AccountSwitches++
		atomic.AddInt64(&e.totalSwitches, 1)

		log.Debug().
			Str("account_id", accountID).
			Int("switches", result.AccountSwitches).
			Msg("switching account")

		// Check context
		select {
		case <-ctx.Done():
			atomic.AddInt64(&e.failedExecutions, 1)
			result.TotalTime = time.Since(start)
			return result, ctx.Err()
		default:
		}
	}

	atomic.AddInt64(&e.failedExecutions, 1)
	result.TotalTime = time.Since(start)

	if lastErr != nil {
		return result, lastErr
	}

	if lastResp != nil {
		result.Response = lastResp
		return result, fmt.Errorf("request failed with status %d after %d attempts and %d account switches",
			lastResp.StatusCode, result.Attempts, result.AccountSwitches)
	}

	return result, fmt.Errorf("request failed after %d attempts and %d account switches",
		result.Attempts, result.AccountSwitches)
}

// executeWithRetry executes an operation with retry for a specific account
func (e *executor) executeWithRetry(ctx context.Context, accountID string, opFn OperationFunc, result *ExecuteResult) (*http.Response, error) {
	var lastErr error
	var lastResp *http.Response

	for attempt := 0; attempt < e.policy.MaxAttempts(); attempt++ {
		result.Attempts++

		// Backoff if not first attempt
		if attempt > 0 {
			atomic.AddInt64(&e.totalRetries, 1)
			backoff := e.policy.GetBackoff(attempt)

			log.Debug().
				Str("account_id", accountID).
				Int("attempt", attempt+1).
				Dur("backoff", backoff).
				Msg("retrying after backoff")

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		// Execute operation
		resp, err := opFn(ctx, accountID)

		// Check for success
		if err == nil && resp != nil && resp.StatusCode < 400 {
			if attempt > 0 {
				atomic.AddInt64(&e.successfulRetries, 1)
			}
			return resp, nil
		}

		lastErr = err
		lastResp = resp

		// Check if we should retry with this account
		if !e.policy.ShouldRetry(err, resp, attempt+1) {
			break
		}

		// Close response body if we're going to retry
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
	}

	return lastResp, lastErr
}

// Stats returns executor statistics
func (e *executor) Stats() ExecutorStats {
	return ExecutorStats{
		TotalExecutions:   atomic.LoadInt64(&e.totalExecutions),
		TotalRetries:      atomic.LoadInt64(&e.totalRetries),
		TotalSwitches:     atomic.LoadInt64(&e.totalSwitches),
		SuccessfulRetries: atomic.LoadInt64(&e.successfulRetries),
		FailedExecutions:  atomic.LoadInt64(&e.failedExecutions),
	}
}
