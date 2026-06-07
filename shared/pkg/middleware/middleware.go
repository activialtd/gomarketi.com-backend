// Package middleware provides shared HTTP middleware for GoMarket's Go services.
//
// Auth note: JWT validation is performed by Envoy, not by these middlewares.
// Envoy validates access tokens on protected routes and injects the user
// context as HTTP headers before forwarding to downstream services. These
// middlewares focus on observability, CORS, and reading those injected headers.
package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ── CORS ──────────────────────────────────────────────────────────────────────

// CORS returns a middleware that sets Access-Control-* headers for requests
// originating from an allowed origin. Non-matching origins receive no CORS
// headers (not an error — the browser enforces the restriction).
//
// allowedOrigins should come from config, e.g.:
//   - development: ["http://localhost:3000"]
//   - production:  ["https://gomarketi.com", "https://app.gomarketi.com"]
func CORS(allowedOrigins []string) gin.HandlerFunc {
	set := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		set[o] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if _, ok := set[origin]; ok {
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

// Recovery catches panics, logs them at ERROR level, and responds with a
// generic 500 JSON body. Always register this as the outermost middleware so
// it wraps all other handlers.
func Recovery(log zerolog.Logger) gin.HandlerFunc {
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
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": "an internal error occurred",
				})
			}
		}()
		c.Next()
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
