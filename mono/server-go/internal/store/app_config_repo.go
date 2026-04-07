package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ── AppConfigRepo ───────────────────────────────────────────────────────────
// System-wide key/value configuration stored in the app_config table.
// NOT for secrets — secrets belong in the keychain.

// AppConfigRepo provides typed access to the app_config table.
type AppConfigRepo struct {
	pool *pgxpool.Pool
}

// NewAppConfigRepo creates a new AppConfigRepo.
func NewAppConfigRepo(pool *pgxpool.Pool) *AppConfigRepo {
	return &AppConfigRepo{pool: pool}
}

// Get returns the value for the given config key, or "" if not found.
func (r *AppConfigRepo) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := r.pool.QueryRow(ctx,
		`SELECT value FROM app_config WHERE key = $1`, key,
	).Scan(&value)
	if err != nil {
		// pgx returns ErrNoRows when the key doesn't exist.
		if err.Error() == "no rows in result set" {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

// Set upserts a config key/value pair.
func (r *AppConfigRepo) Set(ctx context.Context, key, value string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO app_config (key, value, updated_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		key, value,
	)
	return err
}
