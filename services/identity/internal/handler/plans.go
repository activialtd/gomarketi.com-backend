package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

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

// ProvisionDVA godoc
// POST /v1/identity/internal/provision-dva
// Internal service-to-service only (X-Internal-Key) — called by storefront's
// CreateStore right after a store row exists, so the vendor's Dedicated
// Virtual Account is named after their store instead of their personal name.
func (h *Handler) ProvisionDVA(c *gin.Context) {
	var req dto.ProvisionDVAReq
	if !h.bind(c, &req) {
		return
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "invalid user_id"})
		return
	}

	if err := h.svc.ProvisionVendorDVA(c.Request.Context(), userID, req.StoreName); err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"provisioned": true})
}
