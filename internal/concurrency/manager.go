package concurrency

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// ConcurrencyConfig holds concurrency control configuration
type ConcurrencyConfig struct {
	UserMax       int           `mapstructure:"user_max"`        // Max concurrent requests per user
	AccountMax    int           `mapstructure:"account_max"`     // Max concurrent requests per account
	MaxWaitQueue  int           `mapstructure:"max_wait_queue"`  // Max waiting requests
	WaitTimeout   time.Duration `mapstructure:"wait_timeout"`    // Max time to wait for slot
	BackoffBase   time.Duration `mapstructure:"backoff_base"`    // Initial backoff duration
	BackoffMax    time.Duration `mapstructure:"backoff_max"`     // Maximum backoff duration
	BackoffJitter float64       `mapstructure:"backoff_jitter"`  // Jitter factor (0-1)
	PingInterval  time.Duration `mapstructure:"ping_interval"`   // SSE ping interval while waiting
}

// DefaultConcurrencyConfig returns the default concurrency configuration
func DefaultConcurrencyConfig() ConcurrencyConfig {
	return ConcurrencyConfig{
		UserMax:       10,
		AccountMax:    5,
		MaxWaitQueue:  20,
		WaitTimeout:   30 * time.Second,
		BackoffBase:   100 * time.Millisecond,
		BackoffMax:    2 * time.Second,
		BackoffJitter: 0.2,
		PingInterval:  5 * time.Second,
	}
}

// AcquireResult contains the result of acquiring a slot
type AcquireResult struct {
	Acquired bool          // Whether slot was acquired
	WaitTime time.Duration // Time spent waiting
	QueuePos int           // Position in queue (0 if acquired)
}

// LoadInfo contains load information for an entity
type LoadInfo struct {
	Current  int   `json:"current"`   // Current concurrent requests
	Max      int   `json:"max"`       // Maximum allowed
	Waiting  int   `json:"waiting"`   // Requests waiting
	Total    int64 `json:"total"`     // Total requests processed
}

// Manager manages concurrency limits
type Manager interface {
	// AcquireUserSlot acquires a slot for a user
	AcquireUserSlot(ctx context.Context, userID string) (*AcquireResult, error)
	// ReleaseUserSlot releases a user slot
	ReleaseUserSlot(userID string)
	// AcquireAccountSlot acquires a slot for an account
	AcquireAccountSlot(ctx context.Context, accountID string) (*AcquireResult, error)
	// ReleaseAccountSlot releases an account slot
	ReleaseAccountSlot(accountID string)
	// GetUserLoad returns load info for a user
	GetUserLoad(userID string) *LoadInfo
	// GetAccountLoad returns load info for accounts
	GetAccountLoad(accountIDs []string) map[string]*LoadInfo
	// GetLowestLoadAccount returns the account with lowest load
	GetLowestLoadAccount(accountIDs []string) string
	// Stats returns overall statistics
	Stats() ManagerStats
	// Close closes the manager
	Close()
}

// ManagerStats contains overall statistics
type ManagerStats struct {
	TotalUsers      int   `json:"total_users"`
	TotalAccounts   int   `json:"total_accounts"`
	ActiveUserSlots int   `json:"active_user_slots"`
	ActiveAcctSlots int   `json:"active_account_slots"`
	WaitingUsers    int   `json:"waiting_users"`
	WaitingAccounts int   `json:"waiting_accounts"`
	TotalAcquires   int64 `json:"total_acquires"`
	TotalTimeouts   int64 `json:"total_timeouts"`
}

// slot tracks concurrency for a single entity
type slot struct {
	current  int32
	max      int32
	waiting  int32
	total    int64
	mu       sync.Mutex
	cond     *sync.Cond
}

func newSlot(max int) *slot {
	s := &slot{
		max: int32(max),
	}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// concurrencyManager implements Manager
type concurrencyManager struct {
	config        ConcurrencyConfig
	userSlots     map[string]*slot
	accountSlots  map[string]*slot
	userMu        sync.RWMutex
	accountMu     sync.RWMutex
	totalAcquires int64
	totalTimeouts int64
	closed        bool
	closeMu       sync.RWMutex
}

// NewManager creates a new concurrency manager
func NewManager(config ConcurrencyConfig) Manager {
	return &concurrencyManager{
		config:       config,
		userSlots:    make(map[string]*slot),
		accountSlots: make(map[string]*slot),
	}
}

// AcquireUserSlot acquires a slot for a user
func (m *concurrencyManager) AcquireUserSlot(ctx context.Context, userID string) (*AcquireResult, error) {
	m.closeMu.RLock()
	if m.closed {
		m.closeMu.RUnlock()
		return nil, fmt.Errorf("manager closed")
	}
	m.closeMu.RUnlock()

	slot := m.getOrCreateUserSlot(userID)
	return m.acquireSlot(ctx, slot, "user", userID)
}

// ReleaseUserSlot releases a user slot
func (m *concurrencyManager) ReleaseUserSlot(userID string) {
	m.userMu.RLock()
	slot, ok := m.userSlots[userID]
	m.userMu.RUnlock()

	if ok {
		m.releaseSlot(slot)
	}
}

// AcquireAccountSlot acquires a slot for an account
func (m *concurrencyManager) AcquireAccountSlot(ctx context.Context, accountID string) (*AcquireResult, error) {
	m.closeMu.RLock()
	if m.closed {
		m.closeMu.RUnlock()
		return nil, fmt.Errorf("manager closed")
	}
	m.closeMu.RUnlock()

	slot := m.getOrCreateAccountSlot(accountID)
	return m.acquireSlot(ctx, slot, "account", accountID)
}

// ReleaseAccountSlot releases an account slot
func (m *concurrencyManager) ReleaseAccountSlot(accountID string) {
	m.accountMu.RLock()
	slot, ok := m.accountSlots[accountID]
	m.accountMu.RUnlock()

	if ok {
		m.releaseSlot(slot)
	}
}

// getOrCreateUserSlot gets or creates a user slot
func (m *concurrencyManager) getOrCreateUserSlot(userID string) *slot {
	m.userMu.RLock()
	slot, ok := m.userSlots[userID]
	m.userMu.RUnlock()

	if ok {
		return slot
	}

	m.userMu.Lock()
	defer m.userMu.Unlock()

	// Double-check after lock
	if slot, ok := m.userSlots[userID]; ok {
		return slot
	}

	slot = newSlot(m.config.UserMax)
	m.userSlots[userID] = slot
	return slot
}

// getOrCreateAccountSlot gets or creates an account slot
func (m *concurrencyManager) getOrCreateAccountSlot(accountID string) *slot {
	m.accountMu.RLock()
	slot, ok := m.accountSlots[accountID]
	m.accountMu.RUnlock()

	if ok {
		return slot
	}

	m.accountMu.Lock()
	defer m.accountMu.Unlock()

	// Double-check after lock
	if slot, ok := m.accountSlots[accountID]; ok {
		return slot
	}

	slot = newSlot(m.config.AccountMax)
	m.accountSlots[accountID] = slot
	return slot
}

// acquireSlot attempts to acquire a slot with backoff
func (m *concurrencyManager) acquireSlot(ctx context.Context, s *slot, slotType, id string) (*AcquireResult, error) {
	start := time.Now()
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = start.Add(m.config.WaitTimeout)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Try immediate acquire
	if s.current < s.max {
		s.current++
		atomic.AddInt64(&s.total, 1)
		atomic.AddInt64(&m.totalAcquires, 1)
		return &AcquireResult{
			Acquired: true,
			WaitTime: 0,
		}, nil
	}

	// Check if queue is full
	if int(s.waiting) >= m.config.MaxWaitQueue {
		log.Warn().
			Str("type", slotType).
			Str("id", id).
			Int32("waiting", s.waiting).
			Msg("wait queue full")
		return &AcquireResult{
			Acquired: false,
			QueuePos: int(s.waiting),
		}, fmt.Errorf("wait queue full")
	}

	// Wait with exponential backoff
	s.waiting++
	queuePos := int(s.waiting)
	backoff := m.config.BackoffBase

	log.Debug().
		Str("type", slotType).
		Str("id", id).
		Int("queue_pos", queuePos).
		Msg("waiting for slot")

	for {
		// Wait with timeout
		waitCtx, cancel := context.WithTimeout(ctx, backoff)
		done := make(chan struct{})

		go func() {
			s.cond.Wait()
			close(done)
		}()

		select {
		case <-done:
			cancel()
			// Woken up, try to acquire
			if s.current < s.max {
				s.current++
				s.waiting--
				atomic.AddInt64(&s.total, 1)
				atomic.AddInt64(&m.totalAcquires, 1)
				return &AcquireResult{
					Acquired: true,
					WaitTime: time.Since(start),
				}, nil
			}

		case <-waitCtx.Done():
			cancel()
			// Timeout on this iteration, check deadline
			if time.Now().After(deadline) {
				s.waiting--
				atomic.AddInt64(&m.totalTimeouts, 1)
				log.Warn().
					Str("type", slotType).
					Str("id", id).
					Dur("waited", time.Since(start)).
					Msg("timeout waiting for slot")
				return &AcquireResult{
					Acquired: false,
					WaitTime: time.Since(start),
				}, fmt.Errorf("timeout waiting for %s slot", slotType)
			}

		case <-ctx.Done():
			s.waiting--
			return &AcquireResult{
				Acquired: false,
				WaitTime: time.Since(start),
			}, ctx.Err()
		}

		// Exponential backoff with jitter
		backoff = m.nextBackoff(backoff)
	}
}

// releaseSlot releases a slot and signals waiters
func (m *concurrencyManager) releaseSlot(s *slot) {
	s.mu.Lock()
	if s.current > 0 {
		s.current--
	}
	s.mu.Unlock()
	s.cond.Signal()
}

// nextBackoff calculates the next backoff duration with jitter
func (m *concurrencyManager) nextBackoff(current time.Duration) time.Duration {
	next := time.Duration(float64(current) * 2)
	if next > m.config.BackoffMax {
		next = m.config.BackoffMax
	}

	// Add jitter
	jitter := time.Duration(float64(next) * m.config.BackoffJitter)
	next = next - jitter + time.Duration(float64(jitter*2)*0.5) // Simplified random-ish jitter

	return next
}

// GetUserLoad returns load info for a user
func (m *concurrencyManager) GetUserLoad(userID string) *LoadInfo {
	m.userMu.RLock()
	slot, ok := m.userSlots[userID]
	m.userMu.RUnlock()

	if !ok {
		return &LoadInfo{
			Current: 0,
			Max:     m.config.UserMax,
			Waiting: 0,
			Total:   0,
		}
	}

	return &LoadInfo{
		Current: int(atomic.LoadInt32(&slot.current)),
		Max:     int(slot.max),
		Waiting: int(atomic.LoadInt32(&slot.waiting)),
		Total:   atomic.LoadInt64(&slot.total),
	}
}

// GetAccountLoad returns load info for accounts
func (m *concurrencyManager) GetAccountLoad(accountIDs []string) map[string]*LoadInfo {
	result := make(map[string]*LoadInfo, len(accountIDs))

	m.accountMu.RLock()
	defer m.accountMu.RUnlock()

	for _, id := range accountIDs {
		if slot, ok := m.accountSlots[id]; ok {
			result[id] = &LoadInfo{
				Current: int(atomic.LoadInt32(&slot.current)),
				Max:     int(slot.max),
				Waiting: int(atomic.LoadInt32(&slot.waiting)),
				Total:   atomic.LoadInt64(&slot.total),
			}
		} else {
			result[id] = &LoadInfo{
				Current: 0,
				Max:     m.config.AccountMax,
				Waiting: 0,
				Total:   0,
			}
		}
	}

	return result
}

// GetLowestLoadAccount returns the account with lowest load
func (m *concurrencyManager) GetLowestLoadAccount(accountIDs []string) string {
	if len(accountIDs) == 0 {
		return ""
	}

	loads := m.GetAccountLoad(accountIDs)

	var lowestID string
	lowestLoad := int(^uint(0) >> 1) // Max int

	for id, info := range loads {
		// Calculate load score (current + waiting)
		load := info.Current + info.Waiting
		if load < lowestLoad {
			lowestLoad = load
			lowestID = id
		}
	}

	return lowestID
}

// Stats returns overall statistics
func (m *concurrencyManager) Stats() ManagerStats {
	m.userMu.RLock()
	userCount := len(m.userSlots)
	var activeUserSlots, waitingUsers int
	for _, s := range m.userSlots {
		activeUserSlots += int(atomic.LoadInt32(&s.current))
		waitingUsers += int(atomic.LoadInt32(&s.waiting))
	}
	m.userMu.RUnlock()

	m.accountMu.RLock()
	accountCount := len(m.accountSlots)
	var activeAcctSlots, waitingAccounts int
	for _, s := range m.accountSlots {
		activeAcctSlots += int(atomic.LoadInt32(&s.current))
		waitingAccounts += int(atomic.LoadInt32(&s.waiting))
	}
	m.accountMu.RUnlock()

	return ManagerStats{
		TotalUsers:      userCount,
		TotalAccounts:   accountCount,
		ActiveUserSlots: activeUserSlots,
		ActiveAcctSlots: activeAcctSlots,
		WaitingUsers:    waitingUsers,
		WaitingAccounts: waitingAccounts,
		TotalAcquires:   atomic.LoadInt64(&m.totalAcquires),
		TotalTimeouts:   atomic.LoadInt64(&m.totalTimeouts),
	}
}

// Close closes the manager
func (m *concurrencyManager) Close() {
	m.closeMu.Lock()
	m.closed = true
	m.closeMu.Unlock()

	// Wake up all waiters
	m.userMu.Lock()
	for _, s := range m.userSlots {
		s.cond.Broadcast()
	}
	m.userMu.Unlock()

	m.accountMu.Lock()
	for _, s := range m.accountSlots {
		s.cond.Broadcast()
	}
	m.accountMu.Unlock()

	log.Info().Msg("concurrency manager closed")
}
