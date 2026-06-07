package crypto_test

import (
	"strings"
	"testing"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/crypto"
)

// fixedKey is a 32-byte AES-256 key used across all tests. In real code the
// key comes from ParseKey(os.Getenv("ENCRYPTION_KEY")).
// Exactly 32 bytes: 26 lowercase letters + "123456".
var fixedKey = []byte("abcdefghijklmnopqrstuvwxyz123456")

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext string
	}{
		{"simple ASCII", "Hello, GoMarket!"},
		{"empty string", ""},
		{"Nigerian unicode", "₦50,000.00 — payment confirmed"},
		{"long string", strings.Repeat("x", 4096)},
		{"BVN-like", "12345678901"},
		{"account number", "0123456789"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext, err := crypto.Encrypt(fixedKey, tc.plaintext)
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}
			if tc.plaintext != "" && ciphertext == tc.plaintext {
				t.Error("Encrypt() returned plaintext unchanged")
			}

			decrypted, err := crypto.Decrypt(fixedKey, ciphertext)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}
			if decrypted != tc.plaintext {
				t.Errorf("Decrypt() = %q, want %q", decrypted, tc.plaintext)
			}
		})
	}
}

func TestEncrypt_RandomNonce(t *testing.T) {
	// The same plaintext encrypted twice must produce different ciphertexts
	// because each call generates a fresh random nonce.
	ct1, err := crypto.Encrypt(fixedKey, "same plaintext")
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := crypto.Encrypt(fixedKey, "same plaintext")
	if err != nil {
		t.Fatal(err)
	}
	if ct1 == ct2 {
		t.Error("two encryptions of the same plaintext produced the same ciphertext — nonce is not random")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	ciphertext, err := crypto.Encrypt(fixedKey, "secret value")
	if err != nil {
		t.Fatal(err)
	}

	wrongKey := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ123456")
	_, err = crypto.Decrypt(wrongKey, ciphertext)
	if err == nil {
		t.Error("Decrypt() with wrong key should fail")
	}
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	ciphertext, err := crypto.Encrypt(fixedKey, "tamper me")
	if err != nil {
		t.Fatal(err)
	}

	// Flip the last byte of the hex string.
	tampered := ciphertext[:len(ciphertext)-1] + "f"
	if tampered == ciphertext {
		tampered = ciphertext[:len(ciphertext)-1] + "0"
	}

	_, err = crypto.Decrypt(fixedKey, tampered)
	if err == nil {
		t.Error("Decrypt() should fail on tampered ciphertext")
	}
}

func TestDecrypt_TruncatedCiphertext(t *testing.T) {
	_, err := crypto.Decrypt(fixedKey, "deadbeef") // only 4 bytes, need at least 12
	if err == nil {
		t.Error("Decrypt() should fail on too-short ciphertext")
	}
}

func TestDecrypt_InvalidHex(t *testing.T) {
	_, err := crypto.Decrypt(fixedKey, "not-valid-hex!!")
	if err == nil {
		t.Error("Decrypt() should fail on invalid hex")
	}
}

func TestParseKey(t *testing.T) {
	t.Run("valid 32-byte key", func(t *testing.T) {
		hexKey := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
		key, err := crypto.ParseKey(hexKey)
		if err != nil {
			t.Fatalf("ParseKey() error = %v", err)
		}
		if len(key) != 32 {
			t.Errorf("key length = %d, want 32", len(key))
		}
	})

	t.Run("too short", func(t *testing.T) {
		_, err := crypto.ParseKey("deadbeef")
		if err == nil {
			t.Error("ParseKey() should fail for a short key")
		}
	})

	t.Run("invalid hex", func(t *testing.T) {
		_, err := crypto.ParseKey("gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg")
		if err == nil {
			t.Error("ParseKey() should fail for non-hex input")
		}
	})
}
