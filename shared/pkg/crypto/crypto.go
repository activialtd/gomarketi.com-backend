// Package crypto provides AES-256-GCM encryption and decryption for PII fields.
//
// Every PII value (BVN, NIN, account numbers, full addresses) must be encrypted
// with Encrypt before any database write, and decrypted with Decrypt only at the
// point of use. Plain-text PII must never be logged or sent to error trackers.
//
// Wire format: hex(nonce ‖ gcm_ciphertext ‖ gcm_tag)
//   nonce:  12 bytes (GCM standard)
//   key:    32 bytes (AES-256)
//   tag:    16 bytes (appended automatically by cipher.AEAD.Seal)
//
// Each call to Encrypt generates a fresh random nonce, so encrypting the same
// plaintext twice produces different ciphertexts — safe for database UNIQUE
// columns only when the application compares decrypted values, not ciphertexts.
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

const nonceSize = 12 // GCM standard nonce length in bytes

// Encrypt encrypts plaintext with AES-256-GCM.
// key must be exactly 32 bytes (use ParseKey to obtain it).
// Returns a hex-encoded string: nonce ‖ ciphertext ‖ GCM tag.
func Encrypt(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher block: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}

	// Seal appends the ciphertext and GCM authentication tag to nonce.
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(sealed), nil
}

// Decrypt decrypts a hex-encoded ciphertext produced by Encrypt.
// Returns the original plaintext string.
// Returns an error if the ciphertext has been tampered with (GCM authentication
// failure), truncated, or produced with a different key.
func Decrypt(key []byte, ciphertext string) (string, error) {
	raw, err := hex.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decoding hex: %w", err)
	}

	if len(raw) < nonceSize {
		return "", errors.New("ciphertext too short: missing nonce")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher block: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}

	nonce, ct := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		// Do not include the original error — it may reveal key or plaintext info.
		return "", errors.New("decryption failed: authentication tag mismatch or wrong key")
	}

	return string(plaintext), nil
}

// ParseKey decodes a hex-encoded 32-byte AES-256 key.
// The value of the ENCRYPTION_KEY environment variable should be passed here.
// Returns an error if the input is not exactly 64 hex characters (32 bytes).
func ParseKey(hexKey string) ([]byte, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("decoding encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes (64 hex chars), got %d bytes", len(key))
	}
	return key, nil
}
