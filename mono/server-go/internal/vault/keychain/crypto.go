// Package keychain implements a production-grade secret vault inspired by
// Apple's iCloud Keychain architecture (2026 model).
//
// Key hierarchy (iCloud-style dual-wrap):
//
//	Random Master Key (256-bit, crypto/rand)
//	  ├→ Encryption Key (AES-256-GCM — wraps individual secret DEKs)
//	  ├→ Auth Key        (HMAC-SHA256 — challenge-response with server)
//	  └→ Sync Key        (X25519     — future device-to-device exchange)
//
//	Server KEK (from env/KMS)
//	  └→ KEK-wrapped master key      (fast path — primary device)
//
//	Recovery Phrase (BIP39 128-bit, shown once)
//	  └→ Argon2id wrapping key
//	       └→ Phrase-wrapped master key (recovery path — stored server-side)
//
// Every individual secret is encrypted with its own random DEK (data
// encryption key).  The DEK is then wrapped (encrypted) with the user's
// Encryption Key.  This two-layer scheme means rotating the master key
// only requires re-wrapping DEKs, not re-encrypting every secret value.
//
// The master key has full 256-bit entropy (random). The recovery phrase
// is NOT the master key — it wraps/derives a wrapping key that can unlock
// the real master. The server stores two ciphertext blobs per user:
//  1. KEK-wrapped master key  (fast path, used on primary device)
//  2. Phrase-wrapped master key (recovery path, used when entering phrase)
//
// Server is a blind relay — it never sees plaintext master keys.
//
// Primitives:
//   - AES-256-GCM for authenticated encryption
//   - HKDF-SHA256 for sub-key derivation
//   - Argon2id for recovery-phrase-to-wrapping-key derivation
//   - HMAC-SHA256 for auth challenge-response
//   - BIP39 for human-readable recovery phrases
package keychain

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/hkdf"
)

// Key sizes.
const (
	MasterKeySize     = 32 // 256-bit master key
	EncryptionKeySize = 32 // AES-256
	AuthKeySize       = 32 // HMAC-SHA256
	SyncKeySize       = 32 // X25519 seed (reserved for future device linking)
	DEKSize           = 32 // per-secret data encryption key
	NonceSize         = 12 // AES-256-GCM nonce
	SaltSize          = 16 // HKDF salt
)

// HKDF info strings for domain separation.
var (
	hkdfInfoEncryption = []byte("rtvortex-keychain-encryption-v1")
	hkdfInfoAuth       = []byte("rtvortex-keychain-auth-v1")
	hkdfInfoSync       = []byte("rtvortex-keychain-sync-v1")
)

// DerivedKeys holds the three keys derived from a master key.
type DerivedKeys struct {
	EncryptionKey [EncryptionKeySize]byte
	AuthKey       [AuthKeySize]byte
	SyncKey       [SyncKeySize]byte
}

// Wipe zeroes all key material.
func (dk *DerivedKeys) Wipe() {
	wipeBytes(dk.EncryptionKey[:])
	wipeBytes(dk.AuthKey[:])
	wipeBytes(dk.SyncKey[:])
}

// GenerateMasterKey creates a new 256-bit cryptographically random master key.
func GenerateMasterKey() ([MasterKeySize]byte, error) {
	var key [MasterKeySize]byte
	if _, err := io.ReadFull(rand.Reader, key[:]); err != nil {
		return key, fmt.Errorf("keychain: generate master key: %w", err)
	}
	return key, nil
}

// DeriveKeys derives the three sub-keys from a master key and salt using HKDF-SHA256.
// The salt must be unique per user and stored alongside the user record.
func DeriveKeys(masterKey [MasterKeySize]byte, salt []byte) (*DerivedKeys, error) {
	if len(salt) < SaltSize {
		return nil, errors.New("keychain: salt must be at least 16 bytes")
	}

	dk := &DerivedKeys{}

	if err := hkdfExpand(masterKey[:], salt, hkdfInfoEncryption, dk.EncryptionKey[:]); err != nil {
		return nil, fmt.Errorf("keychain: derive encryption key: %w", err)
	}
	if err := hkdfExpand(masterKey[:], salt, hkdfInfoAuth, dk.AuthKey[:]); err != nil {
		return nil, fmt.Errorf("keychain: derive auth key: %w", err)
	}
	if err := hkdfExpand(masterKey[:], salt, hkdfInfoSync, dk.SyncKey[:]); err != nil {
		return nil, fmt.Errorf("keychain: derive sync key: %w", err)
	}

	return dk, nil
}

// ── Data Encryption Key (DEK) Operations ────────────────────────────────────

// GenerateDEK creates a new random 256-bit data encryption key.
func GenerateDEK() ([DEKSize]byte, error) {
	var dek [DEKSize]byte
	if _, err := io.ReadFull(rand.Reader, dek[:]); err != nil {
		return dek, fmt.Errorf("keychain: generate DEK: %w", err)
	}
	return dek, nil
}

// WrapDEK encrypts a DEK with the user's encryption key (AES-256-GCM).
// Returns nonce || ciphertext.
func WrapDEK(encryptionKey [EncryptionKeySize]byte, dek [DEKSize]byte) ([]byte, error) {
	gcm, err := newGCM(encryptionKey[:])
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("keychain: generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, dek[:], nil)
	return ciphertext, nil
}

// UnwrapDEK decrypts a wrapped DEK with the user's encryption key.
// Input is nonce || ciphertext as returned by WrapDEK.
func UnwrapDEK(encryptionKey [EncryptionKeySize]byte, wrappedDEK []byte) ([DEKSize]byte, error) {
	var dek [DEKSize]byte

	gcm, err := newGCM(encryptionKey[:])
	if err != nil {
		return dek, err
	}

	nonceSize := gcm.NonceSize()
	if len(wrappedDEK) < nonceSize+1 {
		return dek, errors.New("keychain: wrapped DEK too short")
	}

	nonce, ciphertext := wrappedDEK[:nonceSize], wrappedDEK[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return dek, fmt.Errorf("keychain: unwrap DEK: %w", err)
	}

	if len(plaintext) != DEKSize {
		return dek, fmt.Errorf("keychain: unwrapped DEK has wrong size (%d)", len(plaintext))
	}
	copy(dek[:], plaintext)
	wipeBytes(plaintext)
	return dek, nil
}

// ── Secret Encryption ───────────────────────────────────────────────────────

// EncryptSecret encrypts a plaintext secret with a DEK (AES-256-GCM).
// Returns nonce || ciphertext || tag.
func EncryptSecret(dek [DEKSize]byte, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(dek[:])
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("keychain: generate nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptSecret decrypts a ciphertext produced by EncryptSecret.
func DecryptSecret(dek [DEKSize]byte, ciphertext []byte) ([]byte, error) {
	gcm, err := newGCM(dek[:])
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize+1 {
		return nil, errors.New("keychain: ciphertext too short")
	}

	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("keychain: decrypt: %w", err)
	}
	return plaintext, nil
}

// ── Auth Challenge-Response ─────────────────────────────────────────────────

// ComputeAuthProof computes HMAC-SHA256(authKey, challenge) for zero-knowledge
// proof of key ownership.
func ComputeAuthProof(authKey [AuthKeySize]byte, challenge []byte) []byte {
	mac := hmac.New(sha256.New, authKey[:])
	mac.Write(challenge)
	return mac.Sum(nil)
}

// VerifyAuthProof checks an HMAC proof against the expected value.
func VerifyAuthProof(authKey [AuthKeySize]byte, challenge, proof []byte) bool {
	expected := ComputeAuthProof(authKey, challenge)
	return hmac.Equal(expected, proof)
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func hkdfExpand(secret, salt, info, out []byte) error {
	reader := hkdf.New(sha256.New, secret, salt, info)
	_, err := io.ReadFull(reader, out)
	return err
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("keychain: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("keychain: create GCM: %w", err)
	}
	return gcm, nil
}

// GenerateSalt creates a cryptographically random salt of SaltSize bytes.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("keychain: generate salt: %w", err)
	}
	return salt, nil
}

// GenerateChallenge creates a random 32-byte challenge for auth proofs.
func GenerateChallenge() ([]byte, error) {
	challenge := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, challenge); err != nil {
		return nil, fmt.Errorf("keychain: generate challenge: %w", err)
	}
	return challenge, nil
}

// MasterKeyToHex encodes a master key as a 64-character hex string.
func MasterKeyToHex(key [MasterKeySize]byte) string {
	return hex.EncodeToString(key[:])
}

// MasterKeyFromHex decodes a 64-character hex string into a master key.
func MasterKeyFromHex(h string) ([MasterKeySize]byte, error) {
	var key [MasterKeySize]byte
	b, err := hex.DecodeString(h)
	if err != nil {
		return key, fmt.Errorf("keychain: decode hex: %w", err)
	}
	if len(b) != MasterKeySize {
		return key, fmt.Errorf("keychain: hex key has wrong length (%d)", len(b))
	}
	copy(key[:], b)
	return key, nil
}

func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// ── Recovery Wrapping Key (Argon2id) ────────────────────────────────────────

// Argon2id parameters for recovery-phrase → wrapping-key derivation.
// These are tuned for server-side one-shot operations (init + recovery),
// not interactive login, so we use stronger parameters.
const (
	argon2Time       = 3         // iterations
	argon2Memory     = 64 * 1024 // 64 MiB
	argon2Threads    = 4
	RecoverySaltSize = 16 // separate salt for the recovery wrapping key
)

// GenerateRecoverySalt creates a cryptographically random salt for Argon2id
// recovery key derivation. This salt is stored alongside the user's keychain
// metadata (NOT secret — its purpose is to prevent rainbow tables).
func GenerateRecoverySalt() ([]byte, error) {
	salt := make([]byte, RecoverySaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("keychain: generate recovery salt: %w", err)
	}
	return salt, nil
}

// DeriveRecoveryWrappingKey derives a 256-bit AES wrapping key from a BIP39
// recovery phrase and a per-user salt using Argon2id. This key is used to
// wrap (encrypt) the master key for the recovery path.
//
// The phrase provides 128 bits of entropy; Argon2id makes brute-force
// infeasible even if the salt + wrapped blob leak.
func DeriveRecoveryWrappingKey(phrase string, salt []byte) ([MasterKeySize]byte, error) {
	var wk [MasterKeySize]byte
	if len(salt) < RecoverySaltSize {
		return wk, errors.New("keychain: recovery salt too short")
	}

	// Normalize phrase: lowercase, single spaces.
	normalized := normalizePhrase(phrase)

	raw := argon2.IDKey([]byte(normalized), salt, argon2Time, argon2Memory, argon2Threads, MasterKeySize)
	copy(wk[:], raw)
	wipeBytes(raw)
	return wk, nil
}

// WrapMasterKeyWithPhrase wraps a master key using a key derived from the
// recovery phrase (via Argon2id). Returns the ciphertext blob (nonce || ct).
func WrapMasterKeyWithPhrase(masterKey [MasterKeySize]byte, phrase string, recoverySalt []byte) ([]byte, error) {
	wk, err := DeriveRecoveryWrappingKey(phrase, recoverySalt)
	if err != nil {
		return nil, err
	}
	defer wipeBytes(wk[:])

	wrapped, err := WrapDEK(wk, masterKey)
	if err != nil {
		return nil, fmt.Errorf("keychain: wrap master key with phrase: %w", err)
	}
	return wrapped, nil
}

// UnwrapMasterKeyWithPhrase unwraps a master key using a key derived from the
// recovery phrase (via Argon2id). Returns the plaintext master key.
func UnwrapMasterKeyWithPhrase(wrappedBlob []byte, phrase string, recoverySalt []byte) ([MasterKeySize]byte, error) {
	wk, err := DeriveRecoveryWrappingKey(phrase, recoverySalt)
	if err != nil {
		var zero [MasterKeySize]byte
		return zero, err
	}
	defer wipeBytes(wk[:])

	masterKey, err := UnwrapDEK(wk, wrappedBlob)
	if err != nil {
		var zero [MasterKeySize]byte
		return zero, fmt.Errorf("keychain: unwrap master key with phrase: %w", err)
	}
	return masterKey, nil
}

// normalizePhrase lowercases and collapses whitespace in a phrase.
func normalizePhrase(phrase string) string {
	// Inline to avoid import cycle with recovery.go — simple normalization.
	words := make([]byte, 0, len(phrase))
	inSpace := true
	for i := 0; i < len(phrase); i++ {
		c := phrase[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if !inSpace && len(words) > 0 {
				words = append(words, ' ')
			}
			inSpace = true
		} else {
			if c >= 'A' && c <= 'Z' {
				c += 32 // toLower
			}
			words = append(words, c)
			inSpace = false
		}
	}
	// Trim trailing space.
	if len(words) > 0 && words[len(words)-1] == ' ' {
		words = words[:len(words)-1]
	}
	return string(words)
}
