package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/dto"
)

// AuthGoogle godoc
// POST /v1/auth/oauth/google
func (h *Handler) AuthGoogle(c *gin.Context) {
	var req dto.GoogleAuthReq
	if !h.bind(c, &req) {
		return
	}

	resp, rawToken, err := h.svc.AuthGoogle(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	h.setRefreshCookie(c, rawToken)
	c.JSON(http.StatusOK, resp)
}

// AuthApple godoc
// POST /v1/auth/oauth/apple
func (h *Handler) AuthApple(c *gin.Context) {
	var req dto.AppleAuthReq
	if !h.bind(c, &req) {
		return
	}

	resp, rawToken, err := h.svc.AuthApple(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	h.setRefreshCookie(c, rawToken)
	c.JSON(http.StatusOK, resp)
}
