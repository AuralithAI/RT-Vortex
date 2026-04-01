package keychain

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SecretEntry represents a single encrypted secret stored in Postgres.
type SecretEntry struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	Name          string
	Ciphertext    []byte // nonce || AES-256-GCM ciphertext
	WrappedDEK    []byte // nonce || AES-256-GCM(encryption_key, DEK)
	Version       int64
	Category      string // "vcs", "llm", "mcp", "custom"
	Metadata      string // non-secret metadata JSON (provider name, etc.)
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// UserKeychain holds per-user keychain metadata in Postgres.
type UserKeychain struct {
	UserID          uuid.UUID
	Salt            []byte // HKDF salt for key derivation
	AuthKeyHash     string // hex-encoded HMAC(auth_key, fixed_challenge) for verification
	RecoveryHint    string // user-provided hint (NOT the phrase itself)
	KeyVersion      int    // incremented on master key rotation
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Store is the Postgres-backed keychain storage layer. It stores only
// encrypted data — plaintext secrets never reach this layer.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a keychain store backed by the given connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ── User Keychain Management ────────────────────────────────────────────────

// InitKeychain creates the keychain record for a new user.
func (s *Store) InitKeychain(ctx context.Context, kc *UserKeychain) error {
	now := time.Now().UTC()
	kc.CreatedAt = now
	kc.UpdatedAt = now

	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_keychains (user_id, salt, auth_key_hash, recovery_hint, key_version, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (user_id) DO UPDATE SET
		     salt = EXCLUDED.salt,
		     auth_key_hash = EXCLUDED.auth_key_hash,
		     recovery_hint = EXCLUDED.recovery_hint,
		     key_version = EXCLUDED.key_version,
		     updated_at = EXCLUDED.updated_at`,
		kc.UserID, kc.Salt, kc.AuthKeyHash, kc.RecoveryHint,
		kc.KeyVersion, kc.CreatedAt, kc.UpdatedAt,
	)
	return err
}

// GetKeychain retrieves a user's keychain metadata.
func (s *Store) GetKeychain(ctx context.Context, userID uuid.UUID) (*UserKeychain, error) {
	var kc UserKeychain
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, salt, auth_key_hash, recovery_hint, key_version, created_at, updated_at
		 FROM user_keychains WHERE user_id = $1`, userID,
	).Scan(&kc.UserID, &kc.Salt, &kc.AuthKeyHash, &kc.RecoveryHint,
		&kc.KeyVersion, &kc.CreatedAt, &kc.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &kc, nil
}

// UpdateKeychainKeys updates the salt, auth hash, and version after a key rotation.
func (s *Store) UpdateKeychainKeys(ctx context.Context, userID uuid.UUID, salt []byte, authKeyHash string, keyVersion int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE user_keychains
		 SET salt = $1, auth_key_hash = $2, key_version = $3, updated_at = NOW()
		 WHERE user_id = $4`,
		salt, authKeyHash, keyVersion, userID,
	)
	return err
}

// ── Secret CRUD ─────────────────────────────────────────────────────────────

// PutSecret stores or updates an encrypted secret. Uses CRDT-style versioning:
// the write succeeds only if the provided version is greater than the stored
// version (or the secret does not yet exist).
func (s *Store) PutSecret(ctx context.Context, entry *SecretEntry) error {
	now := time.Now().UTC()
	entry.UpdatedAt = now
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
		entry.CreatedAt = now
	}

	tag, err := s.pool.Exec(ctx,
		`INSERT INTO keychain_secrets
			(id, user_id, name, ciphertext, wrapped_dek, version, category, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (user_id, name) DO UPDATE SET
		     ciphertext  = EXCLUDED.ciphertext,
		     wrapped_dek = EXCLUDED.wrapped_dek,
		     version     = EXCLUDED.version,
		     category    = EXCLUDED.category,
		     metadata    = EXCLUDED.metadata,
		     updated_at  = EXCLUDED.updated_at
		 WHERE keychain_secrets.version < EXCLUDED.version`,
		entry.ID, entry.UserID, entry.Name, entry.Ciphertext,
		entry.WrappedDEK, entry.Version, entry.Category, entry.Metadata,
		entry.CreatedAt, entry.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("keychain: put secret: %w", err)
	}

	if tag.RowsAffected() == 0 {
		slog.Debug("keychain: put secret skipped (version not newer)",
			"user_id", entry.UserID, "name", entry.Name, "version", entry.Version)
	}
	return nil
}

// GetSecret retrieves a single encrypted secret by user and name.
func (s *Store) GetSecret(ctx context.Context, userID uuid.UUID, name string) (*SecretEntry, error) {
	var e SecretEntry
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, name, ciphertext, wrapped_dek, version, category, metadata, created_at, updated_at
		 FROM keychain_secrets WHERE user_id = $1 AND name = $2`,
		userID, name,
	).Scan(&e.ID, &e.UserID, &e.Name, &e.Ciphertext, &e.WrappedDEK,
		&e.Version, &e.Category, &e.Metadata, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListSecrets returns all encrypted secrets for a user. Only metadata is
// returned in the listing; ciphertext and wrapped DEK are included for
// bulk sync operations.
func (s *Store) ListSecrets(ctx context.Context, userID uuid.UUID) ([]SecretEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, name, ciphertext, wrapped_dek, version, category, metadata, created_at, updated_at
		 FROM keychain_secrets WHERE user_id = $1 ORDER BY name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SecretEntry, error) {
		var e SecretEntry
		err := row.Scan(&e.ID, &e.UserID, &e.Name, &e.Ciphertext, &e.WrappedDEK,
			&e.Version, &e.Category, &e.Metadata, &e.CreatedAt, &e.UpdatedAt)
		return e, err
	})
}

// ListSecretsByCategory returns encrypted secrets filtered by category.
func (s *Store) ListSecretsByCategory(ctx context.Context, userID uuid.UUID, category string) ([]SecretEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, name, ciphertext, wrapped_dek, version, category, metadata, created_at, updated_at
		 FROM keychain_secrets WHERE user_id = $1 AND category = $2 ORDER BY name`,
		userID, category,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SecretEntry, error) {
		var e SecretEntry
		err := row.Scan(&e.ID, &e.UserID, &e.Name, &e.Ciphertext, &e.WrappedDEK,
			&e.Version, &e.Category, &e.Metadata, &e.CreatedAt, &e.UpdatedAt)
		return e, err
	})
}

// DeleteSecret removes a single secret.
func (s *Store) DeleteSecret(ctx context.Context, userID uuid.UUID, name string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM keychain_secrets WHERE user_id = $1 AND name = $2`,
		userID, name,
	)
	return err
}

// DeleteAllSecrets removes all secrets for a user (used during account deletion).
func (s *Store) DeleteAllSecrets(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM keychain_secrets WHERE user_id = $1`, userID)
	return err
}

// ── Sync Operations ─────────────────────────────────────────────────────────

// SecretVersionEntry is a lightweight version-only view for sync negotiation.
type SecretVersionEntry struct {
	Name      string
	Version   int64
	UpdatedAt time.Time
}

// ListVersions returns name + version for all of a user's secrets.
// Used for efficient sync: the client compares versions and only pulls
// secrets with newer versions.
func (s *Store) ListVersions(ctx context.Context, userID uuid.UUID) ([]SecretVersionEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT name, version, updated_at FROM keychain_secrets WHERE user_id = $1 ORDER BY name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SecretVersionEntry, error) {
		var e SecretVersionEntry
		err := row.Scan(&e.Name, &e.Version, &e.UpdatedAt)
		return e, err
	})
}

// MergeSecrets performs a CRDT-style merge of multiple secrets. For each
// secret, the highest version wins. This is the core sync operation.
func (s *Store) MergeSecrets(ctx context.Context, entries []SecretEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("keychain: begin merge tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, entry := range entries {
		entry.UpdatedAt = time.Now().UTC()
		if entry.ID == uuid.Nil {
			entry.ID = uuid.New()
			entry.CreatedAt = entry.UpdatedAt
		}

		_, err := tx.Exec(ctx,
			`INSERT INTO keychain_secrets
				(id, user_id, name, ciphertext, wrapped_dek, version, category, metadata, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			 ON CONFLICT (user_id, name) DO UPDATE SET
			     ciphertext  = EXCLUDED.ciphertext,
			     wrapped_dek = EXCLUDED.wrapped_dek,
			     version     = EXCLUDED.version,
			     category    = EXCLUDED.category,
			     metadata    = EXCLUDED.metadata,
			     updated_at  = EXCLUDED.updated_at
			 WHERE keychain_secrets.version < EXCLUDED.version`,
			entry.ID, entry.UserID, entry.Name, entry.Ciphertext,
			entry.WrappedDEK, entry.Version, entry.Category, entry.Metadata,
			entry.CreatedAt, entry.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("keychain: merge secret %q: %w", entry.Name, err)
		}
	}

	return tx.Commit(ctx)
}

// ── Bulk Key Rotation ───────────────────────────────────────────────────────

// ReWrapAllDEKs updates the wrapped_dek column for all of a user's secrets.
// Used during master key rotation: the caller unwraps each DEK with the old
// encryption key and re-wraps with the new one, then calls this to persist.
func (s *Store) ReWrapAllDEKs(ctx context.Context, userID uuid.UUID, reWrapped map[string][]byte) error {
	if len(reWrapped) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("keychain: begin rewrap tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for name, newWrapped := range reWrapped {
		_, err := tx.Exec(ctx,
			`UPDATE keychain_secrets SET wrapped_dek = $1, updated_at = NOW()
			 WHERE user_id = $2 AND name = $3`,
			newWrapped, userID, name,
		)
		if err != nil {
			return fmt.Errorf("keychain: rewrap DEK for %q: %w", name, err)
		}
	}

	return tx.Commit(ctx)
}

// ── Audit Log ───────────────────────────────────────────────────────────────

// AuditAction represents an auditable keychain operation.
type AuditAction string

const (
	AuditInit        AuditAction = "keychain_init"
	AuditPutSecret   AuditAction = "secret_put"
	AuditGetSecret   AuditAction = "secret_get"
	AuditDeleteSecret AuditAction = "secret_delete"
	AuditSync        AuditAction = "sync"
	AuditRotateKey   AuditAction = "key_rotate"
	AuditRecover     AuditAction = "recovery"
)

// LogAccess records an auditable event in the keychain_audit_log table.
// This logs WHO accessed WHAT and WHEN, never the secret content.
func (s *Store) LogAccess(ctx context.Context, userID uuid.UUID, action AuditAction, secretName, ipAddr, userAgent string) {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO keychain_audit_log (id, user_id, action, secret_name, ip_addr, user_agent, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.New(), userID, string(action), secretName, ipAddr, userAgent, time.Now().UTC(),
	)
	if err != nil {
		slog.Warn("keychain: failed to write audit log", "error", err, "user_id", userID, "action", action)
	}
}

// ── Auth Challenge Storage ──────────────────────────────────────────────────

// StoreAuthKeyVerifier saves the HMAC of a fixed challenge so the server can
// verify key ownership on subsequent requests without storing the auth key.
func ComputeAuthKeyVerifier(authKey [AuthKeySize]byte) string {
	fixedChallenge := []byte("rtvortex-keychain-auth-verifier-v1")
	proof := ComputeAuthProof(authKey, fixedChallenge)
	return hex.EncodeToString(proof)
}
