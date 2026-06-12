package main

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"
)

func main() {
	log := zerolog.New(os.Stdout).With().Timestamp().Logger()
	if err := run(log); err != nil {
		log.Fatal().Err(err).Msg("gateway startup failed")
	}
}

func run(log zerolog.Logger) error {
	upstreams, err := loadUpstreams()
	if err != nil {
		return err
	}

	// JWT public key is required. Auth service sets JWT_PUBLIC_KEY as base64-encoded PEM.
	jwtKey, err := loadPublicKey()
	if err != nil {
		return fmt.Errorf("JWT_PUBLIC_KEY: %w", err)
	}

	storefrontURL := os.Getenv("UPSTREAM_STOREFRONT")
	sc := newStoreCache(storefrontURL, log)

	allowedOrigins := strings.Split(getenv("ALLOWED_ORIGINS", ""), ",")

	mux := http.NewServeMux()

	// Health check — load balancers and Railway hit this.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	// Route each path prefix to the correct upstream.
	for prefix, target := range upstreams {
		prefix, target := prefix, target
		proxy := newProxy(target, log)
		needsStore := needsStoreIDs(prefix)

		mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
			setCORS(w, r, allowedOrigins)
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Auth routes are pass-through — the auth service manages its own auth.
			if strings.HasPrefix(r.URL.Path, "/v1/auth/") {
				proxy.ServeHTTP(w, r)
				return
			}

			// All other routes require a valid Bearer token.
			userID, storeIDs, ok := validateBearer(r, jwtKey)
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, `{"error":"authentication required"}`)
				return
			}

			r.Header.Set("X-User-ID", userID)

			if needsStore {
				ids := storeIDs
				if len(ids) == 0 {
					// JWT has no store_ids yet — look up from storefront service.
					if sid, ok2 := sc.lookup(r.Context(), userID); ok2 {
						ids = []string{sid}
					}
				}
				if len(ids) > 0 {
					r.Header.Set("X-Store-IDs", strings.Join(ids, ","))
				}
			}

			proxy.ServeHTTP(w, r)
		})
	}

	port := getenv("PORT", "8080")
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info().Str("addr", srv.Addr).Msg("gateway listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("server: %w", err)
	case sig := <-quit:
		log.Info().Str("signal", sig.String()).Msg("shutting down")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// ── JWT helpers ───────────────────────────────────────────────────────────────

type gmClaims struct {
	gojwt.RegisteredClaims
	IsBuyer  bool     `json:"is_buyer"`
	IsVendor bool     `json:"is_vendor"`
	StoreIDs []string `json:"store_ids,omitempty"`
}

func validateBearer(r *http.Request, key *rsa.PublicKey) (userID string, storeIDs []string, ok bool) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return "", nil, false
	}
	raw := strings.TrimPrefix(auth, "Bearer ")

	var claims gmClaims
	_, err := gojwt.ParseWithClaims(raw, &claims, func(t *gojwt.Token) (any, error) {
		if _, ok := t.Method.(*gojwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return key, nil
	})
	if err != nil {
		return "", nil, false
	}
	if claims.Subject == "" {
		return "", nil, false
	}
	return claims.Subject, claims.StoreIDs, true
}

func loadPublicKey() (*rsa.PublicKey, error) {
	raw := os.Getenv("JWT_PUBLIC_KEY")
	if raw == "" {
		return nil, fmt.Errorf("JWT_PUBLIC_KEY is required")
	}
	pemBytes, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		// Maybe it's already plain PEM, not base64
		pemBytes = []byte(raw)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	return rsaKey, nil
}

// ── Store lookup cache ────────────────────────────────────────────────────────

type storeEntry struct {
	storeID  string
	fetchedAt time.Time
}

type storeCache struct {
	mu      sync.Mutex
	entries map[string]storeEntry
	baseURL string
	ttl     time.Duration
	log     zerolog.Logger
}

func newStoreCache(storefrontBaseURL string, log zerolog.Logger) *storeCache {
	return &storeCache{
		entries: make(map[string]storeEntry),
		baseURL: storefrontBaseURL,
		ttl:     5 * time.Minute,
		log:     log,
	}
}

func (c *storeCache) lookup(ctx context.Context, userID string) (string, bool) {
	c.mu.Lock()
	entry, ok := c.entries[userID]
	c.mu.Unlock()

	if ok && time.Since(entry.fetchedAt) < c.ttl {
		if entry.storeID == "" {
			return "", false
		}
		return entry.storeID, true
	}

	// Fetch from storefront service (service-to-service: inject X-User-ID directly).
	storeID := c.fetchFromStorefront(ctx, userID)

	c.mu.Lock()
	c.entries[userID] = storeEntry{storeID: storeID, fetchedAt: time.Now()}
	c.mu.Unlock()

	if storeID == "" {
		return "", false
	}
	return storeID, true
}

func (c *storeCache) invalidate(userID string) {
	c.mu.Lock()
	delete(c.entries, userID)
	c.mu.Unlock()
}

func (c *storeCache) fetchFromStorefront(ctx context.Context, userID string) string {
	if c.baseURL == "" {
		return ""
	}
	reqURL := strings.TrimRight(c.baseURL, "/") + "/v1/storefront/stores/mine"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("X-User-ID", userID)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var store struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &store); err != nil {
		return ""
	}
	return store.ID
}

// needsStoreIDs returns true for routes whose upstream services require X-Store-IDs.
func needsStoreIDs(prefix string) bool {
	return strings.HasPrefix(prefix, "/v1/catalogue/") ||
		strings.HasPrefix(prefix, "/v1/orders/") ||
		strings.HasPrefix(prefix, "/v1/crm/") ||
		strings.HasPrefix(prefix, "/v1/analytics/")
}

// ── Upstream loading ──────────────────────────────────────────────────────────

func loadUpstreams() (map[string]string, error) {
	required := map[string]string{
		"/v1/auth/":       "UPSTREAM_AUTH",
		"/v1/identity/":   "UPSTREAM_IDENTITY",
		"/v1/storefront/": "UPSTREAM_STOREFRONT",
		"/v1/catalogue/":  "UPSTREAM_CATALOGUE",
		"/v1/orders/":     "UPSTREAM_ORDERS",
		"/v1/crm/":        "UPSTREAM_ORDERS",
		"/v1/analytics/":  "UPSTREAM_ORDERS",
	}

	seen := map[string]bool{}
	result := map[string]string{}

	for prefix, envKey := range required {
		if seen[envKey] {
			result[prefix] = os.Getenv(envKey)
			continue
		}
		val := os.Getenv(envKey)
		if val == "" {
			return nil, fmt.Errorf("%s is required", envKey)
		}
		if _, err := url.ParseRequestURI(val); err != nil {
			return nil, fmt.Errorf("%s %q is not a valid URL: %w", envKey, val, err)
		}
		seen[envKey] = true
		result[prefix] = val
	}
	return result, nil
}

// ── Reverse proxy ─────────────────────────────────────────────────────────────

func newProxy(target string, log zerolog.Logger) *httputil.ReverseProxy {
	targetURL, _ := url.ParseRequestURI(target)
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Del("X-Forwarded-Host")
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Credentials")
		resp.Header.Del("Access-Control-Allow-Methods")
		resp.Header.Del("Access-Control-Allow-Headers")
		resp.Header.Del("Access-Control-Max-Age")
		resp.Header.Del("Vary")
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Error().Err(err).Str("path", r.URL.Path).Msg("upstream error")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `{"error":"upstream service unavailable"}`)
	}

	return proxy
}

// ── CORS ──────────────────────────────────────────────────────────────────────

func setCORS(w http.ResponseWriter, r *http.Request, allowed []string) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}
	for _, o := range allowed {
		if strings.TrimSpace(o) == origin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Device-ID")
			w.Header().Set("Vary", "Origin")
			return
		}
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
