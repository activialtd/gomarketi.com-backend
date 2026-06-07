// Package jwt provides RS256 JWT issuance and validation for GoMarket services.
//
// RS256 (asymmetric RSA) is required — not HS256 — so that Envoy can validate
// access tokens using only the public key, while the private key never leaves
// the auth service process.
//
// Key management:
//   - Generate a key pair once with: make gen-keys
//   - Pass the file paths via JWT_PRIVATE_KEY_PATH and JWT_PUBLIC_KEY_PATH env vars
//   - Load at startup with LoadPrivateKey / LoadPublicKey
//   - Never log, store, or transmit the private key
package jwt

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims is the GoMarket JWT payload. It extends RegisteredClaims with the
// role and onboarding context that Envoy and downstream services depend on.
//
// Envoy reads these claims from the validated token and injects them as HTTP
// headers (X-User-ID, X-Is-Vendor, X-Store-IDs) for downstream services.
type Claims struct {
	gojwt.RegisteredClaims

	// IsBuyer is true when the user has a buyer_profile row.
	IsBuyer bool `json:"is_buyer"`

	// IsVendor is true when the user has a vendor_profile row.
	IsVendor bool `json:"is_vendor"`

	// BuyerSetup is true when the buyer's profile is complete
	// (full_name set, terms_accepted_at set).
	BuyerSetup bool `json:"buyer_setup"`

	// VendorStep is the current onboarding_step value from vendor_profiles,
	// or "completed". Empty when the user is not a vendor.
	VendorStep string `json:"vendor_step,omitempty"`

	// StoreIDs holds UUIDs of all stores owned by this vendor.
	StoreIDs []string `json:"store_ids,omitempty"`

	// DeviceID identifies the client device for this session. Carried in the
	// refresh_tokens row so device-specific revocation is possible.
	DeviceID string `json:"device_id,omitempty"`
}

// Manager issues and validates JWTs using RS256.
type Manager struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	accessTTL  time.Duration
}

// Config holds the key material and token lifetime for a Manager.
type Config struct {
	PrivateKey     *rsa.PrivateKey
	PublicKey      *rsa.PublicKey
	AccessTokenTTL time.Duration
}

// NewManager creates a Manager. Both keys must be non-nil.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.PrivateKey == nil {
		return nil, errors.New("jwt: PrivateKey is required")
	}
	if cfg.PublicKey == nil {
		return nil, errors.New("jwt: PublicKey is required")
	}
	if cfg.AccessTokenTTL <= 0 {
		return nil, errors.New("jwt: AccessTokenTTL must be positive")
	}
	return &Manager{
		privateKey: cfg.PrivateKey,
		publicKey:  cfg.PublicKey,
		accessTTL:  cfg.AccessTokenTTL,
	}, nil
}

// IssueAccessToken signs a new access JWT for the given user ID and role
// context. The token is RS256-signed and expires after the configured TTL.
// A unique jti is generated per token to support revocation checks.
func (m *Manager) IssueAccessToken(userID string, claims Claims) (string, error) {
	now := time.Now()
	claims.RegisteredClaims = gojwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  gojwt.NewNumericDate(now),
		ExpiresAt: gojwt.NewNumericDate(now.Add(m.accessTTL)),
		ID:        uuid.NewString(), // jti — unique per token
	}

	token := gojwt.NewWithClaims(gojwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(m.privateKey)
	if err != nil {
		return "", fmt.Errorf("signing access token: %w", err)
	}
	return signed, nil
}

// ValidateClaims parses and validates an RS256 JWT. Returns the embedded
// Claims on success. Returns an error if the token is expired, has an invalid
// signature, or uses an unexpected signing algorithm.
func (m *Manager) ValidateClaims(tokenStr string) (*Claims, error) {
	var claims Claims
	_, err := gojwt.ParseWithClaims(tokenStr, &claims, func(t *gojwt.Token) (any, error) {
		if _, ok := t.Method.(*gojwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.publicKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("validating token: %w", err)
	}
	return &claims, nil
}

// ── Key loading ───────────────────────────────────────────────────────────────

// LoadPrivateKey reads and parses a PEM-encoded RSA private key.
// Accepts both PKCS#1 ("BEGIN RSA PRIVATE KEY") and PKCS#8 ("BEGIN PRIVATE KEY")
// formats — OpenSSL 3.x generates PKCS#8 by default.
func LoadPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading private key %q: %w", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in private key file %q", path)
	}

	// Try PKCS#1 first (legacy "BEGIN RSA PRIVATE KEY").
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	// Fall back to PKCS#8 ("BEGIN PRIVATE KEY" — OpenSSL 3.x default).
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key (tried PKCS#1 and PKCS#8): %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not an RSA key")
	}
	return key, nil
}

// LoadPublicKey reads and parses a PEM-encoded PKIX RSA public key.
// Pass the value of JWT_PUBLIC_KEY_PATH env var as path.
func LoadPublicKey(path string) (*rsa.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading public key %q: %w", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in public key file %q", path)
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing public key: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("key is not an RSA public key")
	}
	return rsaPub, nil
}
