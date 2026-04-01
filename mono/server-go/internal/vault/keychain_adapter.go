package vault

import (
	"github.com/AuralithAI/rtvortex-server/internal/vault/keychain"
	"github.com/google/uuid"
)

// KeychainAdapter implements SecretStore by delegating to the keychain
// service for a specific user. It bridges the new per-user encrypted
// keychain storage with the legacy SecretStore interface so that all
// existing consumers (LLM Registry, MCP, VCS, handlers) work without
// changes.
type KeychainAdapter struct {
	service *keychain.Service
}

// NewKeychainAdapter wraps a keychain.Service as a SecretStore factory.
func NewKeychainAdapter(svc *keychain.Service) *KeychainAdapter {
	return &KeychainAdapter{service: svc}
}

// ForUser returns a user-scoped SecretStore backed by the keychain.
// This replaces NewUserScopedVault for code paths that have access to
// the user's UUID.
func (a *KeychainAdapter) ForUser(userID uuid.UUID) SecretStore {
	return a.service.ForUser(userID)
}

// Service returns the underlying keychain service for operations that
// need more than the SecretStore interface (init, rotate, recover).
func (a *KeychainAdapter) Service() *keychain.Service {
	return a.service
}

// VCSKeychainAdapter adapts the keychain to the vcs.VaultReader interface.
// Unlike the FileVault-based adapter, this resolves secrets by user ID
// rather than vault token, since the keychain is already user-scoped.
type VCSKeychainAdapter struct {
	adapter  *KeychainAdapter
	resolver UserIDResolver
}

// UserIDResolver looks up a user ID given a vault token. This is needed
// because the VCS resolver only has vault tokens, not user IDs.
type UserIDResolver interface {
	UserIDFromVaultToken(token string) (uuid.UUID, error)
}

// NewVCSKeychainAdapter creates a VCS-compatible vault reader backed by
// the keychain.
func NewVCSKeychainAdapter(adapter *KeychainAdapter, resolver UserIDResolver) *VCSKeychainAdapter {
	return &VCSKeychainAdapter{adapter: adapter, resolver: resolver}
}

// GetScoped resolves a vault-token to a user ID, then reads the secret
// from that user's keychain.
func (a *VCSKeychainAdapter) GetScoped(vaultToken, key string) (string, error) {
	userID, err := a.resolver.UserIDFromVaultToken(vaultToken)
	if err != nil {
		return "", err
	}
	scoped := a.adapter.ForUser(userID)
	return scoped.Get(key)
}
