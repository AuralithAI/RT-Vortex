// Package vault provides a pluggable secret storage interface with a file-based
// implementation for local development. In production, swap in HashiCorp Vault,
// AWS Secrets Manager, GCP Secret Manager, etc.
//
// The file vault stores secrets as an AES-256-GCM encrypted JSON file at:
//
//	RTVORTEX_HOME/config/.vault.enc
//
// The encryption key is derived from the same TOKEN_ENCRYPTION_KEY used for
// OAuth token encryption (cfg.Auth.EncryptionKey).
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// ── Interface ───────────────────────────────────────────────────────────────

// SecretStore is the pluggable interface for secret persistence.
// Implementations must be safe for concurrent use.
type SecretStore interface {
	// Get retrieves a secret by key. Returns ("", nil) if not found.
	Get(key string) (string, error)

	// Set stores a secret. An empty value deletes the key.
	Set(key, value string) error

	// Delete removes a secret.
	Delete(key string) error

	// List returns all stored key names (not values).
	List() ([]string, error)

	// GetAll returns all stored key-value pairs. Useful for bulk loading.
	GetAll() (map[string]string, error)
}

// ── File Vault Implementation ───────────────────────────────────────────────

// FileVault implements SecretStore using an AES-256-GCM encrypted JSON file.
// It is designed for local development and small deployments. For production,
// consider HashiCorp Vault or a cloud secret manager.
type FileVault struct {
	mu        sync.RWMutex
	path      string      // path to the encrypted vault file
	gcm       cipher.AEAD // AES-256-GCM cipher (nil = plaintext fallback)
	secrets   map[string]string
	noEncrypt bool // true when no encryption key was provided
}

// FileVaultOption configures a FileVault.
type FileVaultOption func(*FileVault)

// WithEncryptionKey sets the AES-256 key for encrypting the vault file.
// The key can be a 64-character hex string (decoded to 32 bytes) or a raw
// 32-byte string. If empty, secrets are stored as plaintext JSON (with a
// warning logged).
func WithEncryptionKey(key string) FileVaultOption {
	return func(v *FileVault) {
		if key == "" {
			v.noEncrypt = true
			return
		}
		keyBytes, err := normalizeKey(key)
		if err != nil {
			slog.Warn("vault: invalid encryption key, falling back to plaintext", "error", err)
			v.noEncrypt = true
			return
		}
		block, err := aes.NewCipher(keyBytes)
		if err != nil {
			slog.Warn("vault: failed to create AES cipher, falling back to plaintext", "error", err)
			v.noEncrypt = true
			return
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			slog.Warn("vault: failed to create GCM, falling back to plaintext", "error", err)
			v.noEncrypt = true
			return
		}
		v.gcm = gcm
	}
}

// NewFileVault creates a file-based vault at the given path.
// It loads existing secrets from disk if the file exists.
func NewFileVault(path string, opts ...FileVaultOption) (*FileVault, error) {
	v := &FileVault{
		path:    path,
		secrets: make(map[string]string),
	}
	for _, opt := range opts {
		opt(v)
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("vault: create directory %s: %w", dir, err)
	}

	// Load existing secrets from disk.
	if err := v.load(); err != nil {
		// If the file doesn't exist, that's fine — start empty.
		if !os.IsNotExist(err) {
			slog.Warn("vault: failed to load existing secrets, starting fresh", "error", err)
		}
	}

	mode := "encrypted"
	if v.noEncrypt {
		mode = "plaintext (no encryption key)"
	}
	slog.Info("vault: file vault initialized", "path", path, "mode", mode, "secrets", len(v.secrets))

	return v, nil
}

// Get retrieves a secret by key.
func (v *FileVault) Get(key string) (string, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.secrets[key], nil
}

// Set stores a secret and persists to disk. Empty value deletes the key.
func (v *FileVault) Set(key, value string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if value == "" {
		delete(v.secrets, key)
	} else {
		v.secrets[key] = value
	}
	return v.save()
}

// Delete removes a secret and persists to disk.
func (v *FileVault) Delete(key string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if _, ok := v.secrets[key]; !ok {
		return nil // nothing to delete
	}
	delete(v.secrets, key)
	return v.save()
}

// List returns all stored key names.
func (v *FileVault) List() ([]string, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	keys := make([]string, 0, len(v.secrets))
	for k := range v.secrets {
		keys = append(keys, k)
	}
	return keys, nil
}

// GetAll returns all stored key-value pairs.
func (v *FileVault) GetAll() (map[string]string, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	out := make(map[string]string, len(v.secrets))
	for k, val := range v.secrets {
		out[k] = val
	}
	return out, nil
}

// ── Internal persistence ────────────────────────────────────────────────────

// load reads and decrypts the vault file.
func (v *FileVault) load() error {
	data, err := os.ReadFile(v.path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}

	var jsonBytes []byte

	if v.gcm != nil {
		// Decrypt hex-encoded nonce+ciphertext.
		raw, err := hex.DecodeString(string(data))
		if err != nil {
			return fmt.Errorf("vault: decode hex: %w", err)
		}
		nonceSize := v.gcm.NonceSize()
		if len(raw) < nonceSize {
			return fmt.Errorf("vault: ciphertext too short")
		}
		nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
		jsonBytes, err = v.gcm.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			return fmt.Errorf("vault: decrypt: %w", err)
		}
	} else {
		jsonBytes = data
	}

	return json.Unmarshal(jsonBytes, &v.secrets)
}

// save encrypts and writes the vault file to disk atomically.
func (v *FileVault) save() error {
	jsonBytes, err := json.MarshalIndent(v.secrets, "", "  ")
	if err != nil {
		return fmt.Errorf("vault: marshal: %w", err)
	}

	var fileData []byte

	if v.gcm != nil {
		// Encrypt with AES-256-GCM.
		nonce := make([]byte, v.gcm.NonceSize())
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return fmt.Errorf("vault: generate nonce: %w", err)
		}
		ciphertext := v.gcm.Seal(nonce, nonce, jsonBytes, nil)
		fileData = []byte(hex.EncodeToString(ciphertext))
	} else {
		fileData = jsonBytes
	}

	// Atomic write: write to temp file then rename.
	tmpPath := v.path + ".tmp"
	if err := os.WriteFile(tmpPath, fileData, 0o600); err != nil {
		return fmt.Errorf("vault: write temp file: %w", err)
	}
	if err := os.Rename(tmpPath, v.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("vault: rename: %w", err)
	}

	return nil
}

// ── Key normalization (shared with crypto package) ──────────────────────────

func normalizeKey(key string) ([]byte, error) {
	// Try hex decode first (64 hex chars → 32 bytes).
	if len(key) == 64 {
		b, err := hex.DecodeString(key)
		if err == nil && len(b) == 32 {
			return b, nil
		}
	}
	// Raw 32-byte key.
	if len(key) == 32 {
		return []byte(key), nil
	}
	return nil, errors.New("encryption key must be 32 bytes (or 64 hex characters)")
}
