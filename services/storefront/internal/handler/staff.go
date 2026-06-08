package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
)

// ListStaff godoc
// GET /v1/storefront/stores/:id/staff
func (h *Handler) ListStaff(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	storeID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	staff, err := h.svc.ListStaff(c.Request.Context(), userID, storeID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"staff": staff})
}

// InviteStaff godoc
// POST /v1/storefront/stores/:id/staff
func (h *Handler) InviteStaff(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	storeID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	var req dto.InviteStaffReq
	if !h.bind(c, &req) {
		return
	}

	member, err := h.svc.InviteStaff(c.Request.Context(), userID, storeID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, member)
}

// RemoveStaff godoc
// DELETE /v1/storefront/stores/:id/staff/:staff_id
func (h *Handler) RemoveStaff(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	storeID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	staffID, ok := h.pathUUID(c, "staff_id")
	if !ok {
		return
	}

	if err := h.svc.RemoveStaff(c.Request.Context(), userID, storeID, staffID); err != nil {
		h.writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
