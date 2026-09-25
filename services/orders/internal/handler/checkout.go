package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// CreateCheckout godoc
// POST /v1/orders/public/checkout — no auth required.
//
// The consumer app's basket, which may span several vendors, paid for once.
// Creates one order per vendor and charges delivery a single time for the
// whole checkout. Retrying with the same payment_reference returns the orders
// already created rather than charging the cart twice.
func (h *Handler) CreateCheckout(c *gin.Context) {
	var req dto.CreateCheckoutReq
	if !h.bind(c, &req) {
		return
	}
	resp, err := h.svc.CreateCheckout(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, resp)
}

// ConfirmDelivery godoc
// POST /v1/orders/public/:id/confirm-delivery — no auth, email-gated.
//
// The buyer saying the order arrived. This is what releases the vendor's
// held credit into their withdrawable balance.
func (h *Handler) ConfirmDelivery(c *gin.Context) {
	orderID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	var req dto.ConfirmDeliveryReq
	if !h.bind(c, &req) {
		return
	}
	resp, err := h.svc.ConfirmDelivery(c.Request.Context(), orderID, req.Email)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ReportMissing godoc
// POST /v1/orders/public/:id/report-missing — no auth, email-gated.
//
// The buyer flagging that a dispatched order never arrived. Freezes escrow
// so the vendor is not auto-paid while the claim is open.
func (h *Handler) ReportMissing(c *gin.Context) {
	orderID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	var req dto.ReportMissingReq
	if !h.bind(c, &req) {
		return
	}
	resp, err := h.svc.ReportMissing(c.Request.Context(), orderID, req.Email, req.Reason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
