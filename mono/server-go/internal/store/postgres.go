// Package store provides PostgreSQL database access with connection pooling
// and repository patterns.
package store

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AuralithAI/rtvortex-server/internal/config"
)

// DB wraps a pgxpool.Pool and provides database operations.
type DB struct {
	Pool *pgxpool.Pool
}

// NewPostgresPool creates a new connection pool to PostgreSQL.
// It configures pool size, timeouts, and health checks for production use.
func NewPostgresPool(ctx context.Context, cfg config.DatabaseConfig) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parsing database DSN: %w", err)
	}

	// ── Pool configuration ──────────────────────────────────────────────
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime

	// Health check: verify connections are still alive
	poolCfg.HealthCheckPeriod = 30 * time.Second

	// Connection timeout
	poolCfg.ConnConfig.ConnectTimeout = cfg.ConnTimeout

	// ── Create pool ─────────────────────────────────────────────────────
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	// Verify connectivity
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	slog.Info("PostgreSQL pool created",
		"max_conns", cfg.MaxConns,
		"min_conns", cfg.MinConns,
		"max_lifetime", cfg.MaxConnLifetime,
	)

	return &DB{Pool: pool}, nil
}

// Close closes all connections in the pool.
func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
		slog.Info("PostgreSQL pool closed")
	}
}

// RunMigrations checks whether the schema has been initialized.
// If the schema_info table does not exist, it runs initData.sql from sqlDir.
// sqlDir is typically RTVORTEX_HOME/data/sql.
func (db *DB) RunMigrations(sqlDir string) error {
	ctx := context.Background()

	// Check if schema is already initialized.
	var exists bool
	err := db.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'schema_info'
		)`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("checking schema_info table: %w", err)
	}

	if exists {
		var ver int
		_ = db.Pool.QueryRow(ctx,
			`SELECT COALESCE(MAX(version), 0) FROM schema_info`).Scan(&ver)
		slog.Info("database schema already initialized", "version", ver)
		return nil
	}

	// Schema not initialized — run initData.sql.
	initPath := filepath.Join(sqlDir, "initData.sql")
	sqlBytes, err := os.ReadFile(initPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w (run create_database.sql and initData.sql manually — see db/sql/)", initPath, err)
	}

	slog.Info("initializing database schema", "file", initPath)
	if _, err := db.Pool.Exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("executing initData.sql: %w", err)
	}

	slog.Info("database schema initialized successfully")
	return nil
}

// Stats returns current pool statistics for monitoring.
func (db *DB) Stats() *PoolStats {
	stat := db.Pool.Stat()
	return &PoolStats{
		TotalConns:        stat.TotalConns(),
		AcquiredConns:     stat.AcquiredConns(),
		IdleConns:         stat.IdleConns(),
		MaxConns:          stat.MaxConns(),
		AcquireCount:      stat.AcquireCount(),
		AcquireDuration:   stat.AcquireDuration(),
		NewConnsCount:     stat.NewConnsCount(),
		EmptyAcquireCount: stat.EmptyAcquireCount(),
	}
}

// PoolStats holds connection pool statistics for health/monitoring endpoints.
type PoolStats struct {
	TotalConns        int32         `json:"total_conns"`
	AcquiredConns     int32         `json:"acquired_conns"`
	IdleConns         int32         `json:"idle_conns"`
	MaxConns          int32         `json:"max_conns"`
	AcquireCount      int64         `json:"acquire_count"`
	AcquireDuration   time.Duration `json:"acquire_duration"`
	NewConnsCount     int64         `json:"new_conns_count"`
	EmptyAcquireCount int64         `json:"empty_acquire_count"`
}
