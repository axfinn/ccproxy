package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// bucket represents a sliding window bucket
type bucket struct {
	count    int64
	windowID int64 // Unix timestamp of window start
	mu       sync.Mutex
}

// memoryLimiter implements Limiter using sliding window in memory
type memoryLimiter struct {
	rule    LimitRule
	buckets map[string]*bucket
	mu      sync.RWMutex
}

// newMemoryLimiter creates a new memory-based limiter
func newMemoryLimiter(rule LimitRule) *memoryLimiter {
	return &memoryLimiter{
		rule:    rule,
		buckets: make(map[string]*bucket),
	}
}

// Allow checks if a request is allowed
func (l *memoryLimiter) Allow(ctx context.Context, key string) (*Result, error) {
	if l.rule.Requests <= 0 || l.rule.Window <= 0 {
		return &Result{
			Allowed:   true,
			Remaining: -1,
			Limit:     -1,
		}, nil
	}

	l.mu.Lock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{}
		l.buckets[key] = b
	}
	l.mu.Unlock()

	now := time.Now()
	windowID := now.UnixNano() / int64(l.rule.Window)

	b.mu.Lock()
	defer b.mu.Unlock()

	// Reset if new window
	if b.windowID != windowID {
		b.count = 0
		b.windowID = windowID
	}

	// Check limit
	if b.count >= int64(l.rule.Requests) {
		windowStart := time.Unix(0, windowID*int64(l.rule.Window))
		resetAt := windowStart.Add(l.rule.Window)
		retryAt := resetAt

		return &Result{
			Allowed:   false,
			Remaining: 0,
			ResetAt:   resetAt,
			RetryAt:   &retryAt,
			Limit:     l.rule.Requests,
			Window:    l.rule.Window,
		}, nil
	}

	// Allow and increment
	b.count++
	remaining := l.rule.Requests - int(b.count)
	windowStart := time.Unix(0, windowID*int64(l.rule.Window))
	resetAt := windowStart.Add(l.rule.Window)

	return &Result{
		Allowed:   true,
		Remaining: remaining,
		ResetAt:   resetAt,
		Limit:     l.rule.Requests,
		Window:    l.rule.Window,
	}, nil
}

// Reset resets the limit for a key
func (l *memoryLimiter) Reset(ctx context.Context, key string) error {
	l.mu.Lock()
	delete(l.buckets, key)
	l.mu.Unlock()
	return nil
}

// MultiMemoryLimiter implements MultiLimiter using memory
type MultiMemoryLimiter struct {
	config        RateLimitConfig
	userLimiter   *memoryLimiter
	acctLimiter   *memoryLimiter
	ipLimiter     *memoryLimiter
	globalLimiter *memoryLimiter

	totalChecks  int64
	totalAllowed int64
	totalDenied  int64

	closed bool
	mu     sync.RWMutex
}

// NewMultiMemoryLimiter creates a new multi-level memory limiter
func NewMultiMemoryLimiter(config RateLimitConfig) MultiLimiter {
	m := &MultiMemoryLimiter{
		config:        config,
		userLimiter:   newMemoryLimiter(config.UserLimit),
		acctLimiter:   newMemoryLimiter(config.AccountLimit),
		ipLimiter:     newMemoryLimiter(config.IPLimit),
		globalLimiter: newMemoryLimiter(config.GlobalLimit),
	}

	// Start cleanup goroutine
	go m.cleanup()

	return m
}

// CheckAll checks all applicable limits
func (m *MultiMemoryLimiter) CheckAll(ctx context.Context, userID, accountID, ip string) (*Result, error) {
	if !m.config.Enabled {
		return &Result{Allowed: true, Remaining: -1}, nil
	}

	atomic.AddInt64(&m.totalChecks, 1)

	// Check global first
	if result, err := m.CheckGlobal(ctx); err != nil || !result.Allowed {
		if result != nil && !result.Allowed {
			atomic.AddInt64(&m.totalDenied, 1)
		}
		return result, err
	}

	// Check user
	if userID != "" {
		if result, err := m.CheckUser(ctx, userID); err != nil || !result.Allowed {
			if result != nil && !result.Allowed {
				atomic.AddInt64(&m.totalDenied, 1)
			}
			return result, err
		}
	}

	// Check account
	if accountID != "" {
		if result, err := m.CheckAccount(ctx, accountID); err != nil || !result.Allowed {
			if result != nil && !result.Allowed {
				atomic.AddInt64(&m.totalDenied, 1)
			}
			return result, err
		}
	}

	// Check IP
	if ip != "" {
		if result, err := m.CheckIP(ctx, ip); err != nil || !result.Allowed {
			if result != nil && !result.Allowed {
				atomic.AddInt64(&m.totalDenied, 1)
			}
			return result, err
		}
	}

	atomic.AddInt64(&m.totalAllowed, 1)
	return &Result{Allowed: true, Remaining: -1}, nil
}

// CheckUser checks user limit
func (m *MultiMemoryLimiter) CheckUser(ctx context.Context, userID string) (*Result, error) {
	return m.userLimiter.Allow(ctx, "user:"+userID)
}

// CheckAccount checks account limit
func (m *MultiMemoryLimiter) CheckAccount(ctx context.Context, accountID string) (*Result, error) {
	return m.acctLimiter.Allow(ctx, "account:"+accountID)
}

// CheckIP checks IP limit
func (m *MultiMemoryLimiter) CheckIP(ctx context.Context, ip string) (*Result, error) {
	return m.ipLimiter.Allow(ctx, "ip:"+ip)
}

// CheckGlobal checks global limit
func (m *MultiMemoryLimiter) CheckGlobal(ctx context.Context) (*Result, error) {
	return m.globalLimiter.Allow(ctx, "global")
}

// Stats returns rate limiter statistics
func (m *MultiMemoryLimiter) Stats() LimiterStats {
	m.userLimiter.mu.RLock()
	userBuckets := len(m.userLimiter.buckets)
	m.userLimiter.mu.RUnlock()

	m.acctLimiter.mu.RLock()
	acctBuckets := len(m.acctLimiter.buckets)
	m.acctLimiter.mu.RUnlock()

	m.ipLimiter.mu.RLock()
	ipBuckets := len(m.ipLimiter.buckets)
	m.ipLimiter.mu.RUnlock()

	return LimiterStats{
		TotalChecks:   atomic.LoadInt64(&m.totalChecks),
		TotalAllowed:  atomic.LoadInt64(&m.totalAllowed),
		TotalDenied:   atomic.LoadInt64(&m.totalDenied),
		ActiveBuckets: userBuckets + acctBuckets + ipBuckets + 1, // +1 for global
	}
}

// Close closes the limiter
func (m *MultiMemoryLimiter) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()

	log.Info().Msg("rate limiter closed")
}

// cleanup periodically removes old buckets
func (m *MultiMemoryLimiter) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.RLock()
		if m.closed {
			m.mu.RUnlock()
			return
		}
		m.mu.RUnlock()

		m.cleanupLimiter(m.userLimiter)
		m.cleanupLimiter(m.acctLimiter)
		m.cleanupLimiter(m.ipLimiter)
		m.cleanupLimiter(m.globalLimiter)
	}
}

// cleanupLimiter removes old buckets from a limiter
func (m *MultiMemoryLimiter) cleanupLimiter(l *memoryLimiter) {
	if l.rule.Window <= 0 {
		return
	}

	now := time.Now()
	currentWindowID := now.UnixNano() / int64(l.rule.Window)

	l.mu.Lock()
	defer l.mu.Unlock()

	for key, b := range l.buckets {
		b.mu.Lock()
		// Remove if from a previous window
		if b.windowID < currentWindowID-1 {
			delete(l.buckets, key)
		}
		b.mu.Unlock()
	}
}
