package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/dto"
)

const refreshCookieName = "refresh_token"

// RefreshTokens godoc
// POST /v1/auth/token/refresh
func (h *Handler) RefreshTokens(c *gin.Context) {
	rawToken, err := c.Cookie(refreshCookieName)
	if err != nil || rawToken == "" {
		c.JSON(http.StatusUnauthorized, dto.ErrorResp{Error: "missing refresh token"})
		return
	}

	resp, newRawToken, svcErr := h.svc.RefreshTokens(c.Request.Context(), rawToken)
	if svcErr != nil {
		h.writeError(c, svcErr)
		return
	}

	h.setRefreshCookie(c, newRawToken)
	c.JSON(http.StatusOK, resp)
}

// Logout godoc
// POST /v1/auth/logout
func (h *Handler) Logout(c *gin.Context) {
	rawToken, _ := c.Cookie(refreshCookieName)

	if rawToken != "" {
		_ = h.svc.Logout(c.Request.Context(), rawToken)
	}

	h.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}
