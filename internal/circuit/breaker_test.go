package circuit

import (
	"testing"
	"time"
)

func TestBreaker_Allow(t *testing.T) {
	config := BreakerConfig{
		Enabled:          true,
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenTimeout:      100 * time.Millisecond,
	}
	breaker := NewBreaker(config)

	// Initial state should be closed
	if breaker.State() != StateClosed {
		t.Errorf("expected state Closed, got %v", breaker.State())
	}

	// Should allow requests initially
	if !breaker.Allow() {
		t.Error("expected Allow() to return true initially")
	}

	// Record failures to trip breaker
	for i := 0; i < 3; i++ {
		breaker.RecordFailure()
	}

	// Should be open now
	if breaker.State() != StateOpen {
		t.Errorf("expected state Open after failures, got %v", breaker.State())
	}

	// Should not allow requests when open
	if breaker.Allow() {
		t.Error("expected Allow() to return false when open")
	}

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Should transition to half-open
	if !breaker.Allow() {
		t.Error("expected Allow() to return true after timeout")
	}
	if breaker.State() != StateHalfOpen {
		t.Errorf("expected state HalfOpen after timeout, got %v", breaker.State())
	}

	// Record successes to close
	breaker.RecordSuccess()
	breaker.RecordSuccess()

	// Should be closed now
	if breaker.State() != StateClosed {
		t.Errorf("expected state Closed after successes, got %v", breaker.State())
	}
}

func TestBreaker_Reset(t *testing.T) {
	config := BreakerConfig{
		Enabled:          true,
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenTimeout:      1 * time.Second,
	}
	breaker := NewBreaker(config)

	// Trip the breaker
	breaker.RecordFailure()
	breaker.RecordFailure()

	if breaker.State() != StateOpen {
		t.Errorf("expected state Open, got %v", breaker.State())
	}

	// Reset
	breaker.Reset()

	if breaker.State() != StateClosed {
		t.Errorf("expected state Closed after reset, got %v", breaker.State())
	}
}

func TestManager_GetAvailableAccounts(t *testing.T) {
	config := BreakerConfig{
		Enabled:          true,
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenTimeout:      1 * time.Second,
	}
	mgr := NewManager(config)
	defer mgr.Close()

	accounts := []string{"acc1", "acc2", "acc3"}

	// Initially all should be available
	available := mgr.GetAvailableAccounts(accounts)
	if len(available) != 3 {
		t.Errorf("expected 3 available accounts, got %d", len(available))
	}

	// Trip acc2's breaker
	mgr.RecordFailure("acc2")
	mgr.RecordFailure("acc2")

	// Now only acc1 and acc3 should be available
	available = mgr.GetAvailableAccounts(accounts)
	if len(available) != 2 {
		t.Errorf("expected 2 available accounts, got %d", len(available))
	}
}

func TestManager_Disabled(t *testing.T) {
	config := BreakerConfig{
		Enabled: false,
	}
	mgr := NewManager(config)
	defer mgr.Close()

	// Should always return available even with failures
	for i := 0; i < 100; i++ {
		mgr.RecordFailure("acc1")
	}

	if !mgr.IsAvailable("acc1") {
		t.Error("expected account to be available when breaker is disabled")
	}
}
