package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/dto"
)

// GetMe godoc
// GET /v1/identity/me
func (h *Handler) GetMe(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	resp, err := h.svc.GetMe(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateMe godoc
// PATCH /v1/identity/me
func (h *Handler) UpdateMe(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	var req dto.UpdateProfileReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.UpdateMe(c.Request.Context(), userID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
