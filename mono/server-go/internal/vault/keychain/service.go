package keychain

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

// Service is the high-level keychain API used by the rest of the application.
// It coordinates between the crypto layer, Postgres store, and Redis cache.
//
// The service holds a cache of derived encryption keys in memory, keyed by
// user ID. Keys are loaded on first access (when the user authenticates)
// and evicted after a configurable TTL.
type Service struct {
	store *Store
	rdb   *redis.Client

	mu       sync.RWMutex
	keyCache map[uuid.UUID]*cachedKey
	cacheTTL time.Duration

	// ServerMasterKey is the server-side KEK used to wrap per-user master keys.
	// In production this should come from an HSM or KMS. For now it is derived
	// from the TOKEN_ENCRYPTION_KEY environment variable (same as the old vault).
	serverMasterKey [MasterKeySize]byte
}

type cachedKey struct {
	keys      *DerivedKeys
	expiresAt time.Time
}

// ServiceConfig configures the keychain service.
type ServiceConfig struct {
	// ServerEncryptionKey is the hex-encoded 256-bit server KEK.
	ServerEncryptionKey string

	// CacheTTL controls how long derived keys stay in memory. Default: 30 minutes.
	CacheTTL time.Duration
}

// NewService creates a new keychain service.
func NewService(store *Store, rdb *redis.Client, cfg ServiceConfig) (*Service, error) {
	serverKey, err := MasterKeyFromHex(cfg.ServerEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("keychain: invalid server encryption key: %w", err)
	}

	cacheTTL := cfg.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = 30 * time.Minute
	}

	svc := &Service{
		store:           store,
		rdb:             rdb,
		keyCache:        make(map[uuid.UUID]*cachedKey),
		cacheTTL:        cacheTTL,
		serverMasterKey: serverKey,
	}

	go svc.evictionLoop()

	return svc, nil
}

// ── Keychain Initialization ─────────────────────────────────────────────────

// InitResult is returned when a user's keychain is first created.
type InitResult struct {
	RecoveryPhrase string // 12-word BIP39 phrase — show once, never store
	MasterKeyHex   string // hex-encoded master key (for client-side caching)
}

// InitForUser creates a new keychain for a user. This is called once during
// user registration or on first vault access. Returns the recovery phrase
// which MUST be shown to the user exactly once.
func (svc *Service) InitForUser(ctx context.Context, userID uuid.UUID) (*InitResult, error) {
	// Check if already initialized.
	if _, err := svc.store.GetKeychain(ctx, userID); err == nil {
		return nil, fmt.Errorf("keychain: already initialized for user %s", userID)
	}

	// Generate master key.
	masterKey, err := GenerateMasterKey()
	if err != nil {
		return nil, err
	}

	// Generate salt.
	salt, err := GenerateSalt()
	if err != nil {
		return nil, err
	}

	// Derive sub-keys.
	dk, err := DeriveKeys(masterKey, salt)
	if err != nil {
		return nil, err
	}
	defer dk.Wipe()

	// Generate recovery phrase.
	phrase, err := GenerateRecoveryPhrase()
	if err != nil {
		return nil, err
	}

	// Compute auth key verifier (HMAC of fixed challenge — stored for verification).
	authVerifier := ComputeAuthKeyVerifier(dk.AuthKey)

	// Wrap the master key with the server KEK for server-side storage.
	wrappedMaster, err := WrapDEK(svc.serverMasterKey, masterKey)
	if err != nil {
		return nil, fmt.Errorf("keychain: wrap master key: %w", err)
	}

	// Store keychain metadata.
	kc := &UserKeychain{
		UserID:       userID,
		Salt:         salt,
		AuthKeyHash:  authVerifier,
		RecoveryHint: "",
		KeyVersion:   1,
	}
	if err := svc.store.InitKeychain(ctx, kc); err != nil {
		return nil, fmt.Errorf("keychain: init keychain record: %w", err)
	}

	// Store the wrapped master key as a special keychain secret.
	entry := &SecretEntry{
		UserID:     userID,
		Name:       "__master_key__",
		Ciphertext: wrappedMaster,
		WrappedDEK: nil, // master key is wrapped by server KEK, not by itself
		Version:    1,
		Category:   "system",
		Metadata:   "{}",
	}
	if err := svc.store.PutSecret(ctx, entry); err != nil {
		return nil, fmt.Errorf("keychain: store wrapped master key: %w", err)
	}

	svc.store.LogAccess(ctx, userID, AuditInit, "", "", "")

	return &InitResult{
		RecoveryPhrase: phrase,
		MasterKeyHex:   MasterKeyToHex(masterKey),
	}, nil
}

// ── Key Loading & Caching ───────────────────────────────────────────────────

// GetKeychain retrieves the user's keychain metadata (salt, key version, etc.).
// Returns nil and an error if the keychain is not initialized.
func (svc *Service) GetKeychain(ctx context.Context, userID uuid.UUID) (*UserKeychain, error) {
	return svc.store.GetKeychain(ctx, userID)
}

// LoadKeys loads and caches the user's derived keys. Called on authentication.
// The server unwraps the master key using the server KEK, then derives sub-keys.
func (svc *Service) LoadKeys(ctx context.Context, userID uuid.UUID) (*DerivedKeys, error) {
	// Check cache first.
	svc.mu.RLock()
	if ck, ok := svc.keyCache[userID]; ok && time.Now().Before(ck.expiresAt) {
		svc.mu.RUnlock()
		return ck.keys, nil
	}
	svc.mu.RUnlock()

	// Load keychain metadata.
	kc, err := svc.store.GetKeychain(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("keychain: user %s not initialized", userID)
	}

	// Load wrapped master key.
	masterEntry, err := svc.store.GetSecret(ctx, userID, "__master_key__")
	if err != nil {
		return nil, fmt.Errorf("keychain: master key not found for user %s", userID)
	}

	// Unwrap master key with server KEK.
	masterKey, err := UnwrapDEK(svc.serverMasterKey, masterEntry.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("keychain: unwrap master key: %w", err)
	}

	// Derive sub-keys.
	dk, err := DeriveKeys(masterKey, kc.Salt)
	if err != nil {
		return nil, fmt.Errorf("keychain: derive keys: %w", err)
	}

	// Cache.
	svc.mu.Lock()
	svc.keyCache[userID] = &cachedKey{
		keys:      dk,
		expiresAt: time.Now().Add(svc.cacheTTL),
	}
	svc.mu.Unlock()

	return dk, nil
}

// EvictKeys removes a user's keys from the cache (on logout or timeout).
func (svc *Service) EvictKeys(userID uuid.UUID) {
	svc.mu.Lock()
	if ck, ok := svc.keyCache[userID]; ok {
		ck.keys.Wipe()
		delete(svc.keyCache, userID)
	}
	svc.mu.Unlock()
}

// ── Secret Operations ───────────────────────────────────────────────────────

// PutSecret encrypts and stores a secret for a user.
func (svc *Service) PutSecret(ctx context.Context, userID uuid.UUID, name, category string, plaintext []byte, metadata string) error {
	dk, err := svc.LoadKeys(ctx, userID)
	if err != nil {
		return err
	}

	// Generate a fresh DEK for this secret.
	dek, err := GenerateDEK()
	if err != nil {
		return err
	}

	// Encrypt the plaintext with the DEK.
	ciphertext, err := EncryptSecret(dek, plaintext)
	if err != nil {
		return err
	}

	// Wrap the DEK with the user's encryption key.
	wrappedDEK, err := WrapDEK(dk.EncryptionKey, dek)
	if err != nil {
		return err
	}

	// Determine next version.
	var nextVersion int64 = 1
	existing, err := svc.store.GetSecret(ctx, userID, name)
	if err == nil {
		nextVersion = existing.Version + 1
	}

	entry := &SecretEntry{
		UserID:     userID,
		Name:       name,
		Ciphertext: ciphertext,
		WrappedDEK: wrappedDEK,
		Version:    nextVersion,
		Category:   category,
		Metadata:   metadata,
	}
	if existing != nil {
		entry.ID = existing.ID
		entry.CreatedAt = existing.CreatedAt
	}

	if err := svc.store.PutSecret(ctx, entry); err != nil {
		return err
	}

	svc.store.LogAccess(ctx, userID, AuditPutSecret, name, "", "")
	return nil
}

// GetSecret decrypts and returns a secret for a user.
func (svc *Service) GetSecret(ctx context.Context, userID uuid.UUID, name string) ([]byte, error) {
	dk, err := svc.LoadKeys(ctx, userID)
	if err != nil {
		return nil, err
	}

	entry, err := svc.store.GetSecret(ctx, userID, name)
	if err != nil {
		return nil, fmt.Errorf("keychain: secret %q not found", name)
	}

	// Unwrap the DEK.
	dek, err := UnwrapDEK(dk.EncryptionKey, entry.WrappedDEK)
	if err != nil {
		return nil, fmt.Errorf("keychain: unwrap DEK for %q: %w", name, err)
	}

	// Decrypt the secret.
	plaintext, err := DecryptSecret(dek, entry.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("keychain: decrypt %q: %w", name, err)
	}

	svc.store.LogAccess(ctx, userID, AuditGetSecret, name, "", "")
	return plaintext, nil
}

// DeleteSecret removes a secret.
func (svc *Service) DeleteSecret(ctx context.Context, userID uuid.UUID, name string) error {
	if err := svc.store.DeleteSecret(ctx, userID, name); err != nil {
		return err
	}
	svc.store.LogAccess(ctx, userID, AuditDeleteSecret, name, "", "")
	return nil
}

// ListSecretNames returns the names and versions of all secrets for a user
// (no plaintext or ciphertext — metadata only).
func (svc *Service) ListSecretNames(ctx context.Context, userID uuid.UUID) ([]SecretVersionEntry, error) {
	return svc.store.ListVersions(ctx, userID)
}

// ListAuditLog returns recent audit log entries for a user.
func (svc *Service) ListAuditLog(ctx context.Context, userID uuid.UUID, limit int) ([]AuditLogEntry, error) {
	return svc.store.ListAuditLog(ctx, userID, limit)
}

// ── Sync ────────────────────────────────────────────────────────────────────

// SyncRequest is the payload from a client that wants to synchronize secrets.
// The client sends its known version vector; the server returns only the
// secrets that have newer versions on the server side, plus a list of any
// secrets the server is missing so the client can push them.
type SyncRequest struct {
	// ClientVersions maps secret name → version the client currently holds.
	ClientVersions map[string]int64 `json:"client_versions"`
}

// SyncResponse is returned by the sync endpoint.
type SyncResponse struct {
	// Updated contains secrets that are newer on the server than the client.
	// Only metadata — the client must call GetSecret to decrypt each one.
	Updated []SecretVersionEntry `json:"updated"`

	// Deleted contains secret names that exist in the client's vector but
	// no longer exist on the server (were deleted).
	Deleted []string `json:"deleted"`

	// ServerVersions is the full version vector from the server so the
	// client can update its local cache in one shot.
	ServerVersions map[string]int64 `json:"server_versions"`
}

// SyncSecrets compares the client's version vector against the server state
// and returns a diff. This is the core sync negotiation — no ciphertext is
// transferred in this call; the client pulls individual secrets as needed.
func (svc *Service) SyncSecrets(ctx context.Context, userID uuid.UUID, req *SyncRequest) (*SyncResponse, error) {
	serverEntries, err := svc.store.ListVersions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("keychain: list versions for sync: %w", err)
	}

	// Build server version map.
	serverMap := make(map[string]SecretVersionEntry, len(serverEntries))
	serverVersions := make(map[string]int64, len(serverEntries))
	for _, e := range serverEntries {
		if e.Name == "__master_key__" {
			continue
		}
		serverMap[e.Name] = e
		serverVersions[e.Name] = e.Version
	}

	resp := &SyncResponse{
		Updated:        make([]SecretVersionEntry, 0),
		Deleted:        make([]string, 0),
		ServerVersions: serverVersions,
	}

	// Find secrets that are newer on the server.
	for name, entry := range serverMap {
		clientVer, exists := req.ClientVersions[name]
		if !exists || clientVer < entry.Version {
			resp.Updated = append(resp.Updated, entry)
		}
	}

	// Find secrets the client has that the server doesn't (deleted).
	for name := range req.ClientVersions {
		if name == "__master_key__" {
			continue
		}
		if _, exists := serverMap[name]; !exists {
			resp.Deleted = append(resp.Deleted, name)
		}
	}

	svc.store.LogAccess(ctx, userID, AuditSync, "", "", "")
	return resp, nil
}

// ── Key Rotation ────────────────────────────────────────────────────────────

// RotateKeys generates a new master key, re-wraps all DEKs, and updates
// the keychain. The old master key is needed to unwrap existing DEKs.
func (svc *Service) RotateKeys(ctx context.Context, userID uuid.UUID) error {
	// Load current keys.
	oldDK, err := svc.LoadKeys(ctx, userID)
	if err != nil {
		return err
	}

	kc, err := svc.store.GetKeychain(ctx, userID)
	if err != nil {
		return err
	}

	// Generate new master key and salt.
	newMaster, err := GenerateMasterKey()
	if err != nil {
		return err
	}
	newSalt, err := GenerateSalt()
	if err != nil {
		return err
	}

	newDK, err := DeriveKeys(newMaster, newSalt)
	if err != nil {
		return err
	}

	// Re-wrap all DEKs: unwrap with old key, re-wrap with new key.
	secrets, err := svc.store.ListSecrets(ctx, userID)
	if err != nil {
		return fmt.Errorf("keychain: list secrets for rotation: %w", err)
	}

	reWrapped := make(map[string][]byte, len(secrets))
	for _, s := range secrets {
		if s.Name == "__master_key__" || s.WrappedDEK == nil {
			continue
		}

		dek, err := UnwrapDEK(oldDK.EncryptionKey, s.WrappedDEK)
		if err != nil {
			slog.Error("keychain: rotation failed to unwrap DEK",
				"secret", s.Name, "error", err)
			continue
		}

		newWrapped, err := WrapDEK(newDK.EncryptionKey, dek)
		if err != nil {
			return fmt.Errorf("keychain: rotation re-wrap %q: %w", s.Name, err)
		}
		reWrapped[s.Name] = newWrapped
	}

	// Wrap new master key with server KEK.
	wrappedNewMaster, err := WrapDEK(svc.serverMasterKey, newMaster)
	if err != nil {
		return fmt.Errorf("keychain: wrap new master key: %w", err)
	}

	newVersion := kc.KeyVersion + 1
	newAuthVerifier := ComputeAuthKeyVerifier(newDK.AuthKey)

	// Persist all changes in a single transaction-like sequence.
	if err := svc.store.ReWrapAllDEKs(ctx, userID, reWrapped); err != nil {
		return fmt.Errorf("keychain: persist re-wrapped DEKs: %w", err)
	}

	// Update the stored master key.
	masterEntry := &SecretEntry{
		UserID:     userID,
		Name:       "__master_key__",
		Ciphertext: wrappedNewMaster,
		Version:    int64(newVersion),
		Category:   "system",
		Metadata:   "{}",
	}
	if err := svc.store.PutSecret(ctx, masterEntry); err != nil {
		return fmt.Errorf("keychain: update master key: %w", err)
	}

	// Update keychain metadata.
	if err := svc.store.UpdateKeychainKeys(ctx, userID, newSalt, newAuthVerifier, newVersion); err != nil {
		return fmt.Errorf("keychain: update keychain keys: %w", err)
	}

	// Evict old cached keys.
	svc.EvictKeys(userID)
	newDK.Wipe()

	svc.store.LogAccess(ctx, userID, AuditRotateKey, "", "", "")
	slog.Info("keychain: keys rotated", "user_id", userID, "new_version", newVersion,
		"secrets_rewrapped", len(reWrapped))
	return nil
}

// ── Recovery ────────────────────────────────────────────────────────────────

// RecoverFromPhrase reconstructs the master key from a recovery phrase and
// re-initializes the keychain. This generates a NEW master key and re-wraps
// all DEKs. The recovery phrase is used to derive the old master key for
// unwrapping.
func (svc *Service) RecoverFromPhrase(ctx context.Context, userID uuid.UUID, phrase string) error {
	oldMaster, err := RecoveryPhraseToMasterKey(phrase)
	if err != nil {
		return err
	}

	kc, err := svc.store.GetKeychain(ctx, userID)
	if err != nil {
		return fmt.Errorf("keychain: no keychain found for user %s", userID)
	}

	// Derive old keys.
	oldDK, err := DeriveKeys(oldMaster, kc.Salt)
	if err != nil {
		return fmt.Errorf("keychain: derive keys from phrase: %w", err)
	}

	// Verify the auth key matches.
	expectedVerifier := ComputeAuthKeyVerifier(oldDK.AuthKey)
	if expectedVerifier != kc.AuthKeyHash {
		oldDK.Wipe()
		return fmt.Errorf("keychain: recovery phrase does not match")
	}

	oldDK.Wipe()

	// Phrase is valid. Rotate to a new master key (re-wraps all DEKs).
	svc.store.LogAccess(ctx, userID, AuditRecover, "", "", "")
	return svc.RotateKeys(ctx, userID)
}

// ── Compatibility: SecretStore Interface ─────────────────────────────────────

// UserScopedService wraps the Service to provide a per-user view that
// implements the legacy SecretStore interface for backward compatibility
// with existing VCS, LLM, and MCP code paths.
type UserScopedService struct {
	svc    *Service
	userID uuid.UUID
}

// ForUser returns a user-scoped view of the keychain service.
func (svc *Service) ForUser(userID uuid.UUID) *UserScopedService {
	return &UserScopedService{svc: svc, userID: userID}
}

// Get retrieves a decrypted secret by name.
func (u *UserScopedService) Get(key string) (string, error) {
	plaintext, err := u.svc.GetSecret(context.Background(), u.userID, key)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return string(plaintext), nil
}

// Set encrypts and stores a secret.
func (u *UserScopedService) Set(key, value string) error {
	if value == "" {
		return u.Delete(key)
	}
	return u.svc.PutSecret(context.Background(), u.userID, key, categorize(key), []byte(value), "{}")
}

// Delete removes a secret.
func (u *UserScopedService) Delete(key string) error {
	return u.svc.DeleteSecret(context.Background(), u.userID, key)
}

// List returns all secret names for this user.
func (u *UserScopedService) List() ([]string, error) {
	versions, err := u.svc.ListSecretNames(context.Background(), u.userID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(versions))
	for _, v := range versions {
		if v.Name != "__master_key__" {
			names = append(names, v.Name)
		}
	}
	return names, nil
}

// GetAll returns all decrypted secrets for this user.
func (u *UserScopedService) GetAll() (map[string]string, error) {
	entries, err := u.svc.store.ListSecrets(context.Background(), u.userID)
	if err != nil {
		return nil, err
	}

	dk, err := u.svc.LoadKeys(context.Background(), u.userID)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.Name == "__master_key__" || e.WrappedDEK == nil {
			continue
		}
		dek, err := UnwrapDEK(dk.EncryptionKey, e.WrappedDEK)
		if err != nil {
			slog.Warn("keychain: skip secret (unwrap failed)", "name", e.Name, "error", err)
			continue
		}
		plaintext, err := DecryptSecret(dek, e.Ciphertext)
		if err != nil {
			slog.Warn("keychain: skip secret (decrypt failed)", "name", e.Name, "error", err)
			continue
		}
		result[e.Name] = string(plaintext)
	}
	return result, nil
}

// categorize infers the category from a secret key name.
func categorize(key string) string {
	if len(key) > 4 {
		switch key[:4] {
		case "vcs.":
			return "vcs"
		case "llm.":
			return "llm"
		case "mcp:":
			return "mcp"
		}
	}
	return "custom"
}

// ── Background Eviction ─────────────────────────────────────────────────────

func (svc *Service) evictionLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		svc.mu.Lock()
		now := time.Now()
		for uid, ck := range svc.keyCache {
			if now.After(ck.expiresAt) {
				ck.keys.Wipe()
				delete(svc.keyCache, uid)
			}
		}
		svc.mu.Unlock()
	}
}

// ── Hex Helpers ─────────────────────────────────────────────────────────────

// AuthKeyHashFromHex decodes a stored auth key hash.
func AuthKeyHashFromHex(h string) ([]byte, error) {
	return hex.DecodeString(h)
}
