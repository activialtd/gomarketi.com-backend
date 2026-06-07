package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/dto"
)

// Register godoc
// POST /v1/auth/register
func (h *Handler) Register(c *gin.Context) {
	var req dto.RegisterReq
	if !h.bind(c, &req) {
		return
	}

	resp, rawToken, err := h.svc.Register(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	h.setRefreshCookie(c, rawToken)
	c.JSON(http.StatusCreated, resp)
}

// Login godoc
// POST /v1/auth/login
func (h *Handler) Login(c *gin.Context) {
	var req dto.LoginReq
	if !h.bind(c, &req) {
		return
	}

	resp, rawToken, err := h.svc.LoginWithPassword(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	h.setRefreshCookie(c, rawToken)
	c.JSON(http.StatusOK, resp)
}
