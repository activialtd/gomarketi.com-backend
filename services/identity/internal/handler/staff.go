package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/service"
)

// callerStoreID reads the first store UUID from the Envoy-injected X-Store-IDs header.
func (h *Handler) callerStoreID(c *gin.Context) (uuid.UUID, bool) {
	raw, _ := c.Get(middleware.CtxKeyStoreIDs)
	ids, _ := raw.([]string)
	if len(ids) == 0 || strings.TrimSpace(ids[0]) == "" {
		c.JSON(http.StatusForbidden, map[string]string{"error": "no store associated with this account"})
		return uuid.UUID{}, false
	}
	id, err := uuid.Parse(ids[0])
	if err != nil {
		c.JSON(http.StatusForbidden, map[string]string{"error": "invalid store id"})
		return uuid.UUID{}, false
	}
	return id, true
}

// ListStaff godoc
// GET /v1/identity/vendor/staff
func (h *Handler) ListStaff(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	staff, err := h.svc.ListStaff(c.Request.Context(), storeID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"staff": staff})
}

// CreateStaff godoc
// POST /v1/identity/vendor/staff
func (h *Handler) CreateStaff(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	var req service.CreateStaffReq
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, apperrors.BadRequest("invalid request body"))
		return
	}
	member, err := h.svc.CreateStaff(c.Request.Context(), storeID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, member)
}

// UpdateStaff godoc
// PATCH /v1/identity/vendor/staff/:id
func (h *Handler) UpdateStaff(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	staffID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	var req service.UpdateStaffReq
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, apperrors.BadRequest("invalid request body"))
		return
	}
	member, err := h.svc.UpdateStaff(c.Request.Context(), storeID, staffID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, member)
}

// DeleteStaff godoc
// DELETE /v1/identity/vendor/staff/:id
func (h *Handler) DeleteStaff(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	staffID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteStaff(c.Request.Context(), storeID, staffID); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
