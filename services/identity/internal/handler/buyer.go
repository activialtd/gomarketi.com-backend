package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/dto"
)

// ListAddresses godoc
// GET /v1/identity/me/addresses
func (h *Handler) ListAddresses(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	addrs, err := h.svc.ListAddresses(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"addresses": addrs})
}

// AddAddress godoc
// POST /v1/identity/me/addresses
func (h *Handler) AddAddress(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	var req dto.AddressReq
	if !h.bind(c, &req) {
		return
	}

	addr, err := h.svc.AddAddress(c.Request.Context(), userID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, addr)
}

// UpdateAddress godoc
// PATCH /v1/identity/me/addresses/:id
func (h *Handler) UpdateAddress(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	addressID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	var req dto.AddressReq
	if !h.bind(c, &req) {
		return
	}

	addr, err := h.svc.UpdateAddress(c.Request.Context(), userID, addressID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, addr)
}

// DeleteAddress godoc
// DELETE /v1/identity/me/addresses/:id
func (h *Handler) DeleteAddress(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	addressID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	if err := h.svc.DeleteAddress(c.Request.Context(), userID, addressID); err != nil {
		h.writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// SetDefaultAddress godoc
// POST /v1/identity/me/addresses/:id/set-default
func (h *Handler) SetDefaultAddress(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	addressID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	if err := h.svc.SetDefaultAddress(c.Request.Context(), userID, addressID); err != nil {
		h.writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
