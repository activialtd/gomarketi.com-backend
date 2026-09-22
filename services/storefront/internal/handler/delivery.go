package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/service"
)

// ── Markets ───────────────────────────────────────────────────────────────────

// ListMarkets godoc
// GET /v1/storefront/public/markets?state=Lagos&city=Ikeja — no auth required.
// Populates the market dropdown in store setup and the consumer-app browse tab.
func (h *Handler) ListMarkets(c *gin.Context) {
	markets, err := h.svc.ListMarkets(c.Request.Context(), c.Query("state"), c.Query("city"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, markets)
}

// SearchStores godoc
// GET /v1/storefront/public/stores/search — no auth required.
// lat, lng and radius_km are accepted for client compatibility but ignored:
// distance ranking is not implemented yet.
func (h *Handler) SearchStores(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))

	resp, err := h.svc.SearchStores(c.Request.Context(), service.StoreSearchParams{
		Query:    c.Query("q"),
		Category: c.Query("category"),
		MarketID: c.Query("market_id"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

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
	opts := store.DeliveryOptions
	if opts == nil {
		opts = []dto.DeliveryOptionResp{}
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
