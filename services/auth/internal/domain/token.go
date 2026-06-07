package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

const RefreshTokenExpiry = 30 * 24 * time.Hour

// GenerateRefreshToken produces an opaque, cryptographically random 32-byte
// refresh token. Returns the raw token (sent to the client via HttpOnly cookie)
// and its SHA-256 hex hash (stored in the database — never the raw value).
func GenerateRefreshToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generating refresh token: %w", err)
	}
	raw = hex.EncodeToString(b)
	hash = HashToken(raw)
	return raw, hash, nil
}

// HashToken returns the SHA-256 hex-encoded hash of any raw token string.
// Used for refresh tokens: the database stores only the hash.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// RefreshTokenFamily represents the family-based reuse-detection contract.
// All tokens issued from the same initial login share a FamilyID. If a
// revoked token from a family is presented again, all siblings are revoked.
type RefreshTokenFamily struct {
	FamilyID  string
	UserID    string
	RevokedAt *time.Time
}
