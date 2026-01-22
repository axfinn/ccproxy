package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestMemoryLimiter_Allow(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		UserLimit: LimitRule{
			Requests: 5,
			Window:   1 * time.Second,
		},
	}
	limiter := NewMultiMemoryLimiter(config)
	defer limiter.Close()

	ctx := context.Background()

	// Should allow first 5 requests
	for i := 0; i < 5; i++ {
		result, err := limiter.CheckUser(ctx, "user1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Allowed {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 6th request should be denied
	result, err := limiter.CheckUser(ctx, "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("6th request should be denied")
	}

	// Different user should be allowed
	result, err = limiter.CheckUser(ctx, "user2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("different user should be allowed")
	}
}

func TestMemoryLimiter_WindowReset(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		UserLimit: LimitRule{
			Requests: 2,
			Window:   100 * time.Millisecond,
		},
	}
	limiter := NewMultiMemoryLimiter(config)
	defer limiter.Close()

	ctx := context.Background()

	// Exhaust limit
	limiter.CheckUser(ctx, "user1")
	limiter.CheckUser(ctx, "user1")

	result, _ := limiter.CheckUser(ctx, "user1")
	if result.Allowed {
		t.Error("should be denied after exhausting limit")
	}

	// Wait for window to reset
	time.Sleep(150 * time.Millisecond)

	result, _ = limiter.CheckUser(ctx, "user1")
	if !result.Allowed {
		t.Error("should be allowed after window reset")
	}
}

func TestMultiLimiter_CheckAll(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		UserLimit: LimitRule{
			Requests: 10,
			Window:   1 * time.Second,
		},
		AccountLimit: LimitRule{
			Requests: 5,
			Window:   1 * time.Second,
		},
		IPLimit: LimitRule{
			Requests: 20,
			Window:   1 * time.Second,
		},
		GlobalLimit: LimitRule{
			Requests: 100,
			Window:   1 * time.Second,
		},
	}
	limiter := NewMultiMemoryLimiter(config)
	defer limiter.Close()

	ctx := context.Background()

	// Should allow initial requests
	for i := 0; i < 5; i++ {
		result, err := limiter.CheckAll(ctx, "user1", "acc1", "1.2.3.4")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Allowed {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// Next request should be denied (account limit reached)
	result, err := limiter.CheckAll(ctx, "user1", "acc1", "1.2.3.4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("should be denied when account limit reached")
	}
}

func TestLimiter_Stats(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		UserLimit: LimitRule{
			Requests: 10,
			Window:   1 * time.Second,
		},
		AccountLimit: LimitRule{
			Requests: 100,
			Window:   1 * time.Second,
		},
		IPLimit: LimitRule{
			Requests: 100,
			Window:   1 * time.Second,
		},
		GlobalLimit: LimitRule{
			Requests: 1000,
			Window:   1 * time.Second,
		},
	}
	limiter := NewMultiMemoryLimiter(config)
	defer limiter.Close()

	ctx := context.Background()

	// Make some requests using CheckAll (which tracks stats)
	for i := 0; i < 5; i++ {
		limiter.CheckAll(ctx, "user1", "acc1", "1.2.3.4")
	}

	stats := limiter.Stats()
	if stats.TotalChecks != 5 {
		t.Errorf("expected 5 total checks, got %d", stats.TotalChecks)
	}
	if stats.TotalAllowed != 5 {
		t.Errorf("expected 5 total allowed, got %d", stats.TotalAllowed)
	}
}
