package keychain

import (
	"bytes"
	"testing"
)

func TestGenerateMasterKey(t *testing.T) {
	k1, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	k2, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k2 {
		t.Fatal("two generated master keys must differ")
	}
}

func TestDeriveKeys(t *testing.T) {
	master, _ := GenerateMasterKey()
	salt, _ := GenerateSalt()

	dk1, err := DeriveKeys(master, salt)
	if err != nil {
		t.Fatal(err)
	}
	defer dk1.Wipe()

	dk2, err := DeriveKeys(master, salt)
	if err != nil {
		t.Fatal(err)
	}
	defer dk2.Wipe()

	if dk1.EncryptionKey != dk2.EncryptionKey {
		t.Error("same master+salt must produce same encryption key")
	}
	if dk1.AuthKey != dk2.AuthKey {
		t.Error("same master+salt must produce same auth key")
	}
	if dk1.EncryptionKey == dk1.AuthKey {
		t.Error("encryption and auth keys must differ (domain separation)")
	}
}

func TestDeriveKeys_DifferentSalt(t *testing.T) {
	master, _ := GenerateMasterKey()
	salt1, _ := GenerateSalt()
	salt2, _ := GenerateSalt()

	dk1, _ := DeriveKeys(master, salt1)
	defer dk1.Wipe()
	dk2, _ := DeriveKeys(master, salt2)
	defer dk2.Wipe()

	if dk1.EncryptionKey == dk2.EncryptionKey {
		t.Error("different salts must produce different keys")
	}
}

func TestWrapUnwrapDEK(t *testing.T) {
	master, _ := GenerateMasterKey()
	salt, _ := GenerateSalt()
	dk, _ := DeriveKeys(master, salt)
	defer dk.Wipe()

	dek, err := GenerateDEK()
	if err != nil {
		t.Fatal(err)
	}

	wrapped, err := WrapDEK(dk.EncryptionKey, dek)
	if err != nil {
		t.Fatal(err)
	}
	if len(wrapped) <= DEKSize {
		t.Fatal("wrapped DEK should be longer than raw DEK")
	}

	unwrapped, err := UnwrapDEK(dk.EncryptionKey, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if unwrapped != dek {
		t.Error("unwrapped DEK must match original")
	}
}

func TestWrapDEK_WrongKey(t *testing.T) {
	master1, _ := GenerateMasterKey()
	master2, _ := GenerateMasterKey()
	salt, _ := GenerateSalt()

	dk1, _ := DeriveKeys(master1, salt)
	defer dk1.Wipe()
	dk2, _ := DeriveKeys(master2, salt)
	defer dk2.Wipe()

	dek, _ := GenerateDEK()
	wrapped, _ := WrapDEK(dk1.EncryptionKey, dek)

	_, err := UnwrapDEK(dk2.EncryptionKey, wrapped)
	if err == nil {
		t.Fatal("unwrapping with wrong key should fail")
	}
}

func TestEncryptDecryptSecret(t *testing.T) {
	dek, _ := GenerateDEK()
	plaintext := []byte("sk-live-12345678901234567890")

	ciphertext, err := EncryptSecret(dek, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext must differ from plaintext")
	}

	decrypted, err := DecryptSecret(dek, ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted=%q want=%q", decrypted, plaintext)
	}
}

func TestEncryptDecrypt_WrongDEK(t *testing.T) {
	dek1, _ := GenerateDEK()
	dek2, _ := GenerateDEK()

	ciphertext, _ := EncryptSecret(dek1, []byte("secret"))

	_, err := DecryptSecret(dek2, ciphertext)
	if err == nil {
		t.Fatal("decryption with wrong DEK should fail")
	}
}

func TestAuthProof(t *testing.T) {
	master, _ := GenerateMasterKey()
	salt, _ := GenerateSalt()
	dk, _ := DeriveKeys(master, salt)
	defer dk.Wipe()

	challenge, _ := GenerateChallenge()
	proof := ComputeAuthProof(dk.AuthKey, challenge)

	if !VerifyAuthProof(dk.AuthKey, challenge, proof) {
		t.Error("valid proof should verify")
	}

	otherChallenge, _ := GenerateChallenge()
	if VerifyAuthProof(dk.AuthKey, otherChallenge, proof) {
		t.Error("proof for different challenge should not verify")
	}
}

func TestAuthKeyVerifier(t *testing.T) {
	master, _ := GenerateMasterKey()
	salt, _ := GenerateSalt()
	dk, _ := DeriveKeys(master, salt)
	defer dk.Wipe()

	v1 := ComputeAuthKeyVerifier(dk.AuthKey)
	v2 := ComputeAuthKeyVerifier(dk.AuthKey)
	if v1 != v2 {
		t.Error("same auth key must produce same verifier")
	}

	dk2, _ := DeriveKeys(master, salt)
	defer dk2.Wipe()
	v3 := ComputeAuthKeyVerifier(dk2.AuthKey)
	if v1 != v3 {
		t.Error("same keys must produce same verifier")
	}
}

func TestMasterKeyHexRoundtrip(t *testing.T) {
	key, _ := GenerateMasterKey()
	hexStr := MasterKeyToHex(key)

	if len(hexStr) != 64 {
		t.Errorf("hex key length=%d want=64", len(hexStr))
	}

	decoded, err := MasterKeyFromHex(hexStr)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != key {
		t.Error("roundtrip should preserve key")
	}
}

func TestMasterKeyFromHex_Invalid(t *testing.T) {
	_, err := MasterKeyFromHex("tooshort")
	if err == nil {
		t.Error("short hex should fail")
	}

	_, err = MasterKeyFromHex("zzzz" + MasterKeyToHex([MasterKeySize]byte{}))
	if err == nil {
		t.Error("invalid hex chars should fail")
	}
}

func TestGenerateSalt(t *testing.T) {
	s1, err := GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}
	if len(s1) != SaltSize {
		t.Errorf("salt size=%d want=%d", len(s1), SaltSize)
	}

	s2, _ := GenerateSalt()
	if bytes.Equal(s1, s2) {
		t.Error("two random salts should differ")
	}
}

func TestDeriveKeys_ShortSalt(t *testing.T) {
	master, _ := GenerateMasterKey()
	_, err := DeriveKeys(master, []byte("short"))
	if err == nil {
		t.Error("short salt should be rejected")
	}
}

func TestWipeKeys(t *testing.T) {
	master, _ := GenerateMasterKey()
	salt, _ := GenerateSalt()
	dk, _ := DeriveKeys(master, salt)

	dk.Wipe()

	var zero [EncryptionKeySize]byte
	if dk.EncryptionKey != zero {
		t.Error("encryption key should be zeroed after wipe")
	}
	if dk.AuthKey != zero {
		t.Error("auth key should be zeroed after wipe")
	}
}

func TestEndToEnd_FullCryptoRoundtrip(t *testing.T) {
	// Simulate full flow: generate master key, derive sub-keys, encrypt a
	// secret with a DEK, wrap the DEK, then unwrap and decrypt.
	master, _ := GenerateMasterKey()
	salt, _ := GenerateSalt()
	dk, _ := DeriveKeys(master, salt)
	defer dk.Wipe()

	secret := []byte("ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789")

	// Encrypt secret with random DEK.
	dek, _ := GenerateDEK()
	ciphertext, _ := EncryptSecret(dek, secret)

	// Wrap DEK with user's encryption key.
	wrappedDEK, _ := WrapDEK(dk.EncryptionKey, dek)

	// Simulate retrieval: unwrap DEK, decrypt secret.
	recoveredDEK, err := UnwrapDEK(dk.EncryptionKey, wrappedDEK)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := DecryptSecret(recoveredDEK, ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plaintext, secret) {
		t.Errorf("full roundtrip failed: got=%q want=%q", plaintext, secret)
	}
}
