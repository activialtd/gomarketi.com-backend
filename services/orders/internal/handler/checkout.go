package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// CreateCheckout godoc
// POST /v1/orders/public/checkout — no auth, called by the consumer app
// when a cart spans more than one vendor store. One Paystack charge is
// verified once, then one order per store is created atomically.
func (h *Handler) CreateCheckout(c *gin.Context) {
	var req dto.CreateCheckoutReq
	if !h.bind(c, &req) {
		return
	}

	orders, err := h.svc.CreateCheckout(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.CreateCheckoutResp{Orders: orders})
}
