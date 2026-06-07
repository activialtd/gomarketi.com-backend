// Package handler wires HTTP request parsing to service calls.
// Each handler file corresponds to one group of related endpoints.
// Handlers do not contain business logic — only input parsing, output
// serialisation, and cookie/header management.
package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/service"
)

// Handler holds the service and any handler-level config.
type Handler struct {
	svc      *service.AuthService
	validate *validator.Validate
	// refreshTTL controls the Max-Age on the refresh token cookie.
	refreshTTL time.Duration
	// secure controls whether cookies are set with Secure flag.
	// Should be true in production (HTTPS only).
	secure bool
}

// New creates a Handler.
func New(svc *service.AuthService, refreshTTL time.Duration, secure bool) *Handler {
	return &Handler{
		svc:        svc,
		validate:   validator.New(),
		refreshTTL: refreshTTL,
		secure:     secure,
	}
}

// bind decodes JSON body and runs struct-tag validation.
// It writes the error response and returns false on failure.
func (h *Handler) bind(c *gin.Context, req interface{}) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "invalid request body"})
		return false
	}
	if err := h.validate.Struct(req); err != nil {
		var fields []dto.FieldError
		if valErrs, ok := err.(validator.ValidationErrors); ok {
			for _, ve := range valErrs {
				fields = append(fields, dto.FieldError{
					Field:   ve.Field(),
					Message: ve.Tag(),
				})
			}
		}
		c.JSON(http.StatusUnprocessableEntity, dto.ValidationErrorResp{
			Error:  "validation failed",
			Fields: fields,
		})
		return false
	}
	return true
}

// writeError maps AppError to the correct HTTP status.
func (h *Handler) writeError(c *gin.Context, err error) {
	var appErr *apperrors.AppError
	if apperrors.As(err, &appErr) {
		c.JSON(appErr.HTTPStatus(), dto.ErrorResp{Error: appErr.Message})
		return
	}
	c.JSON(http.StatusInternalServerError, dto.ErrorResp{Error: "internal server error"})
}

// setRefreshCookie sets the HttpOnly refresh token cookie.
func (h *Handler) setRefreshCookie(c *gin.Context, rawToken string) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(
		refreshCookieName,
		rawToken,
		int(h.refreshTTL.Seconds()),
		"/v1/auth",
		"",
		h.secure,
		true, // HttpOnly
	)
}

// clearRefreshCookie expires the refresh token cookie (logout).
func (h *Handler) clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(
		refreshCookieName,
		"",
		-1,
		"/v1/auth",
		"",
		h.secure,
		true,
	)
}
