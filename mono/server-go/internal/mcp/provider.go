package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type ActionDef struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	RequiredParams  []string `json:"required_params"`
	OptionalParams  []string `json:"optional_params,omitempty"`
	ConsentRequired bool     `json:"consent_required"`
}

type Result struct {
	Success bool                   `json:"success"`
	Data    map[string]interface{} `json:"data,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

type Provider interface {
	Name() string
	Actions() []ActionDef
	Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*Result, error)
	RefreshToken(ctx context.Context, refreshToken string) (newAccessToken string, newRefreshToken string, expiresIn time.Duration, err error)
}

type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	breakers  map[string]*circuitBreaker
	rdb       *redis.Client
}

func NewProviderRegistry(rdb *redis.Client) *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[string]Provider),
		breakers:  make(map[string]*circuitBreaker),
		rdb:       rdb,
	}
}

func (r *ProviderRegistry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
	r.breakers[p.Name()] = newCircuitBreaker(5, 60*time.Second)
}

func (r *ProviderRegistry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

func (r *ProviderRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for n := range r.providers {
		names = append(names, n)
	}
	return names
}

func (r *ProviderRegistry) AllActions(name string) []ActionDef {
	r.mu.RLock()
	p, ok := r.providers[name]
	r.mu.RUnlock()
	if !ok {
		return nil
	}
	return p.Actions()
}

func (r *ProviderRegistry) CheckCircuitBreaker(provider string) error {
	r.mu.RLock()
	cb, ok := r.breakers[provider]
	r.mu.RUnlock()
	if !ok {
		return nil
	}
	return cb.check()
}

func (r *ProviderRegistry) RecordSuccess(provider string) {
	r.mu.RLock()
	cb, ok := r.breakers[provider]
	r.mu.RUnlock()
	if ok {
		cb.recordSuccess()
	}
}

func (r *ProviderRegistry) RecordFailure(provider string) {
	r.mu.RLock()
	cb, ok := r.breakers[provider]
	r.mu.RUnlock()
	if ok {
		cb.recordFailure()
	}
}

func (r *ProviderRegistry) CheckRateLimit(ctx context.Context, provider string) error {
	if r.rdb == nil {
		return nil
	}
	key := fmt.Sprintf("mcp:rl:%s", provider)
	count, err := r.rdb.Incr(ctx, key).Result()
	if err != nil {
		return nil
	}
	if count == 1 {
		r.rdb.Expire(ctx, key, time.Minute)
	}
	limits := map[string]int64{
		"slack":   60,
		"ms365":   120,
		"gmail":   60,
		"discord": 30,
	}
	limit, ok := limits[provider]
	if !ok {
		limit = 60
	}
	if count > limit {
		return fmt.Errorf("rate limit exceeded for provider %s (%d/%d per minute)", provider, count, limit)
	}
	return nil
}

type circuitBreaker struct {
	mu           sync.Mutex
	failures     int
	threshold    int
	resetTimeout time.Duration
	openUntil    time.Time
}

func newCircuitBreaker(threshold int, resetTimeout time.Duration) *circuitBreaker {
	return &circuitBreaker{
		threshold:    threshold,
		resetTimeout: resetTimeout,
	}
}

func (cb *circuitBreaker) check() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.failures < cb.threshold {
		return nil
	}
	if time.Now().After(cb.openUntil) {
		cb.failures = 0
		return nil
	}
	return fmt.Errorf("circuit breaker open: provider unavailable until %s", cb.openUntil.Format(time.RFC3339))
}

func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
}

func (cb *circuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	if cb.failures >= cb.threshold {
		cb.openUntil = time.Now().Add(cb.resetTimeout)
	}
}
