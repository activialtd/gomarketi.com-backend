package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/dto"
)

// ListPlans godoc
// GET /v1/identity/plans
func (h *Handler) ListPlans(c *gin.Context) {
	plans, err := h.svc.ListPlans(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

// SelectPlan godoc
// POST /v1/identity/vendor/plan
func (h *Handler) SelectPlan(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	var req dto.SelectPlanReq
	if !h.bind(c, &req) {
		return
	}

	sub, err := h.svc.SelectPlan(c.Request.Context(), userID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, sub)
}

// GetSubscription godoc
// GET /v1/identity/vendor/subscription
func (h *Handler) GetSubscription(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	sub, err := h.svc.GetSubscription(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, sub)
}
