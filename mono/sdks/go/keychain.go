package rtvortex

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// KeychainClient provides methods for the /api/v1/keychain endpoints.
// It allows VS Code extensions, JetBrains plugins, and other integrations
// to manage the encrypted per-user secret vault through the same HTTP API
// used by the web UI.
type KeychainClient struct {
	c *Client
}

// ── Types ───────────────────────────────────────────────────────────────────

// KeychainStatus represents the initialization state of a user's keychain.
type KeychainStatus struct {
	Initialized bool `json:"initialized"`
	KeyVersion  int  `json:"key_version"`
	SecretCount int  `json:"secret_count"`
}

// KeychainInitResponse is returned when a keychain is first initialized.
type KeychainInitResponse struct {
	RecoveryPhrase string `json:"recovery_phrase"`
}

// KeychainSecret contains a decrypted secret.
type KeychainSecret struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// KeychainSecretListEntry is a metadata-only view of a stored secret.
type KeychainSecretListEntry struct {
	Name      string `json:"name"`
	Version   int64  `json:"version"`
	Category  string `json:"category,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// KeychainPutSecretRequest is the payload for storing a secret.
type KeychainPutSecretRequest struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Category string `json:"category,omitempty"`
	Metadata string `json:"metadata,omitempty"`
}

// KeychainRecoverRequest is the payload for recovering a keychain.
type KeychainRecoverRequest struct {
	RecoveryPhrase string `json:"recovery_phrase"`
}

// KeychainAuditLogEntry represents one audit event.
type KeychainAuditLogEntry struct {
	ID         string `json:"id"`
	Action     string `json:"action"`
	SecretName string `json:"secret_name,omitempty"`
	IPAddr     string `json:"ip_addr,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// KeychainSyncRequest holds the client's version vector for sync negotiation.
type KeychainSyncRequest struct {
	ClientVersions map[string]int64 `json:"client_versions"`
}

// KeychainSyncVersionEntry describes a secret that changed on the server.
type KeychainSyncVersionEntry struct {
	Name      string `json:"name"`
	Version   int64  `json:"version"`
	Category  string `json:"category,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// KeychainSyncResponse is the result of a sync negotiation.
type KeychainSyncResponse struct {
	Updated        []KeychainSyncVersionEntry `json:"updated"`
	Deleted        []string                   `json:"deleted"`
	ServerVersions map[string]int64           `json:"server_versions"`
}

// ── Methods ─────────────────────────────────────────────────────────────────

// Status returns whether the user's keychain is initialized.
func (k *KeychainClient) Status(ctx context.Context) (*KeychainStatus, error) {
	var out KeychainStatus
	err := k.c.do(ctx, http.MethodGet, "/api/v1/keychain/status", nil, &out)
	return &out, err
}

// Init creates a new keychain for the user. The returned recovery phrase
// MUST be shown to the user exactly once — it is never stored server-side.
func (k *KeychainClient) Init(ctx context.Context) (*KeychainInitResponse, error) {
	var out KeychainInitResponse
	err := k.c.do(ctx, http.MethodPost, "/api/v1/keychain/init", nil, &out)
	return &out, err
}

// PutSecret stores an encrypted secret.
func (k *KeychainClient) PutSecret(ctx context.Context, req KeychainPutSecretRequest) error {
	return k.c.do(ctx, http.MethodPut, "/api/v1/keychain/secrets", req, nil)
}

// GetSecret retrieves a single decrypted secret by name.
func (k *KeychainClient) GetSecret(ctx context.Context, name string) (*KeychainSecret, error) {
	var out KeychainSecret
	err := k.c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/keychain/secret?name=%s", name), nil, &out)
	return &out, err
}

// ListSecrets returns metadata for all secrets (no plaintext).
func (k *KeychainClient) ListSecrets(ctx context.Context) ([]KeychainSecretListEntry, error) {
	var out []KeychainSecretListEntry
	err := k.c.do(ctx, http.MethodGet, "/api/v1/keychain/secrets", nil, &out)
	return out, err
}

// DeleteSecret removes a secret by name.
func (k *KeychainClient) DeleteSecret(ctx context.Context, name string) error {
	return k.c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/keychain/secret?name=%s", name), nil, nil)
}

// RotateKeys triggers a full key rotation (re-wraps all DEKs).
func (k *KeychainClient) RotateKeys(ctx context.Context) error {
	return k.c.do(ctx, http.MethodPost, "/api/v1/keychain/rotate", nil, nil)
}

// Recover restores a keychain from a BIP39 recovery phrase.
func (k *KeychainClient) Recover(ctx context.Context, req KeychainRecoverRequest) error {
	return k.c.do(ctx, http.MethodPost, "/api/v1/keychain/recover", req, nil)
}

// Sync performs version-vector sync negotiation and returns the diff.
func (k *KeychainClient) Sync(ctx context.Context, req KeychainSyncRequest) (*KeychainSyncResponse, error) {
	var out KeychainSyncResponse
	err := k.c.do(ctx, http.MethodPost, "/api/v1/keychain/sync", req, &out)
	return &out, err
}

// AuditLog returns recent audit events for the user's keychain.
func (k *KeychainClient) AuditLog(ctx context.Context, limit int) ([]KeychainAuditLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []KeychainAuditLogEntry
	err := k.c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/keychain/audit?limit=%d", limit), nil, &out)
	return out, err
}

// ── Convenience helpers for extension integrations ──────────────────────────

// EnsureInitialized checks if the keychain is initialized, and if not,
// initializes it. Returns the recovery phrase if newly initialized, or
// empty string if already existed. Extensions should display the phrase
// to the user when non-empty.
func (k *KeychainClient) EnsureInitialized(ctx context.Context) (recoveryPhrase string, err error) {
	status, err := k.Status(ctx)
	if err != nil {
		return "", fmt.Errorf("keychain: check status: %w", err)
	}
	if status.Initialized {
		return "", nil
	}
	init, err := k.Init(ctx)
	if err != nil {
		return "", fmt.Errorf("keychain: init: %w", err)
	}
	return init.RecoveryPhrase, nil
}

// SyncAll performs a full sync: sends empty version vector, receives all
// server-side secrets. Useful for first-time extension startup.
func (k *KeychainClient) SyncAll(ctx context.Context) (*KeychainSyncResponse, error) {
	return k.Sync(ctx, KeychainSyncRequest{
		ClientVersions: make(map[string]int64),
	})
}

// ── Unused but reserved for future use ──────────────────────────────────────

var _ = time.Now // ensure time import is used
