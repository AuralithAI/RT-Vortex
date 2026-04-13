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
	ID         uuid.UUID
	UserID     uuid.UUID
	RepoID     *uuid.UUID
	Name       string
	Ciphertext []byte // nonce || AES-256-GCM ciphertext
	WrappedDEK []byte // nonce || AES-256-GCM(encryption_key, DEK)
	Version    int64
	Category   string // "vcs", "llm", "mcp", "custom", "build"
	Metadata   string // non-secret metadata JSON (provider name, etc.)
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// UserKeychain holds per-user keychain metadata in Postgres.
type UserKeychain struct {
	UserID             uuid.UUID
	Salt               []byte // HKDF salt for sub-key derivation
	AuthKeyHash        string // hex-encoded HMAC(auth_key, fixed_challenge) for verification
	RecoveryHint       string // user-provided hint (NOT the phrase itself)
	RecoverySalt       []byte // Argon2id salt for recovery wrapping key derivation
	RecoveryWrappedKey []byte // master key wrapped with phrase-derived key (recovery path)
	KeyVersion         int    // incremented on master key rotation
	CreatedAt          time.Time
	UpdatedAt          time.Time
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
		`INSERT INTO user_keychains (user_id, salt, auth_key_hash, recovery_hint, recovery_salt, recovery_wrapped_key, key_version, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (user_id) DO UPDATE SET
		     salt = EXCLUDED.salt,
		     auth_key_hash = EXCLUDED.auth_key_hash,
		     recovery_hint = EXCLUDED.recovery_hint,
		     recovery_salt = EXCLUDED.recovery_salt,
		     recovery_wrapped_key = EXCLUDED.recovery_wrapped_key,
		     key_version = EXCLUDED.key_version,
		     updated_at = EXCLUDED.updated_at`,
		kc.UserID, kc.Salt, kc.AuthKeyHash, kc.RecoveryHint,
		kc.RecoverySalt, kc.RecoveryWrappedKey,
		kc.KeyVersion, kc.CreatedAt, kc.UpdatedAt,
	)
	return err
}

// GetKeychain retrieves a user's keychain metadata.
func (s *Store) GetKeychain(ctx context.Context, userID uuid.UUID) (*UserKeychain, error) {
	var kc UserKeychain
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, salt, auth_key_hash, recovery_hint, recovery_salt, recovery_wrapped_key, key_version, created_at, updated_at
		 FROM user_keychains WHERE user_id = $1`, userID,
	).Scan(&kc.UserID, &kc.Salt, &kc.AuthKeyHash, &kc.RecoveryHint,
		&kc.RecoverySalt, &kc.RecoveryWrappedKey,
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

// UpdateRecoveryWrappedKey updates only the recovery-wrapped master key blob
// (e.g. after key rotation when the caller has the phrase or re-wraps).
func (s *Store) UpdateRecoveryWrappedKey(ctx context.Context, userID uuid.UUID, recoverySalt, recoveryWrappedKey []byte) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE user_keychains
		 SET recovery_salt = $1, recovery_wrapped_key = $2, updated_at = NOW()
		 WHERE user_id = $3`,
		recoverySalt, recoveryWrappedKey, userID,
	)
	return err
}

// ── Secret CRUD ─────────────────────────────────────────────────────────────

// PutSecret stores or updates an encrypted secret with CRDT-style versioning.
func (s *Store) PutSecret(ctx context.Context, entry *SecretEntry) error {
	now := time.Now().UTC()
	entry.UpdatedAt = now
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
		entry.CreatedAt = now
	}

	tag, err := s.pool.Exec(ctx,
		`INSERT INTO keychain_secrets
			(id, user_id, repo_id, name, ciphertext, wrapped_dek, version, category, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (user_id, name, COALESCE(repo_id, '00000000-0000-0000-0000-000000000000'::uuid)) DO UPDATE SET
		     ciphertext  = EXCLUDED.ciphertext,
		     wrapped_dek = EXCLUDED.wrapped_dek,
		     version     = EXCLUDED.version,
		     category    = EXCLUDED.category,
		     metadata    = EXCLUDED.metadata,
		     updated_at  = EXCLUDED.updated_at
		 WHERE keychain_secrets.version < EXCLUDED.version`,
		entry.ID, entry.UserID, entry.RepoID, entry.Name, entry.Ciphertext,
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

// GetSecret retrieves a single encrypted secret by user, name, and optional repo scope.
func (s *Store) GetSecret(ctx context.Context, userID uuid.UUID, name string, repoID *uuid.UUID) (*SecretEntry, error) {
	var e SecretEntry
	var query string
	var args []any

	if repoID != nil {
		query = `SELECT id, user_id, repo_id, name, ciphertext, wrapped_dek, version, category, metadata, created_at, updated_at
		         FROM keychain_secrets WHERE user_id = $1 AND name = $2 AND repo_id = $3`
		args = []any{userID, name, *repoID}
	} else {
		query = `SELECT id, user_id, repo_id, name, ciphertext, wrapped_dek, version, category, metadata, created_at, updated_at
		         FROM keychain_secrets WHERE user_id = $1 AND name = $2 AND repo_id IS NULL`
		args = []any{userID, name}
	}

	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&e.ID, &e.UserID, &e.RepoID, &e.Name, &e.Ciphertext, &e.WrappedDEK,
		&e.Version, &e.Category, &e.Metadata, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListSecrets returns all encrypted secrets for a user (global scope, repo_id IS NULL).
func (s *Store) ListSecrets(ctx context.Context, userID uuid.UUID) ([]SecretEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, repo_id, name, ciphertext, wrapped_dek, version, category, metadata, created_at, updated_at
		 FROM keychain_secrets WHERE user_id = $1 AND repo_id IS NULL ORDER BY name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SecretEntry, error) {
		var e SecretEntry
		err := row.Scan(&e.ID, &e.UserID, &e.RepoID, &e.Name, &e.Ciphertext, &e.WrappedDEK,
			&e.Version, &e.Category, &e.Metadata, &e.CreatedAt, &e.UpdatedAt)
		return e, err
	})
}

// ListSecretsByCategory returns encrypted secrets filtered by category (global scope only).
func (s *Store) ListSecretsByCategory(ctx context.Context, userID uuid.UUID, category string) ([]SecretEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, repo_id, name, ciphertext, wrapped_dek, version, category, metadata, created_at, updated_at
		 FROM keychain_secrets WHERE user_id = $1 AND category = $2 AND repo_id IS NULL ORDER BY name`,
		userID, category,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SecretEntry, error) {
		var e SecretEntry
		err := row.Scan(&e.ID, &e.UserID, &e.RepoID, &e.Name, &e.Ciphertext, &e.WrappedDEK,
			&e.Version, &e.Category, &e.Metadata, &e.CreatedAt, &e.UpdatedAt)
		return e, err
	})
}

// DeleteSecret removes a single secret (global scope, repo_id IS NULL).
func (s *Store) DeleteSecret(ctx context.Context, userID uuid.UUID, name string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM keychain_secrets WHERE user_id = $1 AND name = $2 AND repo_id IS NULL`,
		userID, name,
	)
	return err
}

// DeleteRepoSecret removes a repo-scoped secret.
func (s *Store) DeleteRepoSecret(ctx context.Context, userID uuid.UUID, repoID uuid.UUID, name string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM keychain_secrets WHERE user_id = $1 AND name = $2 AND repo_id = $3`,
		userID, name, repoID,
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
	Category  string
	RepoID    *uuid.UUID
	UpdatedAt time.Time
}

// ListVersions returns name + version + category for all of a user's global secrets.
func (s *Store) ListVersions(ctx context.Context, userID uuid.UUID) ([]SecretVersionEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT name, version, category, repo_id, updated_at FROM keychain_secrets WHERE user_id = $1 AND repo_id IS NULL ORDER BY name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SecretVersionEntry, error) {
		var e SecretVersionEntry
		err := row.Scan(&e.Name, &e.Version, &e.Category, &e.RepoID, &e.UpdatedAt)
		return e, err
	})
}

// ListRepoSecretVersions returns name + version for secrets scoped to a specific repo.
func (s *Store) ListRepoSecretVersions(ctx context.Context, userID, repoID uuid.UUID) ([]SecretVersionEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT name, version, category, repo_id, updated_at FROM keychain_secrets WHERE user_id = $1 AND repo_id = $2 ORDER BY name`,
		userID, repoID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SecretVersionEntry, error) {
		var e SecretVersionEntry
		err := row.Scan(&e.Name, &e.Version, &e.Category, &e.RepoID, &e.UpdatedAt)
		return e, err
	})
}

// MergeSecrets performs a CRDT-style merge of multiple secrets.
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
				(id, user_id, repo_id, name, ciphertext, wrapped_dek, version, category, metadata, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			 ON CONFLICT (user_id, name, COALESCE(repo_id, '00000000-0000-0000-0000-000000000000'::uuid)) DO UPDATE SET
			     ciphertext  = EXCLUDED.ciphertext,
			     wrapped_dek = EXCLUDED.wrapped_dek,
			     version     = EXCLUDED.version,
			     category    = EXCLUDED.category,
			     metadata    = EXCLUDED.metadata,
			     updated_at  = EXCLUDED.updated_at
			 WHERE keychain_secrets.version < EXCLUDED.version`,
			entry.ID, entry.UserID, entry.RepoID, entry.Name, entry.Ciphertext,
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
	AuditInit            AuditAction = "keychain_init"
	AuditPutSecret       AuditAction = "secret_put"
	AuditGetSecret       AuditAction = "secret_get"
	AuditDeleteSecret    AuditAction = "secret_delete"
	AuditSync            AuditAction = "sync"
	AuditRotateKey       AuditAction = "key_rotate"
	AuditRecover         AuditAction = "recovery"
	AuditRefreshRecovery AuditAction = "recovery_refresh"
	AuditBuildSecretPut  AuditAction = "build_secret_put"
	AuditBuildSecretGet  AuditAction = "build_secret_get"
	AuditBuildSecretDel  AuditAction = "build_secret_delete"
	AuditBuildSecretList AuditAction = "build_secret_list"
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

// AuditLogEntry represents a single row from the keychain_audit_log table.
type AuditLogEntry struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"user_id"`
	Action     string    `json:"action"`
	SecretName string    `json:"secret_name"`
	IPAddr     string    `json:"ip_addr"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListAuditLog returns the most recent audit events for a user, newest first.
// limit controls the max rows returned (default 50, max 200).
func (s *Store) ListAuditLog(ctx context.Context, userID uuid.UUID, limit int) ([]AuditLogEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, action, secret_name, ip_addr, user_agent, created_at
		 FROM keychain_audit_log
		 WHERE user_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (AuditLogEntry, error) {
		var e AuditLogEntry
		err := row.Scan(&e.ID, &e.UserID, &e.Action, &e.SecretName, &e.IPAddr, &e.UserAgent, &e.CreatedAt)
		return e, err
	})
}

// FindUsersWithSecretPrefix returns distinct user IDs that have at least one
// secret whose name starts with the given prefix (e.g. "llm."). Used at
// startup to discover which users have persisted LLM API keys so the server
// can rehydrate the in-memory LLM registry without waiting for a UI request.
func (s *Store) FindUsersWithSecretPrefix(ctx context.Context, prefix string) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT user_id FROM keychain_secrets WHERE name LIKE $1`,
		prefix+"%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var uid uuid.UUID
		err := row.Scan(&uid)
		return uid, err
	})
}

// ── Auth Challenge Storage ──────────────────────────────────────────────────

// StoreAuthKeyVerifier saves the HMAC of a fixed challenge so the server can
// verify key ownership on subsequent requests without storing the auth key.
func ComputeAuthKeyVerifier(authKey [AuthKeySize]byte) string {
	fixedChallenge := []byte("rtvortex-keychain-auth-verifier-v1")
	proof := ComputeAuthProof(authKey, fixedChallenge)
	return hex.EncodeToString(proof)
}
