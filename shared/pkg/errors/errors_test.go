package errors_test

import (
	stderrors "errors"
	"net/http"
	"testing"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
)

func TestConstructors(t *testing.T) {
	tests := []struct {
		name       string
		err        *errors.AppError
		wantCode   int
		wantMsg    string
		wantIsFunc func(error) bool
	}{
		{
			name:       "NotFound",
			err:        errors.NotFound("user not found"),
			wantCode:   http.StatusNotFound,
			wantMsg:    "user not found",
			wantIsFunc: errors.IsNotFound,
		},
		{
			name:       "Unauthorized",
			err:        errors.Unauthorized("invalid token"),
			wantCode:   http.StatusUnauthorized,
			wantMsg:    "invalid token",
			wantIsFunc: errors.IsUnauthorized,
		},
		{
			name:       "Forbidden",
			err:        errors.Forbidden("access denied"),
			wantCode:   http.StatusForbidden,
			wantMsg:    "access denied",
			wantIsFunc: errors.IsForbidden,
		},
		{
			name:       "BadRequest",
			err:        errors.BadRequest("missing field"),
			wantCode:   http.StatusBadRequest,
			wantMsg:    "missing field",
			wantIsFunc: nil,
		},
		{
			name:       "Conflict",
			err:        errors.Conflict("email taken"),
			wantCode:   http.StatusConflict,
			wantMsg:    "email taken",
			wantIsFunc: errors.IsConflict,
		},
		{
			name:       "TooManyRequests",
			err:        errors.TooManyRequests("slow down"),
			wantCode:   http.StatusTooManyRequests,
			wantMsg:    "slow down",
			wantIsFunc: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.HTTPStatus() != tc.wantCode {
				t.Errorf("HTTPStatus() = %d, want %d", tc.err.HTTPStatus(), tc.wantCode)
			}
			if tc.err.Message != tc.wantMsg {
				t.Errorf("Message = %q, want %q", tc.err.Message, tc.wantMsg)
			}
			if tc.err.Error() != tc.wantMsg {
				t.Errorf("Error() = %q, want %q", tc.err.Error(), tc.wantMsg)
			}
			if tc.wantIsFunc != nil && !tc.wantIsFunc(tc.err) {
				t.Errorf("Is predicate returned false for %s", tc.name)
			}
		})
	}
}

func TestInternal_WrapsErr(t *testing.T) {
	cause := stderrors.New("db connection refused")
	err := errors.Internal(cause)

	if err.HTTPStatus() != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", err.HTTPStatus())
	}
	if !errors.IsInternal(err) {
		t.Fatal("IsInternal should be true")
	}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is should find the cause through Unwrap")
	}
	// Client-safe message must not expose the cause.
	if err.Message != "an internal error occurred" {
		t.Errorf("unexpected message: %q", err.Message)
	}
}

func TestWrap_Unwrap(t *testing.T) {
	cause := stderrors.New("original error")
	err := errors.Wrap(http.StatusBadRequest, "bad input", cause)

	if !errors.Is(err, cause) {
		t.Fatal("Wrap should preserve the cause chain via Unwrap")
	}
}

func TestAs_ReturnsAppError(t *testing.T) {
	err := errors.NotFound("thing not found")
	var ae *errors.AppError
	if !errors.As(err, &ae) {
		t.Fatal("errors.As should resolve to *AppError")
	}
	if ae.Code != http.StatusNotFound {
		t.Errorf("got code %d, want 404", ae.Code)
	}
}

func TestPredicates_WrongCode(t *testing.T) {
	err := errors.NotFound("x")
	if errors.IsConflict(err) {
		t.Error("IsConflict should be false for a NotFound error")
	}
	if errors.IsUnauthorized(err) {
		t.Error("IsUnauthorized should be false for a NotFound error")
	}
}

func TestNew_PlainError(t *testing.T) {
	err := errors.New("plain")
	if err == nil {
		t.Fatal("New should not return nil")
	}
	if err.Error() != "plain" {
		t.Errorf("got %q, want %q", err.Error(), "plain")
	}
}
