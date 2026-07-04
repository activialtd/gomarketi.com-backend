package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// GET /v1/store/payment-gateways — authenticated merchant view (all gateways + enabled status)
func (h *Handler) ListPaymentGateways(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	gws, err := h.svc.ListPaymentGateways(c.Request.Context(), storeID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"gateways": gws})
}

// PUT /v1/store/payment-gateways/:gateway — toggle enabled/config
func (h *Handler) UpsertPaymentGateway(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	gateway := c.Param("gateway")
	if gateway == "" {
		h.writeError(c, apperrors.BadRequest("gateway name is required"))
		return
	}
	var req dto.UpsertPaymentGatewayReq
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, apperrors.BadRequest(err.Error()))
		return
	}
	resp, err := h.svc.UpsertPaymentGateway(c.Request.Context(), storeID, gateway, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GET /v1/orders/public/gateways/:store_id — no auth, called by storefront checkout
func (h *Handler) GetPublicGateways(c *gin.Context) {
	storeID, err := uuid.Parse(c.Param("store_id"))
	if err != nil {
		h.writeError(c, apperrors.BadRequest("invalid store_id"))
		return
	}
	gws, err := h.svc.GetPublicGateways(c.Request.Context(), storeID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"gateways": gws})
}
