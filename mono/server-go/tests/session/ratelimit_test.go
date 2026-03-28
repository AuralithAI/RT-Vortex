package session_test

import (
	"testing"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/session"
)

// Tests for the rate limiter that don't require Redis.

func TestNewRateLimiter_NilRedis(t *testing.T) {
	// Creating a limiter with nil Redis should not panic.
	rl := session.NewRateLimiter(nil)
	if rl == nil {
		t.Fatal("expected non-nil rate limiter")
	}
}

func TestRateLimiter_Configure(t *testing.T) {
	rl := session.NewRateLimiter(nil)
	rl.Configure("api", session.RateLimitConfig{
		MaxRequests: 100,
		Window:      time.Minute,
	})
	// No error expected — just verifying it doesn't panic.
}

func TestRateLimitConfig_Fields(t *testing.T) {
	cfg := session.RateLimitConfig{
		MaxRequests: 50,
		Window:      30 * time.Second,
	}

	if cfg.MaxRequests != 50 {
		t.Errorf("expected 50, got %d", cfg.MaxRequests)
	}
	if cfg.Window != 30*time.Second {
		t.Errorf("expected 30s, got %v", cfg.Window)
	}
}

func TestRateLimitResult_Fields(t *testing.T) {
	now := time.Now()
	r := session.RateLimitResult{
		Allowed:   true,
		Remaining: 99,
		ResetAt:   now,
	}

	if !r.Allowed {
		t.Error("expected allowed")
	}
	if r.Remaining != 99 {
		t.Errorf("expected 99, got %d", r.Remaining)
	}
}
