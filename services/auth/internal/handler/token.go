package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/dto"
)

const refreshCookieName = "refresh_token"

// refreshTokenBody is the optional JSON body carrying the refresh token for
// mobile clients that can't rely on a cookie jar. Ignored for web clients,
// which always have the cookie present.
type refreshTokenBody struct {
	RefreshToken string `json:"refresh_token"`
}

// readRefreshToken tries the HttpOnly cookie first (web), then falls back to
// a JSON body field (mobile). c.ShouldBindJSON on an empty/absent body is a
// harmless no-op here since rawToken is already checked for emptiness.
func readRefreshToken(c *gin.Context) string {
	if rawToken, err := c.Cookie(refreshCookieName); err == nil && rawToken != "" {
		return rawToken
	}
	var body refreshTokenBody
	_ = c.ShouldBindJSON(&body) //nolint:errcheck
	return body.RefreshToken
}

// RefreshTokens godoc
// POST /v1/auth/token/refresh
func (h *Handler) RefreshTokens(c *gin.Context) {
	rawToken := readRefreshToken(c)
	if rawToken == "" {
		c.JSON(http.StatusUnauthorized, dto.ErrorResp{Error: "missing refresh token"})
		return
	}

	resp, newRawToken, svcErr := h.svc.RefreshTokens(c.Request.Context(), rawToken)
	if svcErr != nil {
		h.writeError(c, svcErr)
		return
	}

	h.respondWithAuth(c, http.StatusOK, resp, newRawToken)
}

// Logout godoc
// POST /v1/auth/logout
func (h *Handler) Logout(c *gin.Context) {
	rawToken := readRefreshToken(c)

	if rawToken != "" {
		_ = h.svc.Logout(c.Request.Context(), rawToken)
	}

	h.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}
