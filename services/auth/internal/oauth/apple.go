package oauth

import (
	"fmt"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

const (
	appleJWKSURL = "https://appleid.apple.com/auth/keys"
	appleIssuer  = "https://appleid.apple.com"
)

// AppleClaims holds the decoded fields from a verified Apple identity_token.
type AppleClaims struct {
	Sub   string  // Apple user ID — used as provider_uid; stable per user per app
	Email string  // may be a private relay address (xxx@privaterelay.appleid.com)
}

// AppleVerifier verifies Apple identity_tokens using Apple's public JWKS.
type AppleVerifier struct {
	cache    *jwksCache
	bundleID string // APPLE_BUNDLE_ID — the JWT aud claim must equal this
}

// NewAppleVerifier creates an AppleVerifier. bundleID is the value of
// APPLE_BUNDLE_ID env var (e.g. "com.activia.gomarket").
func NewAppleVerifier(bundleID string) *AppleVerifier {
	return &AppleVerifier{
		cache:    newJWKSCache(appleJWKSURL, 60*time.Minute),
		bundleID: bundleID,
	}
}

// Verify validates an Apple identity_token and returns the decoded claims.
// Returns an error if the token is expired, has an invalid signature,
// was not issued by Apple, or has the wrong audience.
//
// Note on email: Apple may provide a private relay email address
// (xxx@privaterelay.appleid.com). This is normal — store it as-is.
// On return visits, Apple does NOT re-send the user's name or email in
// the request body — those fields come only on the very first sign-in.
func (v *AppleVerifier) Verify(identityToken string) (*AppleClaims, error) {
	var rawClaims struct {
		gojwt.RegisteredClaims
		Email string `json:"email"`
	}

	_, err := gojwt.ParseWithClaims(identityToken, &rawClaims, func(t *gojwt.Token) (any, error) {
		if _, ok := t.Method.(*gojwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		return v.cache.GetKey(kid)
	})
	if err != nil {
		return nil, fmt.Errorf("verifying apple identity_token: %w", err)
	}

	if rawClaims.Issuer != appleIssuer {
		return nil, fmt.Errorf("invalid issuer: %q", rawClaims.Issuer)
	}

	found := false
	for _, aud := range rawClaims.Audience {
		if aud == v.bundleID {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("token audience does not include bundle ID")
	}

	return &AppleClaims{
		Sub:   rawClaims.Subject,
		Email: rawClaims.Email,
	}, nil
}
