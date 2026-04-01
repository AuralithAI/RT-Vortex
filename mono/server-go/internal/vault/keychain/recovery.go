package keychain

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// BIP39 word list (2048 words). A 12-word phrase encodes 128 bits of entropy.
// This is the English BIP39 word list as specified in BIP-0039.
//
// Source: https://github.com/bitcoin/bips/blob/master/bip-0039/english.txt
//
// We embed the list at compile time. The full 2048 words are in bip39_wordlist.go.

// GenerateRecoveryPhrase creates a new 12-word BIP39 recovery phrase from
// 128 bits of cryptographic randomness.
func GenerateRecoveryPhrase() (string, error) {
	words, err := entropyToMnemonic(128)
	if err != nil {
		return "", fmt.Errorf("keychain: generate recovery phrase: %w", err)
	}
	return strings.Join(words, " "), nil
}

// ValidateRecoveryPhrase checks that a phrase is exactly 12 valid BIP39 words.
func ValidateRecoveryPhrase(phrase string) error {
	words := strings.Fields(strings.TrimSpace(phrase))
	if len(words) != 12 {
		return fmt.Errorf("keychain: expected 12 words, got %d", len(words))
	}
	for i, w := range words {
		if _, ok := bip39WordIndex[strings.ToLower(w)]; !ok {
			return fmt.Errorf("keychain: word %d (%q) is not a valid BIP39 word", i+1, w)
		}
	}
	return nil
}

// RecoveryPhraseToMasterKey derives a 256-bit master key from a 12-word
// recovery phrase using HKDF-SHA256. The phrase provides 128 bits of entropy
// which is expanded to 256 bits via HKDF with a fixed domain-separation salt.
func RecoveryPhraseToMasterKey(phrase string) ([MasterKeySize]byte, error) {
	var key [MasterKeySize]byte

	if err := ValidateRecoveryPhrase(phrase); err != nil {
		return key, err
	}

	// Normalize: lowercase, single spaces.
	normalized := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(phrase))), " ")

	// Use HKDF to expand 128 bits of phrase entropy into 256-bit master key.
	// The salt is a fixed domain-separation string (not secret).
	phraseSalt := []byte("rtvortex-keychain-recovery-phrase-v1")
	phraseInfo := []byte("master-key-from-recovery-phrase")
	if err := hkdfExpand([]byte(normalized), phraseSalt, phraseInfo, key[:]); err != nil {
		return key, fmt.Errorf("keychain: derive master key from phrase: %w", err)
	}
	return key, nil
}

// entropyToMnemonic generates a BIP39-style mnemonic from random entropy.
func entropyToMnemonic(bits int) ([]string, error) {
	if bits != 128 {
		return nil, errors.New("keychain: only 128-bit entropy (12 words) is supported")
	}

	entropyBytes := bits / 8 // 16 bytes
	entropy := make([]byte, entropyBytes)
	if _, err := rand.Read(entropy); err != nil {
		return nil, fmt.Errorf("keychain: random entropy: %w", err)
	}

	// BIP39: 128 bits entropy + 4-bit checksum = 132 bits = 12 × 11-bit words.
	// For simplicity and security, we use uniform random selection from the
	// 2048-word list rather than implementing the full BIP39 checksum scheme.
	// This gives the same 128 bits of entropy (12 words × log2(2048) = 132 bits,
	// minus ~4 bits for the checksum in true BIP39).
	wordCount := 12
	words := make([]string, wordCount)
	for i := 0; i < wordCount; i++ {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(bip39WordList))))
		if err != nil {
			return nil, fmt.Errorf("keychain: random word selection: %w", err)
		}
		words[i] = bip39WordList[idx.Int64()]
	}

	return words, nil
}
