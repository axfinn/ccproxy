package retry

import (
	"net/http"
	"time"
)

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxAttempts        int           `mapstructure:"max_attempts"`         // Max retry attempts per account
	MaxAccountSwitches int           `mapstructure:"max_account_switches"` // Max account switches
	InitialBackoff     time.Duration `mapstructure:"initial_backoff"`      // Initial backoff duration
	MaxBackoff         time.Duration `mapstructure:"max_backoff"`          // Maximum backoff duration
	Jitter             float64       `mapstructure:"jitter"`               // Jitter factor (0-1)
}

// DefaultRetryConfig returns the default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:        3,
		MaxAccountSwitches: 10,
		InitialBackoff:     100 * time.Millisecond,
		MaxBackoff:         2 * time.Second,
		Jitter:             0.2,
	}
}

// Policy defines retry behavior
type Policy interface {
	// ShouldRetry returns whether to retry the request
	ShouldRetry(err error, resp *http.Response, attempt int) bool
	// ShouldSwitchAccount returns whether to switch accounts
	ShouldSwitchAccount(err error, resp *http.Response) bool
	// GetBackoff returns the backoff duration for the given attempt
	GetBackoff(attempt int) time.Duration
	// MaxAttempts returns the maximum attempts
	MaxAttempts() int
	// MaxAccountSwitches returns the maximum account switches
	MaxAccountSwitches() int
}

// defaultPolicy implements Policy
type defaultPolicy struct {
	config RetryConfig
}

// NewPolicy creates a new retry policy
func NewPolicy(config RetryConfig) Policy {
	return &defaultPolicy{config: config}
}

// ShouldRetry returns whether to retry the request
func (p *defaultPolicy) ShouldRetry(err error, resp *http.Response, attempt int) bool {
	if attempt >= p.config.MaxAttempts {
		return false
	}

	// Network errors are retryable
	if err != nil {
		return true
	}

	if resp == nil {
		return true
	}

	// Classify by status code
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		// Rate limited - should retry with backoff
		return true
	case resp.StatusCode >= 500:
		// Server errors are retryable
		return true
	case resp.StatusCode == http.StatusBadGateway,
		resp.StatusCode == http.StatusServiceUnavailable,
		resp.StatusCode == http.StatusGatewayTimeout:
		return true
	default:
		// Client errors (4xx) are not retryable
		return false
	}
}

// ShouldSwitchAccount returns whether to switch accounts
func (p *defaultPolicy) ShouldSwitchAccount(err error, resp *http.Response) bool {
	// Network errors should switch accounts
	if err != nil {
		return true
	}

	if resp == nil {
		return true
	}

	// These errors indicate account-specific issues
	switch resp.StatusCode {
	case http.StatusUnauthorized,
		http.StatusForbidden:
		// Auth errors should switch accounts
		return true
	case http.StatusTooManyRequests:
		// Rate limited - switch accounts
		return true
	case http.StatusServiceUnavailable:
		// Service unavailable might be account-specific
		return true
	default:
		return false
	}
}

// GetBackoff returns the backoff duration for the given attempt
func (p *defaultPolicy) GetBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	backoff := p.config.InitialBackoff
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff > p.config.MaxBackoff {
			backoff = p.config.MaxBackoff
			break
		}
	}

	// Apply jitter
	jitter := time.Duration(float64(backoff) * p.config.Jitter)
	// Simplified jitter: subtract half, add random portion
	backoff = backoff - jitter + time.Duration(float64(jitter*2)*0.5)

	return backoff
}

// MaxAttempts returns the maximum attempts
func (p *defaultPolicy) MaxAttempts() int {
	return p.config.MaxAttempts
}

// MaxAccountSwitches returns the maximum account switches
func (p *defaultPolicy) MaxAccountSwitches() int {
	return p.config.MaxAccountSwitches
}

// ErrorClassification represents the type of error
type ErrorClassification int

const (
	ErrorRetryable     ErrorClassification = iota // Temporary error, can retry
	ErrorFatal                                    // Permanent error, don't retry
	ErrorRateLimited                              // Rate limited, retry with backoff
	ErrorAccountIssue                             // Account-specific issue, switch account
)

// ClassifyError classifies an error for retry handling
func ClassifyError(err error, resp *http.Response) ErrorClassification {
	if err != nil {
		return ErrorRetryable
	}

	if resp == nil {
		return ErrorRetryable
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return ErrorRateLimited
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden:
		return ErrorAccountIssue
	case resp.StatusCode >= 500:
		return ErrorRetryable
	case resp.StatusCode >= 400:
		return ErrorFatal
	default:
		return ErrorFatal // Success, no retry needed
	}
}
