package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"ccproxy/internal/circuit"
	"ccproxy/internal/concurrency"
)

// SchedulerConfig holds scheduler configuration
type SchedulerConfig struct {
	StickySessionTTL time.Duration `mapstructure:"sticky_session_ttl"`
	Strategy         Strategy      `mapstructure:"strategy"`
}

// Strategy defines the scheduling strategy
type Strategy string

const (
	StrategyLeastLoaded Strategy = "least_loaded"
	StrategyRoundRobin  Strategy = "round_robin"
	StrategyRandom      Strategy = "random"
)

// DefaultSchedulerConfig returns the default scheduler configuration
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		StickySessionTTL: 1 * time.Hour,
		Strategy:         StrategyLeastLoaded,
	}
}

// SelectOptions contains options for account selection
type SelectOptions struct {
	AccountIDs  []string // Available account IDs
	SessionHash string   // Session hash for sticky sessions
	UserID      string   // User ID for load consideration
}

// SelectionResult contains the result of account selection
type SelectionResult struct {
	AccountID   string        // Selected account ID
	FromSticky  bool          // Whether selected from sticky session
	LoadScore   int           // Load score of selected account
	SelectTime  time.Duration // Time taken to select
}

// Scheduler selects accounts for requests
type Scheduler interface {
	// SelectAccount selects an account for a request
	SelectAccount(ctx context.Context, opts SelectOptions) (*SelectionResult, error)
	// SelectAccountWithRetry selects an account excluding certain IDs
	SelectAccountWithRetry(ctx context.Context, opts SelectOptions, excludeIDs []string) (*SelectionResult, error)
	// BindStickySession binds a session hash to an account
	BindStickySession(ctx context.Context, sessionHash, accountID string) error
	// GetStickyAccount returns the sticky account for a session hash
	GetStickyAccount(ctx context.Context, sessionHash string) (string, bool)
	// Stats returns scheduler statistics
	Stats() SchedulerStats
	// Close closes the scheduler
	Close()
}

// SchedulerStats contains scheduler statistics
type SchedulerStats struct {
	TotalSelections    int64 `json:"total_selections"`
	StickyHits         int64 `json:"sticky_hits"`
	StickyMisses       int64 `json:"sticky_misses"`
	NoAccountAvailable int64 `json:"no_account_available"`
	ActiveStickySessions int `json:"active_sticky_sessions"`
}

// stickyEntry represents a sticky session binding
type stickyEntry struct {
	accountID string
	createdAt time.Time
	expiresAt time.Time
}

// scheduler implements Scheduler
type scheduler struct {
	config       SchedulerConfig
	circuitMgr   circuit.Manager
	concurrency  concurrency.Manager

	stickySessions map[string]*stickyEntry
	roundRobinIdx  int
	mu             sync.RWMutex

	totalSelections    int64
	stickyHits         int64
	stickyMisses       int64
	noAccountAvailable int64

	closed bool
}

// NewScheduler creates a new scheduler
func NewScheduler(config SchedulerConfig, circuitMgr circuit.Manager, concurrencyMgr concurrency.Manager) Scheduler {
	s := &scheduler{
		config:         config,
		circuitMgr:     circuitMgr,
		concurrency:    concurrencyMgr,
		stickySessions: make(map[string]*stickyEntry),
	}

	// Start cleanup goroutine
	go s.cleanup()

	return s
}

// SelectAccount selects an account for a request
func (s *scheduler) SelectAccount(ctx context.Context, opts SelectOptions) (*SelectionResult, error) {
	return s.SelectAccountWithRetry(ctx, opts, nil)
}

// SelectAccountWithRetry selects an account excluding certain IDs
func (s *scheduler) SelectAccountWithRetry(ctx context.Context, opts SelectOptions, excludeIDs []string) (*SelectionResult, error) {
	start := time.Now()
	s.mu.Lock()
	s.totalSelections++
	s.mu.Unlock()

	// Filter out excluded IDs
	availableIDs := s.filterExcluded(opts.AccountIDs, excludeIDs)

	// Filter out circuit-broken accounts
	if s.circuitMgr != nil {
		availableIDs = s.circuitMgr.GetAvailableAccounts(availableIDs)
	}

	if len(availableIDs) == 0 {
		s.mu.Lock()
		s.noAccountAvailable++
		s.mu.Unlock()
		return nil, fmt.Errorf("no available accounts")
	}

	// Check sticky session
	if opts.SessionHash != "" {
		if accountID, ok := s.GetStickyAccount(ctx, opts.SessionHash); ok {
			// Verify account is still available
			if s.contains(availableIDs, accountID) {
				s.mu.Lock()
				s.stickyHits++
				s.mu.Unlock()

				return &SelectionResult{
					AccountID:  accountID,
					FromSticky: true,
					SelectTime: time.Since(start),
				}, nil
			}
		}
		s.mu.Lock()
		s.stickyMisses++
		s.mu.Unlock()
	}

	// Select based on strategy
	var accountID string
	var loadScore int

	switch s.config.Strategy {
	case StrategyLeastLoaded:
		accountID, loadScore = s.selectLeastLoaded(availableIDs)
	case StrategyRoundRobin:
		accountID = s.selectRoundRobin(availableIDs)
	case StrategyRandom:
		accountID = s.selectRandom(availableIDs)
	default:
		accountID, loadScore = s.selectLeastLoaded(availableIDs)
	}

	// Bind sticky session if hash provided
	if opts.SessionHash != "" && accountID != "" {
		_ = s.BindStickySession(ctx, opts.SessionHash, accountID)
	}

	return &SelectionResult{
		AccountID:  accountID,
		FromSticky: false,
		LoadScore:  loadScore,
		SelectTime: time.Since(start),
	}, nil
}

// BindStickySession binds a session hash to an account
func (s *scheduler) BindStickySession(ctx context.Context, sessionHash, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.stickySessions[sessionHash] = &stickyEntry{
		accountID: accountID,
		createdAt: now,
		expiresAt: now.Add(s.config.StickySessionTTL),
	}

	log.Debug().
		Str("session_hash", sessionHash[:8]).
		Str("account_id", accountID).
		Msg("bound sticky session")

	return nil
}

// GetStickyAccount returns the sticky account for a session hash
func (s *scheduler) GetStickyAccount(ctx context.Context, sessionHash string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.stickySessions[sessionHash]
	if !ok {
		return "", false
	}

	// Check expiration
	if time.Now().After(entry.expiresAt) {
		return "", false
	}

	return entry.accountID, true
}

// Stats returns scheduler statistics
func (s *scheduler) Stats() SchedulerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return SchedulerStats{
		TotalSelections:      s.totalSelections,
		StickyHits:           s.stickyHits,
		StickyMisses:         s.stickyMisses,
		NoAccountAvailable:   s.noAccountAvailable,
		ActiveStickySessions: len(s.stickySessions),
	}
}

// Close closes the scheduler
func (s *scheduler) Close() {
	s.mu.Lock()
	s.closed = true
	s.stickySessions = make(map[string]*stickyEntry)
	s.mu.Unlock()

	log.Info().Msg("scheduler closed")
}

// selectLeastLoaded selects the account with lowest load
func (s *scheduler) selectLeastLoaded(accountIDs []string) (string, int) {
	if s.concurrency == nil {
		// Fallback to round robin
		return s.selectRoundRobin(accountIDs), 0
	}

	return s.concurrency.GetLowestLoadAccount(accountIDs), 0
}

// selectRoundRobin selects the next account in round-robin order
func (s *scheduler) selectRoundRobin(accountIDs []string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(accountIDs) == 0 {
		return ""
	}

	idx := s.roundRobinIdx % len(accountIDs)
	s.roundRobinIdx++

	return accountIDs[idx]
}

// selectRandom selects a random account
func (s *scheduler) selectRandom(accountIDs []string) string {
	if len(accountIDs) == 0 {
		return ""
	}

	// Use a simple hash of current time for randomness
	idx := int(time.Now().UnixNano()) % len(accountIDs)
	if idx < 0 {
		idx = -idx
	}

	return accountIDs[idx]
}

// filterExcluded removes excluded IDs from the list
func (s *scheduler) filterExcluded(accountIDs, excludeIDs []string) []string {
	if len(excludeIDs) == 0 {
		return accountIDs
	}

	excludeSet := make(map[string]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		excludeSet[id] = true
	}

	result := make([]string, 0, len(accountIDs))
	for _, id := range accountIDs {
		if !excludeSet[id] {
			result = append(result, id)
		}
	}

	return result
}

// contains checks if a slice contains a value
func (s *scheduler) contains(slice []string, val string) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

// cleanup periodically removes expired sticky sessions
func (s *scheduler) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}

		now := time.Now()
		var expired []string

		for hash, entry := range s.stickySessions {
			if now.After(entry.expiresAt) {
				expired = append(expired, hash)
			}
		}

		for _, hash := range expired {
			delete(s.stickySessions, hash)
		}

		if len(expired) > 0 {
			log.Debug().Int("expired", len(expired)).Msg("cleaned up sticky sessions")
		}

		s.mu.Unlock()
	}
}
