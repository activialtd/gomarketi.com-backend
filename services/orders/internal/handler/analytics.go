package handler

import (
	"net/http"

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
