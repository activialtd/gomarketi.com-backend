package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/service"
)

// PaystackWebhook godoc
// POST /v1/orders/public/webhooks/paystack
// Called directly by Paystack, not a user — authenticated by the
// X-Paystack-Signature header instead of a JWT. Needs the raw request body
// (not Gin's usual bound struct) since the signature covers the exact bytes
// Paystack sent, not a re-serialized version of them.
func (h *Handler) PaystackWebhook(c *gin.Context) {
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "could not read request body"})
		return
	}

	if !service.VerifyPaystackWebhookSignature(body, c.GetHeader("X-Paystack-Signature")) {
		c.JSON(http.StatusUnauthorized, dto.ErrorResp{Error: "invalid signature"})
		return
	}

	if err := h.svc.HandlePaystackWebhook(c.Request.Context(), body); err != nil {
		h.log.Error().Err(err).Msg("paystack webhook processing failed")
		// Still 200 — Paystack retries on non-2xx, and retrying a processing
		// error (vs. a rejected signature) won't fix it; log and move on.
	}

	c.JSON(http.StatusOK, gin.H{"received": true})
}
