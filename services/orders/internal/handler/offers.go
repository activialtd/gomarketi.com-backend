package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// CreateOffer godoc
// POST /v1/orders/public/offers — a buyer opens a negotiation. No auth: the
// response's buyer_token is the guest's key to it from then on.
func (h *Handler) CreateOffer(c *gin.Context) {
	var req dto.CreateOfferReq
	if !h.bind(c, &req) {
		return
	}
	resp, err := h.svc.CreateOffer(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, resp)
}

// GetPublicOffer godoc
// GET /v1/orders/public/offers/:id?token=...
func (h *Handler) GetPublicOffer(c *gin.Context) {
	offerID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	resp, err := h.svc.GetOfferForBuyer(c.Request.Context(), offerID, c.Query("token"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// RespondPublicOffer godoc
// POST /v1/orders/public/offers/:id/respond?token=... — a guest buyer's move.
func (h *Handler) RespondPublicOffer(c *gin.Context) {
	offerID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	var req dto.RespondOfferReq
	if !h.bind(c, &req) {
		return
	}
	resp, err := h.svc.RespondAsBuyer(c.Request.Context(), offerID, c.Query("token"), req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetMyOffers godoc
// GET /v1/orders/offers/mine — the signed-in buyer's negotiations.
func (h *Handler) GetMyOffers(c *gin.Context) {
	userID, ok := h.callerUserID(c)
	if !ok {
		return
	}
	resp, err := h.svc.ListMyOffers(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// RespondMyOffer godoc
// POST /v1/orders/offers/mine/:id/respond — a signed-in buyer's move.
func (h *Handler) RespondMyOffer(c *gin.Context) {
	userID, ok := h.callerUserID(c)
	if !ok {
		return
	}
	offerID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	var req dto.RespondOfferReq
	if !h.bind(c, &req) {
		return
	}
	resp, err := h.svc.RespondAsSignedInBuyer(c.Request.Context(), userID, offerID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ListOffers godoc
// GET /v1/orders/offers?status=pending|countered|accepted|closed|all&page=1&per_page=20
func (h *Handler) ListOffers(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	resp, err := h.svc.ListStoreOffers(c.Request.Context(), storeID, c.DefaultQuery("status", "all"), page, perPage)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// RespondOffer godoc
// POST /v1/orders/offers/:id/respond — the vendor accepts, counters or declines.
func (h *Handler) RespondOffer(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	offerID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	var req dto.RespondOfferReq
	if !h.bind(c, &req) {
		return
	}
	resp, err := h.svc.RespondAsVendor(c.Request.Context(), storeID, offerID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
