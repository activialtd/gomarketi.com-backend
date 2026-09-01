// Package middleware provides shared HTTP middleware for GoMarket's Go services.
//
// Auth note: JWT validation is performed by Envoy, not by these middlewares.
// Envoy validates access tokens on protected routes and injects the user
// context as HTTP headers before forwarding to downstream services. These
// middlewares focus on observability, CORS, and reading those injected headers.
package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog"
)

// ── CORS ──────────────────────────────────────────────────────────────────────

// CORS returns a middleware that sets Access-Control-* headers for requests
// originating from an allowed origin. Non-matching origins receive no CORS
// headers (not an error — the browser enforces the restriction).
//
// Every configured origin also implicitly allows its subdomains — every
// vendor's storefront lives on its own subdomain (e.g. cobi.gomarketi.com),
// so a bare "https://gomarketi.com" entry must cover the whole *.gomarketi.com
// fleet, not just the apex, or checkout (which calls the orders service
// cross-origin from the storefront) breaks for every vendor.
//
// allowedOrigins should come from config, e.g.:
//   - development: ["http://localhost:3000"]
//   - production:  ["https://gomarketi.com", "https://app.gomarketi.com"]
type originSuffix struct {
	scheme string // e.g. "https://"
	host   string // e.g. ".gomarketi.com" — leading dot enforces a real subdomain boundary
}

func CORS(allowedOrigins []string) gin.HandlerFunc {
	set := make(map[string]struct{}, len(allowedOrigins))
	var suffixes []originSuffix
	for _, o := range allowedOrigins {
		set[o] = struct{}{}
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			suffixes = append(suffixes, originSuffix{scheme: u.Scheme + "://", host: "." + u.Host})
		}
	}

	originAllowed := func(origin string) bool {
		if origin == "" {
			return false
		}
		if _, ok := set[origin]; ok {
			return true
		}
		for _, s := range suffixes {
			// Matches "scheme://<subdomain>.host" for any non-empty subdomain
			// — origin must be strictly longer than scheme+host so there's
			// at least one character in the subdomain label.
			if strings.HasPrefix(origin, s.scheme) && strings.HasSuffix(origin, s.host) &&
				len(origin) > len(s.scheme)+len(s.host) {
				return true
			}
		}
		return false
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if originAllowed(origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers",
			"Authorization, Content-Type, X-Request-ID, Idempotency-Key")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// ── Request ID ────────────────────────────────────────────────────────────────

const headerRequestID = "X-Request-ID"

// RequestID injects a unique request identifier into the gin context and
// echoes it in the response header. Uses the incoming X-Request-ID header
// when present (e.g. forwarded by Envoy), otherwise generates a new UUID v4.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(headerRequestID)
		if id == "" {
			id = uuid.NewString()
		}
		c.Set("request_id", id)
		c.Header(headerRequestID, id)
		c.Next()
	}
}

// ── Request Logger ────────────────────────────────────────────────────────────

// RequestLogger logs each HTTP request at INFO level with method, path, status
// code, latency, client IP, and request ID. Attach it after RequestID so the
// request_id field is always present in the log entry.
func RequestLogger(log zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		path := c.Request.URL.Path
		if q := c.Request.URL.RawQuery; q != "" {
			path += "?" + q
		}

		c.Next()

		reqID, _ := c.Get("request_id")
		log.Info().
			Str("request_id", stringVal(reqID)).
			Str("method", c.Request.Method).
			Str("path", path).
			Int("status", c.Writer.Status()).
			Dur("latency_ms", time.Since(start)).
			Str("ip", c.ClientIP()).
			Msg("http")
	}
}

// ── Recovery ──────────────────────────────────────────────────────────────────

// Recovery catches panics, logs them at ERROR level, responds with a generic
// 500 JSON body, and — for both panics and handlers that return a 5xx without
// panicking — writes a row to the shared error_events table (owned by
// admin-api, surfaced in the Admin Center's error queue). db may be nil (e.g.
// in tests), in which case error capture is skipped but recovery still works.
// Always register this as the outermost middleware so it wraps all other
// handlers.
func Recovery(log zerolog.Logger, db *sqlx.DB, serviceName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				reqID, _ := c.Get("request_id")
				log.Error().
					Interface("panic", r).
					Str("request_id", stringVal(reqID)).
					Str("method", c.Request.Method).
					Str("path", c.Request.URL.Path).
					Msg("panic recovered")
				recordErrorEvent(db, serviceName, log, c, http.StatusInternalServerError, stringVal(r))
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": "an internal error occurred",
				})
			}
		}()
		c.Next()
		if status := c.Writer.Status(); !c.IsAborted() && status >= http.StatusInternalServerError {
			recordErrorEvent(db, serviceName, log, c, status, "")
		}
	}
}

// recordErrorEvent writes one row to error_events for an HTTP-request-scoped
// failure (panic or 5xx response) caught by Recovery.
func recordErrorEvent(db *sqlx.DB, serviceName string, log zerolog.Logger, c *gin.Context, status int, panicVal string) {
	message := panicVal
	if message == "" {
		message = "HTTP " + http.StatusText(status)
	}

	reqID, _ := c.Get("request_id")
	userID := c.GetString(CtxKeyUserID)

	fields := map[string]any{
		"request_id": stringVal(reqID),
		"method":     c.Request.Method,
	}

	writeErrorEvent(db, log, serviceName, message, c.Request.URL.Path, &status, userID, fields)
}

// RecordBackgroundError writes an error_events row for a failure that
// happened OUTSIDE the HTTP request/response cycle — a goroutine, a
// scheduled job — the same queue Recovery writes to for request-scoped
// failures, but reachable from anywhere a service holds a *sqlx.DB. These
// failures (a third-party API call inside a background provisioning step,
// a transactional email, a scheduled release job) previously only ever hit
// a zerolog Warn() — invisible to the Admin Center's error queue unless
// someone went looking at container logs. fields is optional free-form
// context (e.g. {"vendor_id": "..."}) shown alongside the error.
func RecordBackgroundError(db *sqlx.DB, log zerolog.Logger, serviceName, message string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	writeErrorEvent(db, log, serviceName, message, "", nil, "", fields)
}

// writeErrorEvent is the shared low-level writer behind both the HTTP-path
// (recordErrorEvent) and background-path (RecordBackgroundError) capture.
// Best-effort: a failure to record must never affect the caller — a request
// already in flight, or a background job already doing its own thing — so
// errors here are logged (not returned) and the write runs against a short,
// bounded context rather than whatever context the caller has (which, for
// an HTTP request, dies the moment the response is written).
func writeErrorEvent(db *sqlx.DB, log zerolog.Logger, serviceName, message, requestPath string, statusCode *int, userID string, fields map[string]any) {
	if db == nil {
		return
	}

	fieldsJSON, err := json.Marshal(fields)
	if err != nil {
		fieldsJSON = []byte("{}")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = db.ExecContext(ctx, `
		INSERT INTO error_events (service, level, message, request_path, status_code, user_id, context)
		VALUES ($1, 'error', $2, NULLIF($3, ''), $4, NULLIF($5, ''), $6)
	`, serviceName, message, requestPath, statusCode, userID, fieldsJSON)
	if err != nil {
		log.Error().Err(err).Msg("failed to record error_event")
	}
}

// ── Envoy user-context headers ────────────────────────────────────────────────

// Header names injected by Envoy after JWT validation.
const (
	HeaderUserID   = "X-User-ID"
	HeaderIsVendor = "X-Is-Vendor"
	HeaderStoreIDs = "X-Store-IDs" // comma-separated list of store UUID strings
)

// Gin context keys set by UserContext.
const (
	CtxKeyUserID   = "user_id"
	CtxKeyIsVendor = "is_vendor"
	CtxKeyStoreIDs = "store_ids"
)

// UserContext reads Envoy-injected headers and stores them in the gin context
// so downstream handlers can call c.GetString(middleware.CtxKeyUserID) etc.
// Used by identity, catalogue, orders, storefront — NOT by the auth service
// (which validates JWTs directly before Envoy is involved).
func UserContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(CtxKeyUserID, c.GetHeader(HeaderUserID))
		c.Set(CtxKeyIsVendor, c.GetHeader(HeaderIsVendor) == "true")

		if raw := c.GetHeader(HeaderStoreIDs); raw != "" {
			c.Set(CtxKeyStoreIDs, strings.Split(raw, ","))
		} else {
			c.Set(CtxKeyStoreIDs, []string{})
		}

		c.Next()
	}
}

// RequireUser aborts with 401 if X-User-ID is not present. Use on any route
// that requires an authenticated user but is not already guarded by Envoy JWT
// validation (e.g. in integration tests or when Envoy is bypassed locally).
func RequireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader(HeaderUserID) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
			})
			return
		}
		c.Next()
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func stringVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
