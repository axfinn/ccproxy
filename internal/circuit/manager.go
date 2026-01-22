package circuit

import (
	"sync"

	"github.com/rs/zerolog/log"
)

// Manager manages circuit breakers for multiple accounts
type Manager interface {
	// GetBreaker returns the circuit breaker for an account
	GetBreaker(accountID string) Breaker
	// IsAvailable returns true if the account is available (breaker not open)
	IsAvailable(accountID string) bool
	// GetAvailableAccounts filters accounts to only those available
	GetAvailableAccounts(accountIDs []string) []string
	// RecordSuccess records a successful request for an account
	RecordSuccess(accountID string)
	// RecordFailure records a failed request for an account
	RecordFailure(accountID string)
	// Reset resets the breaker for an account
	Reset(accountID string)
	// Stats returns statistics for all breakers
	Stats() map[string]BreakerStats
	// Close closes the manager
	Close()
}

// breakerManager implements Manager
type breakerManager struct {
	config   BreakerConfig
	breakers map[string]Breaker
	mu       sync.RWMutex
	closed   bool
}

// NewManager creates a new circuit breaker manager
func NewManager(config BreakerConfig) Manager {
	return &breakerManager{
		config:   config,
		breakers: make(map[string]Breaker),
	}
}

// GetBreaker returns the circuit breaker for an account
func (m *breakerManager) GetBreaker(accountID string) Breaker {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		// Return a no-op breaker if closed
		return NewBreaker(BreakerConfig{Enabled: false})
	}

	if breaker, ok := m.breakers[accountID]; ok {
		return breaker
	}

	// Create new breaker
	breaker := NewBreaker(m.config)
	m.breakers[accountID] = breaker

	log.Debug().Str("account_id", accountID).Msg("created new circuit breaker")

	return breaker
}

// IsAvailable returns true if the account is available (breaker not open)
func (m *breakerManager) IsAvailable(accountID string) bool {
	if !m.config.Enabled {
		return true
	}

	breaker := m.GetBreaker(accountID)
	return breaker.Allow()
}

// GetAvailableAccounts filters accounts to only those available
func (m *breakerManager) GetAvailableAccounts(accountIDs []string) []string {
	if !m.config.Enabled {
		return accountIDs
	}

	available := make([]string, 0, len(accountIDs))
	for _, id := range accountIDs {
		if m.IsAvailable(id) {
			available = append(available, id)
		}
	}

	if len(available) < len(accountIDs) {
		log.Debug().
			Int("total", len(accountIDs)).
			Int("available", len(available)).
			Msg("filtered unavailable accounts")
	}

	return available
}

// RecordSuccess records a successful request for an account
func (m *breakerManager) RecordSuccess(accountID string) {
	breaker := m.GetBreaker(accountID)
	breaker.RecordSuccess()
}

// RecordFailure records a failed request for an account
func (m *breakerManager) RecordFailure(accountID string) {
	breaker := m.GetBreaker(accountID)
	prevState := breaker.State()
	breaker.RecordFailure()
	newState := breaker.State()

	if prevState != newState {
		log.Warn().
			Str("account_id", accountID).
			Str("prev_state", prevState.String()).
			Str("new_state", newState.String()).
			Msg("circuit breaker state changed")
	}
}

// Reset resets the breaker for an account
func (m *breakerManager) Reset(accountID string) {
	m.mu.RLock()
	breaker, ok := m.breakers[accountID]
	m.mu.RUnlock()

	if ok {
		breaker.Reset()
		log.Info().Str("account_id", accountID).Msg("circuit breaker reset")
	}
}

// Stats returns statistics for all breakers
func (m *breakerManager) Stats() map[string]BreakerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]BreakerStats, len(m.breakers))
	for id, breaker := range m.breakers {
		stats[id] = breaker.Stats()
	}

	return stats
}

// Close closes the manager
func (m *breakerManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	m.breakers = make(map[string]Breaker)

	log.Info().Msg("circuit breaker manager closed")
}
