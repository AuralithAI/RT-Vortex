package session

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ── Rate Limiter ────────────────────────────────────────────────────────────

const rateLimitPrefix = "rl:"

// RateLimitConfig configures a rate limiter.
type RateLimitConfig struct {
	MaxRequests int           // max requests per window
	Window      time.Duration // sliding window duration
}

// RateLimiter implements a sliding window rate limiter backed by Redis.
type RateLimiter struct {
	rdb     *redis.Client
	configs map[string]RateLimitConfig // keyed by limiter name
	mu      sync.RWMutex
}

// NewRateLimiter creates a rate limiter.
func NewRateLimiter(rdb *redis.Client) *RateLimiter {
	return &RateLimiter{
		rdb:     rdb,
		configs: make(map[string]RateLimitConfig),
	}
}

// Configure registers a named rate limit configuration.
func (rl *RateLimiter) Configure(name string, cfg RateLimitConfig) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.configs[name] = cfg
}

// RateLimitResult contains the result of a rate limit check.
type RateLimitResult struct {
	Allowed   bool
	Remaining int
	ResetAt   time.Time
}

// Allow checks whether a request identified by key is within the limit.
// Uses Redis sorted sets for sliding window counting.
func (rl *RateLimiter) Allow(ctx context.Context, limiterName, key string) (*RateLimitResult, error) {
	rl.mu.RLock()
	cfg, ok := rl.configs[limiterName]
	rl.mu.RUnlock()
	if !ok {
		return &RateLimitResult{Allowed: true, Remaining: -1}, nil
	}

	now := time.Now()
	windowStart := now.Add(-cfg.Window)
	redisKey := fmt.Sprintf("%s%s:%s", rateLimitPrefix, limiterName, key)

	// Use a pipeline:
	// 1. Remove expired entries
	// 2. Count current entries
	// 3. Add current request if within limit
	// 4. Set TTL

	pipe := rl.rdb.Pipeline()
	pipe.ZRemRangeByScore(ctx, redisKey, "0", fmt.Sprintf("%d", windowStart.UnixMicro()))
	countCmd := pipe.ZCard(ctx, redisKey)
	_, err := pipe.Exec(ctx)
	if err != nil {
		slog.Error("rate limiter pipeline error", "error", err)
		// Fail open — allow on Redis errors.
		return &RateLimitResult{Allowed: true, Remaining: cfg.MaxRequests}, nil
	}

	currentCount := int(countCmd.Val())
	remaining := cfg.MaxRequests - currentCount

	if remaining <= 0 {
		// Over limit — find the earliest entry to calculate reset time.
		entries, err := rl.rdb.ZRangeWithScores(ctx, redisKey, 0, 0).Result()
		resetAt := now.Add(cfg.Window)
		if err == nil && len(entries) > 0 {
			earliestMicro := int64(entries[0].Score)
			resetAt = time.UnixMicro(earliestMicro).Add(cfg.Window)
		}
		return &RateLimitResult{
			Allowed:   false,
			Remaining: 0,
			ResetAt:   resetAt,
		}, nil
	}

	// Within limit — add this request.
	pipe = rl.rdb.Pipeline()
	member := fmt.Sprintf("%d", now.UnixMicro())
	pipe.ZAdd(ctx, redisKey, redis.Z{Score: float64(now.UnixMicro()), Member: member})
	pipe.Expire(ctx, redisKey, cfg.Window+time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		slog.Error("rate limiter add error", "error", err)
	}

	return &RateLimitResult{
		Allowed:   true,
		Remaining: remaining - 1,
		ResetAt:   now.Add(cfg.Window),
	}, nil
}
