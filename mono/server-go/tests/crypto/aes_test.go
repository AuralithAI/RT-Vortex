package crypto_test

import (
	"strings"
	"testing"

	rtcrypto "github.com/AuralithAI/rtvortex-server/internal/crypto"
)

// ── Encrypt / Decrypt round-trip ────────────────────────────────────────────

func TestTokenEncryptor_RoundTrip(t *testing.T) {
	// 32-byte key = 64 hex chars
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	enc, err := rtcrypto.NewTokenEncryptor(key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	plaintext := "my-secret-oauth-token-12345"
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if ciphertext == plaintext {
		t.Error("ciphertext should differ from plaintext")
	}

	decrypted, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("expected %q, got %q", plaintext, decrypted)
	}
}

// ── Empty plaintext ─────────────────────────────────────────────────────────

func TestTokenEncryptor_EmptyPlaintext(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	enc, err := rtcrypto.NewTokenEncryptor(key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ciphertext, err := enc.Encrypt("")
	if err != nil {
		t.Fatalf("encrypt empty failed: %v", err)
	}

	decrypted, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt empty failed: %v", err)
	}
	if decrypted != "" {
		t.Errorf("expected empty string, got %q", decrypted)
	}
}

// ── Different ciphertexts for same plaintext (random nonce) ─────────────────

func TestTokenEncryptor_NonDeterministic(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	enc, err := rtcrypto.NewTokenEncryptor(key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c1, _ := enc.Encrypt("same-value")
	c2, _ := enc.Encrypt("same-value")

	if c1 == c2 {
		t.Error("expected different ciphertexts due to random nonce")
	}
}

// ── Decrypt with wrong key ──────────────────────────────────────────────────

func TestTokenEncryptor_DecryptWrongKey(t *testing.T) {
	key1 := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	key2 := "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

	enc1, _ := rtcrypto.NewTokenEncryptor(key1)
	enc2, _ := rtcrypto.NewTokenEncryptor(key2)

	ciphertext, _ := enc1.Encrypt("secret-data")

	_, err := enc2.Decrypt(ciphertext)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

// ── Decrypt corrupt ciphertext ──────────────────────────────────────────────

func TestTokenEncryptor_DecryptCorrupt(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	enc, _ := rtcrypto.NewTokenEncryptor(key)

	_, err := enc.Decrypt("not-a-valid-hex-ciphertext")
	if err == nil {
		t.Fatal("expected error for corrupt ciphertext")
	}
}

func TestTokenEncryptor_DecryptTooShort(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	enc, _ := rtcrypto.NewTokenEncryptor(key)

	_, err := enc.Decrypt("abcd")
	if err == nil {
		t.Fatal("expected error for too-short ciphertext")
	}
}

// ── No-op mode (empty key) ──────────────────────────────────────────────────

func TestTokenEncryptor_NoopMode(t *testing.T) {
	enc, err := rtcrypto.NewTokenEncryptor("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc.IsEnabled() {
		t.Error("expected disabled encryptor for empty key")
	}

	ct, err := enc.Encrypt("pass-through")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if ct != "pass-through" {
		t.Errorf("expected pass-through, got %q", ct)
	}

	pt, err := enc.Decrypt("pass-through")
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if pt != "pass-through" {
		t.Errorf("expected pass-through, got %q", pt)
	}
}

// ── IsEnabled ───────────────────────────────────────────────────────────────

func TestTokenEncryptor_IsEnabled(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	enc, _ := rtcrypto.NewTokenEncryptor(key)
	if !enc.IsEnabled() {
		t.Error("expected enabled encryptor for valid key")
	}
}

// ── Long plaintext ──────────────────────────────────────────────────────────

func TestTokenEncryptor_LongPlaintext(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	enc, _ := rtcrypto.NewTokenEncryptor(key)

	long := strings.Repeat("a", 10000)
	ciphertext, err := enc.Encrypt(long)
	if err != nil {
		t.Fatalf("encrypt long failed: %v", err)
	}

	decrypted, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt long failed: %v", err)
	}
	if decrypted != long {
		t.Error("round-trip failed for long plaintext")
	}
}
