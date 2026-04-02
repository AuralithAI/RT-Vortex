package vault

import (
	"github.com/AuralithAI/rtvortex-server/internal/vcs"
)

// ── VCS Vault Adapter ───────────────────────────────────────────────────────

// VCSVaultAdapter adapts a SecretStore to the vcs.VaultReader interface so that
// the VCS resolver can read per-user secrets without importing the vault
// package directly.
type VCSVaultAdapter struct {
	inner SecretStore
}

// NewVCSVaultAdapter creates a vault adapter for the VCS resolver.
func NewVCSVaultAdapter(v SecretStore) *VCSVaultAdapter {
	return &VCSVaultAdapter{inner: v}
}

// GetScoped reads a secret from the vault scoped to a specific user's vault
// token.  This creates an ephemeral UserScopedVault for the lookup.
func (a *VCSVaultAdapter) GetScoped(vaultToken, key string) (string, error) {
	scoped := NewUserScopedVault(a.inner, vaultToken)
	return scoped.Get(key)
}

// Ensure VCSVaultAdapter implements vcs.VaultReader at compile time.
var _ vcs.VaultReader = (*VCSVaultAdapter)(nil)
