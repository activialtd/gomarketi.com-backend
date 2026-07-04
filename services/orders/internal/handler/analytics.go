package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// GetAnalyticsOverview godoc
// GET /v1/analytics/overview
func (h *Handler) GetAnalyticsOverview(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}

	resp, err := h.svc.GetAnalyticsOverview(c.Request.Context(), storeID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// TrackVisit godoc
// POST /v1/orders/public/visit — lightweight page-view beacon from the storefront.
func (h *Handler) TrackVisit(c *gin.Context) {
	var req dto.TrackVisitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, apperrors.BadRequest(err.Error()))
		return
	}
	storeID, err := uuid.Parse(req.StoreID)
	if err != nil {
		h.writeError(c, apperrors.BadRequest("invalid store_id"))
		return
	}
	page := req.Page
	if page == "" {
		page = "/"
	}
	h.svc.TrackVisit(c.Request.Context(), storeID, req.SessionID, page)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GetRevenueTrend godoc
// GET /v1/analytics/revenue-trend?days=30
func (h *Handler) GetRevenueTrend(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	points, err := h.svc.GetRevenueTrend(c.Request.Context(), storeID, days)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"trend": points})
}

// GetTopProducts godoc
// GET /v1/analytics/top-products?limit=5
func (h *Handler) GetTopProducts(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "5"))

	resp, err := h.svc.GetTopProducts(c.Request.Context(), storeID, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"products": resp})
}
