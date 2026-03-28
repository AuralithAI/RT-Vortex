// Package crypto provides AES-256-GCM encryption for sensitive data at rest.
//
// All ciphertext is encoded as hex(nonce || ciphertext) so it can be stored
// directly in a TEXT database column.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// ErrInvalidKey is returned when the encryption key is not 32 bytes (256 bits).
var ErrInvalidKey = errors.New("encryption key must be exactly 32 bytes (256 bits)")

// ErrCiphertextTooShort is returned when decrypting data shorter than a nonce.
var ErrCiphertextTooShort = errors.New("ciphertext too short")

// TokenEncryptor provides AES-256-GCM encryption and decryption for OAuth
// access tokens and refresh tokens stored in the database.
type TokenEncryptor struct {
	gcm cipher.AEAD
}

// NewTokenEncryptor creates an encryptor from a 32-byte key.
//
// The key can be provided as:
//   - A 64-character hex string (decoded to 32 bytes), or
//   - A raw 32-byte string.
//
// If the key is empty, a no-op encryptor is returned that passes values
// through unencrypted (useful for local development with a warning).
func NewTokenEncryptor(key string) (*TokenEncryptor, error) {
	if key == "" {
		return &TokenEncryptor{gcm: nil}, nil
	}

	keyBytes, err := normalizeKey(key)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	return &TokenEncryptor{gcm: gcm}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM and returns hex-encoded
// nonce+ciphertext. Returns the plaintext unchanged if no key is configured.
func (e *TokenEncryptor) Encrypt(plaintext string) (string, error) {
	if e.gcm == nil || plaintext == "" {
		return plaintext, nil
	}

	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// Decrypt decrypts hex-encoded nonce+ciphertext produced by Encrypt.
// Returns the input unchanged if no key is configured.
func (e *TokenEncryptor) Decrypt(encoded string) (string, error) {
	if e.gcm == nil || encoded == "" {
		return encoded, nil
	}

	data, err := hex.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode hex: %w", err)
	}

	nonceSize := e.gcm.NonceSize()
	if len(data) < nonceSize {
		return "", ErrCiphertextTooShort
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

// IsEnabled returns true if an encryption key is configured.
func (e *TokenEncryptor) IsEnabled() bool {
	return e.gcm != nil
}

// normalizeKey accepts either a 64-char hex string or a raw 32-byte string.
func normalizeKey(key string) ([]byte, error) {
	// Try hex-decode first (64 hex chars → 32 bytes).
	if len(key) == 64 {
		if b, err := hex.DecodeString(key); err == nil {
			return b, nil
		}
	}
	// Treat as raw bytes.
	if len(key) != 32 {
		return nil, ErrInvalidKey
	}
	return []byte(key), nil
}
