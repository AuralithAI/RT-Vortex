// Package vault — UserScopedVault wraps a SecretStore to isolate secrets per user.
//
// Each user has a unique vault token (64-char hex string, 32 bytes of
// crypto/rand entropy) generated on user creation and stored in PostgreSQL.
// All keys are prefixed with "user.<vault_token>." so that different users
// never collide or see each other's secrets in the same backing store.
//
// The vault token is stored in the users table — it is never exposed to the
// frontend. Server-side, the authenticated user's vault token is resolved
// from the DB and used to scope all vault operations.
package vault

import (
	"fmt"
	"strings"
)

// UserScopedVault wraps a SecretStore and prefixes all keys with a user-specific
// namespace derived from the user's vault token. This means N users can share
// the same backing file vault and their secrets never collide.
type UserScopedVault struct {
	inner      SecretStore
	vaultToken string // 64-char hex string (32 bytes crypto-random), unique per user
}

// NewUserScopedVault creates a scoped vault for a specific user.
// vaultToken must be a non-empty, unique-per-user identifier.
func NewUserScopedVault(inner SecretStore, vaultToken string) *UserScopedVault {
	return &UserScopedVault{
		inner:      inner,
		vaultToken: vaultToken,
	}
}

// prefix returns the namespace prefix for this user's secrets.
func (v *UserScopedVault) prefix() string {
	return fmt.Sprintf("user.%s.", v.vaultToken)
}

// scopedKey adds the user prefix.
func (v *UserScopedVault) scopedKey(key string) string {
	return v.prefix() + key
}

// Get retrieves a secret scoped to this user.
func (v *UserScopedVault) Get(key string) (string, error) {
	return v.inner.Get(v.scopedKey(key))
}

// Set stores a secret scoped to this user.
func (v *UserScopedVault) Set(key, value string) error {
	return v.inner.Set(v.scopedKey(key), value)
}

// Delete removes a secret scoped to this user.
func (v *UserScopedVault) Delete(key string) error {
	return v.inner.Delete(v.scopedKey(key))
}

// List returns all key names stored for this user (without the prefix).
func (v *UserScopedVault) List() ([]string, error) {
	all, err := v.inner.List()
	if err != nil {
		return nil, err
	}
	prefix := v.prefix()
	var keys []string
	for _, k := range all {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, strings.TrimPrefix(k, prefix))
		}
	}
	return keys, nil
}

// GetAll returns all key-value pairs for this user (keys without prefix).
func (v *UserScopedVault) GetAll() (map[string]string, error) {
	all, err := v.inner.GetAll()
	if err != nil {
		return nil, err
	}
	prefix := v.prefix()
	result := make(map[string]string)
	for k, val := range all {
		if strings.HasPrefix(k, prefix) {
			result[strings.TrimPrefix(k, prefix)] = val
		}
	}
	return result, nil
}
