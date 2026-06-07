package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/dto"
)

// ValidateToken godoc
// POST /v1/internal/validate-token  (called by Envoy ext_authz)
// Accepts: Authorization: Bearer <access_token>
func (h *Handler) ValidateToken(c *gin.Context) {
	raw := c.GetHeader("Authorization")
	token := strings.TrimPrefix(raw, "Bearer ")
	if token == "" {
		c.JSON(http.StatusUnauthorized, dto.ErrorResp{Error: "missing token"})
		return
	}

	resp, err := h.svc.ValidateToken(c.Request.Context(), token)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
