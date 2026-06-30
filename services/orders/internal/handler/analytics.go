package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
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
