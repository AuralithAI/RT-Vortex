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

func TestRecoveryPhraseToMasterKey_Deterministic(t *testing.T) {
	phrase, _ := GenerateRecoveryPhrase()

	k1, err := RecoveryPhraseToMasterKey(phrase)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := RecoveryPhraseToMasterKey(phrase)
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k2 {
		t.Error("same phrase must produce same master key")
	}
}

func TestRecoveryPhraseToMasterKey_DifferentPhrases(t *testing.T) {
	p1, _ := GenerateRecoveryPhrase()
	p2, _ := GenerateRecoveryPhrase()

	k1, _ := RecoveryPhraseToMasterKey(p1)
	k2, _ := RecoveryPhraseToMasterKey(p2)
	if k1 == k2 {
		t.Error("different phrases must produce different keys")
	}
}

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
