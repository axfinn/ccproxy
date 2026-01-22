package circuit

import (
	"sync"
	"time"
)

// State represents the circuit breaker state
type State int

const (
	StateClosed   State = iota // Normal operation, requests allowed
	StateOpen                  // Circuit tripped, requests blocked
	StateHalfOpen              // Testing if service recovered
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// BreakerConfig holds circuit breaker configuration
type BreakerConfig struct {
	FailureThreshold int           `mapstructure:"failure_threshold"` // Failures to open circuit
	SuccessThreshold int           `mapstructure:"success_threshold"` // Successes to close circuit
	OpenTimeout      time.Duration `mapstructure:"open_timeout"`      // Time before half-open
	Enabled          bool          `mapstructure:"enabled"`
}

// DefaultBreakerConfig returns the default breaker configuration
func DefaultBreakerConfig() BreakerConfig {
	return BreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
		Enabled:          true,
	}
}

// Breaker represents a circuit breaker for a single account
type Breaker interface {
	// Allow returns true if a request is allowed
	Allow() bool
	// RecordSuccess records a successful request
	RecordSuccess()
	// RecordFailure records a failed request
	RecordFailure()
	// State returns the current state
	State() State
	// Reset resets the breaker to closed state
	Reset()
	// Stats returns breaker statistics
	Stats() BreakerStats
}

// BreakerStats contains breaker statistics
type BreakerStats struct {
	State            State     `json:"state"`
	ConsecutiveFails int       `json:"consecutive_fails"`
	ConsecutiveOK    int       `json:"consecutive_ok"`
	TotalFailures    int64     `json:"total_failures"`
	TotalSuccesses   int64     `json:"total_successes"`
	LastFailure      time.Time `json:"last_failure,omitempty"`
	LastSuccess      time.Time `json:"last_success,omitempty"`
	OpenedAt         time.Time `json:"opened_at,omitempty"`
}

// circuitBreaker implements Breaker
type circuitBreaker struct {
	config           BreakerConfig
	state            State
	consecutiveFails int
	consecutiveOK    int
	totalFailures    int64
	totalSuccesses   int64
	lastFailure      time.Time
	lastSuccess      time.Time
	openedAt         time.Time
	mu               sync.RWMutex
}

// NewBreaker creates a new circuit breaker
func NewBreaker(config BreakerConfig) Breaker {
	return &circuitBreaker{
		config: config,
		state:  StateClosed,
	}
}

// Allow returns true if a request is allowed
func (b *circuitBreaker) Allow() bool {
	if !b.config.Enabled {
		return true
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		return true

	case StateOpen:
		// Check if enough time has passed to try again
		if time.Since(b.openedAt) >= b.config.OpenTimeout {
			b.state = StateHalfOpen
			b.consecutiveOK = 0
			return true
		}
		return false

	case StateHalfOpen:
		// Allow one request to test
		return true

	default:
		return true
	}
}

// RecordSuccess records a successful request
func (b *circuitBreaker) RecordSuccess() {
	if !b.config.Enabled {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.totalSuccesses++
	b.lastSuccess = time.Now()
	b.consecutiveFails = 0
	b.consecutiveOK++

	switch b.state {
	case StateHalfOpen:
		if b.consecutiveOK >= b.config.SuccessThreshold {
			b.state = StateClosed
			b.consecutiveOK = 0
		}
	case StateOpen:
		// Shouldn't happen, but handle gracefully
		b.state = StateHalfOpen
	}
}

// RecordFailure records a failed request
func (b *circuitBreaker) RecordFailure() {
	if !b.config.Enabled {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.totalFailures++
	b.lastFailure = time.Now()
	b.consecutiveFails++
	b.consecutiveOK = 0

	switch b.state {
	case StateClosed:
		if b.consecutiveFails >= b.config.FailureThreshold {
			b.state = StateOpen
			b.openedAt = time.Now()
		}
	case StateHalfOpen:
		// Any failure in half-open returns to open
		b.state = StateOpen
		b.openedAt = time.Now()
	}
}

// State returns the current state
func (b *circuitBreaker) State() State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// Reset resets the breaker to closed state
func (b *circuitBreaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.state = StateClosed
	b.consecutiveFails = 0
	b.consecutiveOK = 0
}

// Stats returns breaker statistics
func (b *circuitBreaker) Stats() BreakerStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return BreakerStats{
		State:            b.state,
		ConsecutiveFails: b.consecutiveFails,
		ConsecutiveOK:    b.consecutiveOK,
		TotalFailures:    b.totalFailures,
		TotalSuccesses:   b.totalSuccesses,
		LastFailure:      b.lastFailure,
		LastSuccess:      b.lastSuccess,
		OpenedAt:         b.openedAt,
	}
}
