package ratelimit

import (
	"context"
	"time"
)

// RateLimitConfig holds rate limit configuration
type RateLimitConfig struct {
	Enabled      bool        `mapstructure:"enabled"`
	UserLimit    LimitRule   `mapstructure:"user_limit"`
	AccountLimit LimitRule   `mapstructure:"account_limit"`
	IPLimit      LimitRule   `mapstructure:"ip_limit"`
	GlobalLimit  LimitRule   `mapstructure:"global_limit"`
}

// LimitRule defines a rate limit rule
type LimitRule struct {
	Requests int           `mapstructure:"requests"` // Max requests
	Window   time.Duration `mapstructure:"window"`   // Time window
}

// DefaultRateLimitConfig returns the default rate limit configuration
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Enabled: true,
		UserLimit: LimitRule{
			Requests: 100,
			Window:   1 * time.Minute,
		},
		AccountLimit: LimitRule{
			Requests: 1000,
			Window:   1 * time.Minute,
		},
		IPLimit: LimitRule{
			Requests: 200,
			Window:   1 * time.Minute,
		},
		GlobalLimit: LimitRule{
			Requests: 10000,
			Window:   1 * time.Minute,
		},
	}
}

// Result contains the result of a rate limit check
type Result struct {
	Allowed   bool          `json:"allowed"`
	Remaining int           `json:"remaining"`
	ResetAt   time.Time     `json:"reset_at"`
	RetryAt   *time.Time    `json:"retry_at,omitempty"`
	Limit     int           `json:"limit"`
	Window    time.Duration `json:"window"`
}

// Limiter checks rate limits for a single key
type Limiter interface {
	// Allow checks if a request is allowed
	Allow(ctx context.Context, key string) (*Result, error)
	// Reset resets the limit for a key
	Reset(ctx context.Context, key string) error
}

// MultiLimiter checks multiple rate limits
type MultiLimiter interface {
	// CheckAll checks all applicable limits
	CheckAll(ctx context.Context, userID, accountID, ip string) (*Result, error)
	// CheckUser checks user limit
	CheckUser(ctx context.Context, userID string) (*Result, error)
	// CheckAccount checks account limit
	CheckAccount(ctx context.Context, accountID string) (*Result, error)
	// CheckIP checks IP limit
	CheckIP(ctx context.Context, ip string) (*Result, error)
	// CheckGlobal checks global limit
	CheckGlobal(ctx context.Context) (*Result, error)
	// Stats returns rate limit statistics
	Stats() LimiterStats
	// Close closes the limiter
	Close()
}

// LimiterStats contains rate limiter statistics
type LimiterStats struct {
	TotalChecks   int64 `json:"total_checks"`
	TotalAllowed  int64 `json:"total_allowed"`
	TotalDenied   int64 `json:"total_denied"`
	ActiveBuckets int   `json:"active_buckets"`
}
