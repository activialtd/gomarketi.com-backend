package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
)

// ── Delivery options ──────────────────────────────────────────────────────────

// ListDeliveryOptionsPublic godoc
// GET /v1/storefront/public/stores/:slug/delivery-options — no auth required.
// Checkout uses this when it needs the options without refetching the store.
func (h *Handler) ListDeliveryOptionsPublic(c *gin.Context) {
	store, err := h.svc.GetStoreBySlug(c.Request.Context(), c.Param("slug"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	storeID, err := uuid.Parse(store.ID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	opts, err := h.svc.ListDeliveryOptions(c.Request.Context(), storeID, true)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, opts)
}

// ListDeliveryOptions godoc
// GET /v1/storefront/stores/:id/delivery-options — vendor dashboard, includes
// options the vendor has disabled.
func (h *Handler) ListDeliveryOptions(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	storeID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	opts, err := h.svc.ListDeliveryOptionsForVendor(c.Request.Context(), userID, storeID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, opts)
}

// CreateDeliveryOption godoc
// POST /v1/storefront/stores/:id/delivery-options
func (h *Handler) CreateDeliveryOption(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	storeID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	var req dto.CreateDeliveryOptionReq
	if !h.bind(c, &req) {
		return
	}
	resp, err := h.svc.CreateDeliveryOption(c.Request.Context(), userID, storeID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, resp)
}

// UpdateDeliveryOption godoc
// PATCH /v1/storefront/stores/:id/delivery-options/:option_id
func (h *Handler) UpdateDeliveryOption(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	storeID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	optionID, ok := h.pathUUID(c, "option_id")
	if !ok {
		return
	}
	var req dto.UpdateDeliveryOptionReq
	if !h.bind(c, &req) {
		return
	}
	resp, err := h.svc.UpdateDeliveryOption(c.Request.Context(), userID, storeID, optionID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// DeleteDeliveryOption godoc
// DELETE /v1/storefront/stores/:id/delivery-options/:option_id
func (h *Handler) DeleteDeliveryOption(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	storeID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	optionID, ok := h.pathUUID(c, "option_id")
	if !ok {
		return
	}
	if err := h.svc.DeleteDeliveryOption(c.Request.Context(), userID, storeID, optionID); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
