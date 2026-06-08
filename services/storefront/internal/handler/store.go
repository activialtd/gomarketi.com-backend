package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
)

// CreateStore godoc
// POST /v1/storefront/stores
func (h *Handler) CreateStore(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	var req dto.CreateStoreReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.CreateStore(c.Request.Context(), userID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// GetMyStore godoc
// GET /v1/storefront/stores/mine
func (h *Handler) GetMyStore(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	resp, err := h.svc.GetMyStore(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateStore godoc
// PATCH /v1/storefront/stores/:id
func (h *Handler) UpdateStore(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	storeID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	var req dto.UpdateStoreReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.UpdateStore(c.Request.Context(), userID, storeID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// CheckSlugAvailable godoc
// GET /v1/storefront/slugs/check?slug=your-store
func (h *Handler) CheckSlugAvailable(c *gin.Context) {
	slug := c.Query("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "slug query parameter required"})
		return
	}

	resp, err := h.svc.CheckSlugAvailable(c.Request.Context(), slug)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
