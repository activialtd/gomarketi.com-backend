// Package errors defines standard application error types and their HTTP
// status code mappings. All errors produced by service and repository layers
// must be wrapped in one of these types before reaching the handler layer.
// Plain database errors, network errors, etc. must never be returned raw.
package errors

import (
	stderrors "errors"
	"fmt"
	"net/http"
)

// AppError is a structured error that carries an HTTP status code and a
// user-safe message alongside the underlying cause.
type AppError struct {
	Code    int    // HTTP status code
	Message string // safe to send to API clients
	Err     error  // underlying cause — log this, never expose it
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap enables errors.Is and errors.As to traverse the cause chain.
func (e *AppError) Unwrap() error { return e.Err }

// HTTPStatus returns the HTTP status code that the handler should respond with.
func (e *AppError) HTTPStatus() int { return e.Code }

// ── Constructors ──────────────────────────────────────────────────────────────

// NotFound returns a 404 error.
func NotFound(msg string) *AppError {
	return &AppError{Code: http.StatusNotFound, Message: msg}
}

// Unauthorized returns a 401 error. Use for missing or invalid credentials,
// not for permission failures (use Forbidden for those).
func Unauthorized(msg string) *AppError {
	return &AppError{Code: http.StatusUnauthorized, Message: msg}
}

// Forbidden returns a 403 error. The caller is authenticated but lacks permission.
func Forbidden(msg string) *AppError {
	return &AppError{Code: http.StatusForbidden, Message: msg}
}

// BadRequest returns a 400 error for malformed or logically invalid input
// where no field-level detail is needed. For field-level validation failures,
// use Unprocessable instead.
func BadRequest(msg string) *AppError {
	return &AppError{Code: http.StatusBadRequest, Message: msg}
}

// Conflict returns a 409 error for duplicate resource or state conflicts
// (e.g. email already registered, idempotency key collision).
func Conflict(msg string) *AppError {
	return &AppError{Code: http.StatusConflict, Message: msg}
}

// Unprocessable returns a 422 error for structurally valid but semantically
// invalid input. Handlers typically attach field-level detail alongside this.
func Unprocessable(msg string) *AppError {
	return &AppError{Code: http.StatusUnprocessableEntity, Message: msg}
}

// TooManyRequests returns a 429 error.
func TooManyRequests(msg string) *AppError {
	return &AppError{Code: http.StatusTooManyRequests, Message: msg}
}

// Internal wraps an unexpected error as a 500. The Message is a generic string
// safe for clients. Always log e.Err before returning this to the handler —
// it must never be sent to the client.
func Internal(err error) *AppError {
	return &AppError{
		Code:    http.StatusInternalServerError,
		Message: "an internal error occurred",
		Err:     err,
	}
}

// Wrap is the escape hatch for status codes not covered by the named
// constructors. Prefer the named constructors for readability.
func Wrap(code int, msg string, err error) *AppError {
	return &AppError{Code: code, Message: msg, Err: err}
}

// ── Predicates ────────────────────────────────────────────────────────────────

// IsNotFound reports whether err (or any in its chain) is a 404 AppError.
func IsNotFound(err error) bool { return hasCode(err, http.StatusNotFound) }

// IsConflict reports whether err is a 409 AppError.
func IsConflict(err error) bool { return hasCode(err, http.StatusConflict) }

// IsUnauthorized reports whether err is a 401 AppError.
func IsUnauthorized(err error) bool { return hasCode(err, http.StatusUnauthorized) }

// IsForbidden reports whether err is a 403 AppError.
func IsForbidden(err error) bool { return hasCode(err, http.StatusForbidden) }

// IsInternal reports whether err is a 500 AppError.
func IsInternal(err error) bool { return hasCode(err, http.StatusInternalServerError) }

func hasCode(err error, code int) bool {
	var ae *AppError
	return stderrors.As(err, &ae) && ae.Code == code
}

// ── stdlib re-exports ─────────────────────────────────────────────────────────

// As is a convenience re-export so callers that import only this package do
// not also need to import stdlib errors.
func As(err error, target any) bool { return stderrors.As(err, target) }

// Is is a convenience re-export of errors.Is.
func Is(err, target error) bool { return stderrors.Is(err, target) }

// New creates a plain error with the given message. Use sparingly — prefer
// the typed constructors above for errors that cross service boundaries.
func New(msg string) error { return stderrors.New(msg) }
