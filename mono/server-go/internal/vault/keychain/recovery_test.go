package keychain

import (
	"strings"
	"testing"
)

func TestGenerateRecoveryPhrase(t *testing.T) {
	phrase, err := GenerateRecoveryPhrase()
	if err != nil {
		t.Fatal(err)
	}

	words := strings.Fields(phrase)
	if len(words) != 12 {
		t.Errorf("phrase word count=%d want=12", len(words))
	}

	// Every word must be in the BIP39 word list.
	for _, w := range words {
		if _, ok := bip39WordIndex[w]; !ok {
			t.Errorf("word %q not in BIP39 word list", w)
		}
	}
}

func TestGenerateRecoveryPhrase_Unique(t *testing.T) {
	p1, _ := GenerateRecoveryPhrase()
	p2, _ := GenerateRecoveryPhrase()
	if p1 == p2 {
		t.Error("two phrases should differ (128-bit entropy)")
	}
}

func TestValidateRecoveryPhrase(t *testing.T) {
	phrase, _ := GenerateRecoveryPhrase()
	if err := ValidateRecoveryPhrase(phrase); err != nil {
		t.Errorf("valid phrase rejected: %v", err)
	}
}

func TestValidateRecoveryPhrase_Invalid(t *testing.T) {
	tests := []struct {
		name   string
		phrase string
	}{
		{"empty", ""},
		{"too few words", "abandon abandon abandon"},
		{"too many words", "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon"},
		{"invalid word", "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon notaword"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateRecoveryPhrase(tt.phrase); err == nil {
				t.Error("expected error for invalid phrase")
			}
		})
	}
}

// ── Recovery Wrapping Key (Argon2id) Tests ──────────────────────────────────

func TestDeriveRecoveryWrappingKey_Deterministic(t *testing.T) {
	phrase, _ := GenerateRecoveryPhrase()
	salt, err := GenerateRecoverySalt()
	if err != nil {
		t.Fatal(err)
	}

	k1, err := DeriveRecoveryWrappingKey(phrase, salt)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := DeriveRecoveryWrappingKey(phrase, salt)
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k2 {
		t.Error("same phrase + salt must produce same wrapping key")
	}
}

func TestDeriveRecoveryWrappingKey_DifferentPhrases(t *testing.T) {
	salt, _ := GenerateRecoverySalt()
	p1, _ := GenerateRecoveryPhrase()
	p2, _ := GenerateRecoveryPhrase()

	k1, _ := DeriveRecoveryWrappingKey(p1, salt)
	k2, _ := DeriveRecoveryWrappingKey(p2, salt)
	if k1 == k2 {
		t.Error("different phrases must produce different wrapping keys")
	}
}

func TestDeriveRecoveryWrappingKey_DifferentSalts(t *testing.T) {
	phrase, _ := GenerateRecoveryPhrase()
	salt1, _ := GenerateRecoverySalt()
	salt2, _ := GenerateRecoverySalt()

	k1, _ := DeriveRecoveryWrappingKey(phrase, salt1)
	k2, _ := DeriveRecoveryWrappingKey(phrase, salt2)
	if k1 == k2 {
		t.Error("different salts must produce different wrapping keys")
	}
}

func TestDeriveRecoveryWrappingKey_CaseInsensitive(t *testing.T) {
	phrase, _ := GenerateRecoveryPhrase()
	salt, _ := GenerateRecoverySalt()

	k1, _ := DeriveRecoveryWrappingKey(phrase, salt)
	k2, _ := DeriveRecoveryWrappingKey(strings.ToUpper(phrase), salt)
	if k1 != k2 {
		t.Error("phrase derivation should be case-insensitive")
	}
}

// ── Master Key Wrap / Unwrap (dual-wrap recovery path) ──────────────────────

func TestWrapUnwrapMasterKeyWithPhrase(t *testing.T) {
	masterKey, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	phrase, _ := GenerateRecoveryPhrase()
	salt, _ := GenerateRecoverySalt()

	wrapped, err := WrapMasterKeyWithPhrase(masterKey, phrase, salt)
	if err != nil {
		t.Fatalf("wrap failed: %v", err)
	}

	recovered, err := UnwrapMasterKeyWithPhrase(wrapped, phrase, salt)
	if err != nil {
		t.Fatalf("unwrap failed: %v", err)
	}

	if recovered != masterKey {
		t.Error("unwrapped master key does not match original")
	}
}

func TestUnwrapMasterKeyWithPhrase_WrongPhrase(t *testing.T) {
	masterKey, _ := GenerateMasterKey()
	phrase1, _ := GenerateRecoveryPhrase()
	phrase2, _ := GenerateRecoveryPhrase()
	salt, _ := GenerateRecoverySalt()

	wrapped, _ := WrapMasterKeyWithPhrase(masterKey, phrase1, salt)

	_, err := UnwrapMasterKeyWithPhrase(wrapped, phrase2, salt)
	if err == nil {
		t.Error("unwrap with wrong phrase should fail")
	}
}

func TestUnwrapMasterKeyWithPhrase_WrongSalt(t *testing.T) {
	masterKey, _ := GenerateMasterKey()
	phrase, _ := GenerateRecoveryPhrase()
	salt1, _ := GenerateRecoverySalt()
	salt2, _ := GenerateRecoverySalt()

	wrapped, _ := WrapMasterKeyWithPhrase(masterKey, phrase, salt1)

	_, err := UnwrapMasterKeyWithPhrase(wrapped, phrase, salt2)
	if err == nil {
		t.Error("unwrap with wrong salt should fail")
	}
}

func TestWrapMasterKeyWithPhrase_DifferentWraps(t *testing.T) {
	masterKey, _ := GenerateMasterKey()
	phrase, _ := GenerateRecoveryPhrase()
	salt, _ := GenerateRecoverySalt()

	w1, _ := WrapMasterKeyWithPhrase(masterKey, phrase, salt)
	w2, _ := WrapMasterKeyWithPhrase(masterKey, phrase, salt)

	// Wrapped blobs should differ (random nonce each time).
	if string(w1) == string(w2) {
		t.Error("two wraps of same key should differ (different nonces)")
	}

	// But both should unwrap to the same master key.
	k1, _ := UnwrapMasterKeyWithPhrase(w1, phrase, salt)
	k2, _ := UnwrapMasterKeyWithPhrase(w2, phrase, salt)
	if k1 != k2 {
		t.Error("both wraps should unwrap to the same master key")
	}
}

// ── BIP39 Word List Tests ───────────────────────────────────────────────────

func TestBIP39WordList(t *testing.T) {
	if len(bip39WordList) != 2048 {
		t.Errorf("BIP39 word list has %d words, want 2048", len(bip39WordList))
	}
	if len(bip39WordIndex) != 2048 {
		t.Errorf("BIP39 word index has %d entries, want 2048", len(bip39WordIndex))
	}

	// Spot-check a few known BIP39 words.
	knownWords := []string{"abandon", "ability", "able", "about", "zoo", "zone", "zero"}
	for _, w := range knownWords {
		if _, ok := bip39WordIndex[w]; !ok {
			t.Errorf("expected word %q in BIP39 list", w)
		}
	}
}
