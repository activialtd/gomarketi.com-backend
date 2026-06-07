package oauth

import (
	"fmt"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

const (
	googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"
	googleIssuer  = "https://accounts.google.com"
)

// GoogleClaims holds the decoded fields from a verified Google id_token.
type GoogleClaims struct {
	Sub           string `json:"sub"`            // Google user ID — used as provider_uid
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// GoogleVerifier verifies Google id_tokens using Google's public JWKS.
type GoogleVerifier struct {
	cache    *jwksCache
	clientID string // GOOGLE_CLIENT_ID — the JWT aud claim must equal this
}

// NewGoogleVerifier creates a GoogleVerifier. clientID is the value of
// GOOGLE_CLIENT_ID env var.
func NewGoogleVerifier(clientID string) *GoogleVerifier {
	return &GoogleVerifier{
		cache:    newJWKSCache(googleJWKSURL, 60*time.Minute),
		clientID: clientID,
	}
}

// Verify validates a Google id_token and returns the decoded claims.
// Returns an error if the token is expired, has an invalid signature,
// was not issued by Google, or has the wrong audience.
// Rejects tokens where email_verified is false.
func (v *GoogleVerifier) Verify(idToken string) (*GoogleClaims, error) {
	var rawClaims struct {
		gojwt.RegisteredClaims
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}

	_, err := gojwt.ParseWithClaims(idToken, &rawClaims, func(t *gojwt.Token) (any, error) {
		if _, ok := t.Method.(*gojwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		return v.cache.GetKey(kid)
	})
	if err != nil {
		return nil, fmt.Errorf("verifying google id_token: %w", err)
	}

	// Validate issuer.
	if rawClaims.Issuer != googleIssuer && rawClaims.Issuer != "accounts.google.com" {
		return nil, fmt.Errorf("invalid issuer: %q", rawClaims.Issuer)
	}

	// Validate audience — must be our client ID.
	found := false
	for _, aud := range rawClaims.Audience {
		if aud == v.clientID {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("token audience does not include client ID")
	}

	if !rawClaims.EmailVerified {
		return nil, fmt.Errorf("google account email is not verified")
	}

	return &GoogleClaims{
		Sub:           rawClaims.Subject,
		Email:         rawClaims.Email,
		EmailVerified: rawClaims.EmailVerified,
		Name:          rawClaims.Name,
		Picture:       rawClaims.Picture,
	}, nil
}
