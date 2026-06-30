package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// GetWallet godoc
// GET /v1/wallet/balance
func (h *Handler) GetWallet(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}

	resp, err := h.svc.GetWallet(c.Request.Context(), storeID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// Withdraw godoc
// POST /v1/wallet/withdraw — simulates a Paystack transfer payout
func (h *Handler) Withdraw(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}

	var req dto.WithdrawReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.Withdraw(c.Request.Context(), storeID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
