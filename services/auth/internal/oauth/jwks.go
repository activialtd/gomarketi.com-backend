// Package oauth provides JWT identity-token verification for Google and Apple OAuth.
// All verification uses stdlib (net/http, crypto/rsa, math/big) — no OAuth SDK.
package oauth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// jwk is a single JSON Web Key entry from a JWKS endpoint.
type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"` // base64url-encoded modulus
	E   string `json:"e"` // base64url-encoded exponent
}

type jwksResponse struct {
	Keys []jwk `json:"keys"`
}

// jwksCache fetches and caches RSA public keys from a JWKS endpoint.
// A single cache instance is shared per provider (Google / Apple).
type jwksCache struct {
	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey // keyed by kid
	fetchedAt time.Time
	ttl       time.Duration
	url       string
	client    *http.Client
}

func newJWKSCache(url string, ttl time.Duration) *jwksCache {
	return &jwksCache{
		url:  url,
		ttl:  ttl,
		keys: make(map[string]*rsa.PublicKey),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetKey returns the RSA public key for the given kid.
// It refreshes the cache if stale or if the kid is unknown.
func (c *jwksCache) GetKey(kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	key, ok := c.keys[kid]
	expired := time.Since(c.fetchedAt) > c.ttl
	c.mu.RUnlock()

	if ok && !expired {
		return key, nil
	}

	// Refresh under write lock.
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check: another goroutine may have refreshed while we waited.
	if key, ok = c.keys[kid]; ok && time.Since(c.fetchedAt) <= c.ttl {
		return key, nil
	}

	if err := c.refresh(); err != nil {
		return nil, fmt.Errorf("refreshing JWKS from %s: %w", c.url, err)
	}

	key, ok = c.keys[kid]
	if !ok {
		return nil, fmt.Errorf("kid %q not found in JWKS", kid)
	}
	return key, nil
}

func (c *jwksCache) refresh() error {
	resp, err := c.client.Get(c.url)
	if err != nil {
		return fmt.Errorf("fetching JWKS: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("reading JWKS response: %w", err)
	}

	var result jwksResponse
	if err = json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parsing JWKS JSON: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(result.Keys))
	for _, k := range result.Keys {
		if k.Kty != "RSA" || k.Use != "sig" {
			continue
		}
		pub, err := jwkToRSA(k.N, k.E)
		if err != nil {
			continue // skip malformed keys, don't fail the whole refresh
		}
		keys[k.Kid] = pub
	}

	c.keys = keys
	c.fetchedAt = time.Now()
	return nil
}

// jwkToRSA converts base64url-encoded modulus and exponent to *rsa.PublicKey.
func jwkToRSA(n, e string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(n)
	if err != nil {
		return nil, fmt.Errorf("decoding modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(e)
	if err != nil {
		return nil, fmt.Errorf("decoding exponent: %w", err)
	}

	nBig := new(big.Int).SetBytes(nBytes)
	eBig := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: nBig,
		E: int(eBig.Int64()),
	}, nil
}
