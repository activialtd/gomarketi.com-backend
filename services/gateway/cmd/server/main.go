package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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

	allowedOrigins := strings.Split(getenv("ALLOWED_ORIGINS", ""), ",")

	mux := http.NewServeMux()

	// Health check — load balancers and Heroku router hit this.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	// Route each path prefix to the correct upstream.
	for prefix, target := range upstreams {
		prefix, target := prefix, target // capture loop vars
		proxy := newProxy(target, log)
		mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
			setCORS(w, r, allowedOrigins)
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
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

// loadUpstreams reads the five upstream URLs from env and returns a map of
// path prefix → upstream URL. All upstreams are required.
func loadUpstreams() (map[string]string, error) {
	required := map[string]string{
		// path prefix        env var
		"/v1/auth/":         "UPSTREAM_AUTH",
		"/v1/identity/":     "UPSTREAM_IDENTITY",
		"/v1/storefront/":   "UPSTREAM_STOREFRONT",
		"/v1/catalogue/":    "UPSTREAM_CATALOGUE",
		"/v1/orders/":       "UPSTREAM_ORDERS",
		"/v1/crm/":          "UPSTREAM_ORDERS",     // orders service also owns CRM
		"/v1/analytics/":    "UPSTREAM_ORDERS",     // and analytics
	}

	seen := map[string]bool{}
	result := map[string]string{}

	for prefix, envKey := range required {
		if seen[envKey] {
			// already validated this env var for a different prefix
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

// newProxy creates a reverse proxy that forwards to target, stripping hop-by-hop
// headers and injecting X-Forwarded-For / X-Forwarded-Proto.
func newProxy(target string, log zerolog.Logger) *httputil.ReverseProxy {
	targetURL, _ := url.ParseRequestURI(target)

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Preserve the original Host header so upstream services can log it.
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Del("X-Forwarded-Host")
	}

	// Strip CORS headers from upstream responses — the gateway owns CORS.
	// Without this, both the upstream and the gateway set the headers and
	// browsers reject the duplicate values (e.g. "true, true").
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

// setCORS writes CORS headers. Matches the exact origin against the allowlist.
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
