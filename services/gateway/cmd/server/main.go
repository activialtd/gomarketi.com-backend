package main

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
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

		handler := func(w http.ResponseWriter, r *http.Request) {
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

			// Public storefront routes (e.g. store lookup by slug) require no auth.
			if strings.HasPrefix(r.URL.Path, "/v1/storefront/public/") {
				proxy.ServeHTTP(w, r)
				return
			}

			// Public catalogue routes (published products for storefront pages).
			if strings.HasPrefix(r.URL.Path, "/v1/catalogue/public/") {
				proxy.ServeHTTP(w, r)
				return
			}

			// Public order creation — storefront checkout creates orders without a vendor JWT.
			if strings.HasPrefix(r.URL.Path, "/v1/orders/public") {
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
		}

		mux.HandleFunc(prefix, handler)
		// Also register the exact path without a trailing slash so a bare
		// request like GET /v1/orders doesn't hit ServeMux's subtree
		// redirect (which would 307 to /v1/orders/ and risk a redirect
		// loop against Gin's own trailing-slash redirect on the other side).
		mux.HandleFunc(strings.TrimSuffix(prefix, "/"), handler)
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
	storeID   string
	fetchedAt time.Time
	failed    bool // true when the last lookup returned no store
}

type storeCache struct {
	mu         sync.Mutex
	entries    map[string]storeEntry
	baseURL    string
	ttl        time.Duration
	failedTTL  time.Duration // short TTL for failed lookups — retry sooner
	log        zerolog.Logger
}

func newStoreCache(storefrontBaseURL string, log zerolog.Logger) *storeCache {
	return &storeCache{
		entries:   make(map[string]storeEntry),
		baseURL:   storefrontBaseURL,
		ttl:       5 * time.Minute,
		failedTTL: 5 * time.Second, // retry failed lookups after 5s (handles slow service start)
		log:       log,
	}
}

func (c *storeCache) lookup(ctx context.Context, userID string) (string, bool) {
	c.mu.Lock()
	entry, ok := c.entries[userID]
	c.mu.Unlock()

	if ok {
		ttl := c.ttl
		if entry.failed {
			ttl = c.failedTTL // don't block requests for 5 min on a temporary failure
		}
		if time.Since(entry.fetchedAt) < ttl {
			if entry.storeID == "" {
				return "", false
			}
			return entry.storeID, true
		}
	}

	// Fetch from storefront service (service-to-service: inject X-User-ID directly).
	storeID := c.fetchFromStorefront(ctx, userID)

	c.mu.Lock()
	c.entries[userID] = storeEntry{
		storeID:   storeID,
		fetchedAt: time.Now(),
		failed:    storeID == "",
	}
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
		c.log.Warn().Msg("store cache: UPSTREAM_STOREFRONT not set — cannot look up store ID")
		return ""
	}
	reqURL := strings.TrimRight(c.baseURL, "/") + "/v1/storefront/stores/mine"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		c.log.Error().Err(err).Msg("store cache: failed to build storefront request")
		return ""
	}
	req.Header.Set("X-User-ID", userID)

	// Use a longer timeout — storefront may be waiting on a cold Neon DB connection.
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.log.Warn().Err(err).Str("user_id", userID).Msg("store cache: storefront unreachable")
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		// Vendor hasn't created a store yet — not an error.
		return ""
	}
	if resp.StatusCode != http.StatusOK {
		c.log.Warn().Int("status", resp.StatusCode).Str("user_id", userID).
			Str("body", string(body)).Msg("store cache: storefront returned non-200")
		return ""
	}

	var store struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &store); err != nil {
		c.log.Error().Err(err).Str("body", string(body)).Msg("store cache: failed to parse store response")
		return ""
	}
	if store.ID == "" {
		c.log.Warn().Str("user_id", userID).Msg("store cache: storefront returned empty store ID")
		return ""
	}
	c.log.Debug().Str("user_id", userID).Str("store_id", store.ID).Msg("store cache: resolved store ID")
	return store.ID
}

// needsStoreIDs returns true for routes whose upstream services require X-Store-IDs.
func needsStoreIDs(prefix string) bool {
	return strings.HasPrefix(prefix, "/v1/catalogue/") ||
		strings.HasPrefix(prefix, "/v1/orders/") ||
		strings.HasPrefix(prefix, "/v1/crm/") ||
		strings.HasPrefix(prefix, "/v1/analytics/") ||
		strings.HasPrefix(prefix, "/v1/wallet/")
}

// ── Upstream loading ──────────────────────────────────────────────────────────

func loadUpstreams() (map[string]string, error) {
	// loadDotEnv reads KEY=VALUE lines from .env in the working directory.
	// It only sets vars that are not already in the environment, so real env
	// vars always win (same behaviour as viper.AutomaticEnv).
	loadDotEnv()

	// Required upstreams — gateway will not start without these.
	required := map[string]string{
		"/v1/auth/":       "UPSTREAM_AUTH",
		"/v1/storefront/": "UPSTREAM_STOREFRONT",
	}

	// Optional upstreams — routes are registered only when the var is set.
	optional := map[string]string{
		"/v1/identity/":  "UPSTREAM_IDENTITY",
		"/v1/catalogue/": "UPSTREAM_CATALOGUE",
		"/v1/orders/":    "UPSTREAM_ORDERS",
		"/v1/crm/":       "UPSTREAM_ORDERS",
		"/v1/analytics/": "UPSTREAM_ORDERS",
		"/v1/wallet/":    "UPSTREAM_ORDERS",
	}

	result := map[string]string{}

	for prefix, envKey := range required {
		val := os.Getenv(envKey)
		if val == "" {
			return nil, fmt.Errorf("%s is required", envKey)
		}
		if _, err := url.ParseRequestURI(val); err != nil {
			return nil, fmt.Errorf("%s %q is not a valid URL: %w", envKey, val, err)
		}
		result[prefix] = val
	}

	seen := map[string]bool{}
	for prefix, envKey := range optional {
		val := os.Getenv(envKey)
		if val == "" {
			continue // service not running locally — skip route
		}
		if !seen[envKey] {
			if _, err := url.ParseRequestURI(val); err != nil {
				return nil, fmt.Errorf("%s %q is not a valid URL: %w", envKey, val, err)
			}
			seen[envKey] = true
		}
		result[prefix] = val
	}

	return result, nil
}

// loadDotEnv reads KEY=VALUE pairs from .env in the current directory and
// sets them in the process environment if not already set.
func loadDotEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return // no .env — fine in production
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if os.Getenv(key) == "" { // don't override real env vars
			os.Setenv(key, val)
		}
	}
}

// ── Reverse proxy ─────────────────────────────────────────────────────────────

func newProxy(target string, log zerolog.Logger) *httputil.ReverseProxy {
	targetURL, _ := url.ParseRequestURI(target)
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Wrap the default transport with retry-on-transient-error logic.
	// Handles EOF / connection-reset that occur when an upstream service
	// restarts or when Neon's serverless DB wakes from idle.
	proxy.Transport = &retryTransport{
		base: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     30 * time.Second, // shorter than Neon's idle timeout
			DisableKeepAlives:   false,
		},
		log: log,
	}

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Del("X-Forwarded-Host")

		// Buffer body so the retry transport can re-send it on retry.
		if req.Body != nil && req.Body != http.NoBody {
			body, _ := io.ReadAll(req.Body)
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(body)), nil
			}
		}
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

// retryTransport retries once on transient upstream errors (EOF, connection
// reset, connection refused). These occur when a service restarts or when an
// idle keep-alive connection is recycled by the upstream's TCP stack.
type retryTransport struct {
	base http.RoundTripper
	log  zerolog.Logger
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err == nil || !isTransientUpstreamErr(err) {
		return resp, err
	}

	// One retry after a short pause.
	time.Sleep(80 * time.Millisecond)
	t.log.Warn().Err(err).Str("path", req.URL.Path).Msg("transient upstream error — retrying once")

	// Restore the request body for the retry.
	if req.GetBody != nil {
		body, bodyErr := req.GetBody()
		if bodyErr != nil {
			return nil, err // return original error
		}
		req.Body = body
	}

	return t.base.RoundTrip(req)
}

func isTransientUpstreamErr(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		msg := strings.ToLower(urlErr.Error())
		return urlErr.Temporary() ||
			strings.Contains(msg, "eof") ||
			strings.Contains(msg, "connection reset") ||
			strings.Contains(msg, "connection refused") ||
			strings.Contains(msg, "broken pipe")
	}
	return false
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
