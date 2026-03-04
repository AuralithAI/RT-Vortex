// Package session provides Redis-backed session management and rate limiting.
package session

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"

	"github.com/AuralithAI/rtvortex-server/internal/config"
)

// RedisClient wraps go-redis with application-specific methods.
type RedisClient struct {
	client *redis.Client
}

// NewRedisClient creates a new Redis connection with the given configuration.
func NewRedisClient(cfg config.RedisConfig) (*RedisClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		MaxRetries:   cfg.MaxRetries,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	// Verify connectivity
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("pinging Redis: %w", err)
	}

	slog.Info("Redis client connected",
		"addr", cfg.Addr,
		"pool_size", cfg.PoolSize,
	)

	return &RedisClient{client: client}, nil
}

// Client returns the underlying go-redis client.
func (r *RedisClient) Client() *redis.Client {
	return r.client
}

// Close closes the Redis connection.
func (r *RedisClient) Close() error {
	slog.Info("Redis client closing")
	return r.client.Close()
}
