package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestScheduler_SelectAccount(t *testing.T) {
	config := SchedulerConfig{
		StickySessionTTL: 1 * time.Hour,
		Strategy:         StrategyRoundRobin,
	}
	sched := NewScheduler(config, nil, nil)
	defer sched.Close()

	ctx := context.Background()
	accounts := []string{"acc1", "acc2", "acc3"}

	// Should select accounts in round-robin order
	result1, err := sched.SelectAccount(ctx, SelectOptions{AccountIDs: accounts})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result2, err := sched.SelectAccount(ctx, SelectOptions{AccountIDs: accounts})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result3, err := sched.SelectAccount(ctx, SelectOptions{AccountIDs: accounts})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify different accounts were selected
	if result1.AccountID == result2.AccountID && result2.AccountID == result3.AccountID {
		t.Error("expected different accounts in round-robin")
	}
}

func TestScheduler_StickySession(t *testing.T) {
	config := SchedulerConfig{
		StickySessionTTL: 100 * time.Millisecond,
		Strategy:         StrategyRoundRobin,
	}
	sched := NewScheduler(config, nil, nil)
	defer sched.Close()

	ctx := context.Background()
	accounts := []string{"acc1", "acc2", "acc3"}
	sessionHash := "test-session-hash"

	// First request should bind sticky session
	result1, err := sched.SelectAccount(ctx, SelectOptions{
		AccountIDs:  accounts,
		SessionHash: sessionHash,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Subsequent requests with same hash should get same account
	result2, err := sched.SelectAccount(ctx, SelectOptions{
		AccountIDs:  accounts,
		SessionHash: sessionHash,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result1.AccountID != result2.AccountID {
		t.Errorf("expected same account for sticky session, got %s and %s",
			result1.AccountID, result2.AccountID)
	}

	if !result2.FromSticky {
		t.Error("expected FromSticky to be true")
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Should not get sticky result after TTL
	result3, err := sched.SelectAccount(ctx, SelectOptions{
		AccountIDs:  accounts,
		SessionHash: sessionHash,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// FromSticky should be false (but it might get same account by chance)
	if result3.FromSticky {
		t.Error("expected FromSticky to be false after TTL")
	}
}

func TestScheduler_SelectWithRetry(t *testing.T) {
	config := SchedulerConfig{
		StickySessionTTL: 1 * time.Hour,
		Strategy:         StrategyRoundRobin,
	}
	sched := NewScheduler(config, nil, nil)
	defer sched.Close()

	ctx := context.Background()
	accounts := []string{"acc1", "acc2", "acc3"}

	// Select first account
	result1, err := sched.SelectAccountWithRetry(ctx, SelectOptions{AccountIDs: accounts}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Exclude first account and select again
	excludeIDs := []string{result1.AccountID}
	result2, err := sched.SelectAccountWithRetry(ctx, SelectOptions{AccountIDs: accounts}, excludeIDs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result2.AccountID == result1.AccountID {
		t.Error("should have selected different account when excluding first one")
	}
}

func TestScheduler_NoAccounts(t *testing.T) {
	config := SchedulerConfig{
		StickySessionTTL: 1 * time.Hour,
		Strategy:         StrategyRoundRobin,
	}
	sched := NewScheduler(config, nil, nil)
	defer sched.Close()

	ctx := context.Background()

	_, err := sched.SelectAccount(ctx, SelectOptions{AccountIDs: []string{}})
	if err == nil {
		t.Error("expected error when no accounts available")
	}
}

func TestGenerateStickyHash(t *testing.T) {
	// With user ID
	hash1 := GenerateStickyHash(StickyHashOptions{
		UserID: "user123",
	})
	if hash1 == "" {
		t.Error("expected non-empty hash with user ID")
	}

	// Same user ID should produce same hash
	hash2 := GenerateStickyHash(StickyHashOptions{
		UserID: "user123",
	})
	if hash1 != hash2 {
		t.Error("same user ID should produce same hash")
	}

	// With system prompt
	hash3 := GenerateStickyHash(StickyHashOptions{
		SystemPrompt: "You are a helpful assistant",
	})
	if hash3 == "" {
		t.Error("expected non-empty hash with system prompt")
	}

	// Different inputs should produce different hashes
	if hash1 == hash3 {
		t.Error("different inputs should produce different hashes")
	}

	// Empty options should produce empty hash
	hash4 := GenerateStickyHash(StickyHashOptions{})
	if hash4 != "" {
		t.Error("expected empty hash with no inputs")
	}
}
