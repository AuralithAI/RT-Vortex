// Package store provides PostgreSQL database access with connection pooling
// and repository patterns.
package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

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

// RunMigrations applies database migrations using golang-migrate.
// It looks for migration files in migrationsDir (e.g., RTVORTEX_HOME/db/migrations).
// If no migration files are found, it falls back to running initData.sql from sqlDir.
func (db *DB) RunMigrations(sqlDir string) error {
	ctx := context.Background()

	// Resolve migrations directory (sibling of sql/)
	migrationsDir := filepath.Join(filepath.Dir(sqlDir), "migrations")

	// Try golang-migrate first if migration files exist.
	if entries, err := os.ReadDir(migrationsDir); err == nil && len(entries) > 0 {
		return db.runGolangMigrate(migrationsDir)
	}

	// Fallback: check schema_info and run initData.sql directly.
	slog.Info("no migration files found, falling back to initData.sql", "dir", migrationsDir)
	return db.runInitDataSQL(ctx, sqlDir)
}

// runGolangMigrate applies versioned migrations via golang-migrate.
func (db *DB) runGolangMigrate(migrationsDir string) error {
	// Open a *sql.DB from the pgx pool for golang-migrate compatibility.
	sqlDB := stdlib.OpenDBFromPool(db.Pool)
	defer sqlDB.Close()

	driver, err := postgres.WithInstance(sqlDB, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("creating migrate postgres driver: %w", err)
	}

	absPath, err := filepath.Abs(migrationsDir)
	if err != nil {
		return fmt.Errorf("resolving migrations path: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://"+absPath,
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("creating migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running migrations: %w", err)
	}

	version, dirty, _ := m.Version()
	slog.Info("database migrations applied", "version", version, "dirty", dirty)
	return nil
}

// runInitDataSQL is the legacy bootstrap path: checks schema_info and runs
// initData.sql if the schema has not been initialized.
func (db *DB) runInitDataSQL(ctx context.Context, sqlDir string) error {
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
