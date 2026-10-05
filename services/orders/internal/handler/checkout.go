package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// PlaceCheckout godoc
// POST /v1/orders/public/checkout — no auth, called by the consumer app when
// a cart spans more than one vendor store. Creates one order per store,
// atomically, all sharing a payment reference — before payment. The app then
// charges that reference and calls confirm-payment.
func (h *Handler) PlaceCheckout(c *gin.Context) {
	var req dto.CreateCheckoutReq
	if !h.bind(c, &req) {
		return
	}

	orders, err := h.svc.PlaceCheckout(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.CreateCheckoutResp{Orders: orders})
}
