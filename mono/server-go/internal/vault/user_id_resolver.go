package vault

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBUserIDResolver implements UserIDResolver by looking up the user ID
// from PostgreSQL given a vault token. This bridges the legacy vault_token
// based VCS resolver with the new user-ID-based keychain.
type DBUserIDResolver struct {
	pool *pgxpool.Pool
}

// NewDBUserIDResolver creates a resolver backed by the users table.
func NewDBUserIDResolver(pool *pgxpool.Pool) *DBUserIDResolver {
	return &DBUserIDResolver{pool: pool}
}

// UserIDFromVaultToken looks up the user ID for a given vault token.
func (r *DBUserIDResolver) UserIDFromVaultToken(token string) (uuid.UUID, error) {
	var userID uuid.UUID
	err := r.pool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE vault_token = $1`, token,
	).Scan(&userID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("vault: resolve user from vault_token: %w", err)
	}
	return userID, nil
}
