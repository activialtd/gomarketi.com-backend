package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// POST /v1/orders/public/subscribe — no auth, called from storefront newsletter form.
func (h *Handler) Subscribe(c *gin.Context) {
	var req dto.SubscribeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, apperrors.BadRequest(err.Error()))
		return
	}
	storeID, err := uuid.Parse(req.StoreID)
	if err != nil {
		h.writeError(c, apperrors.BadRequest("invalid store_id"))
		return
	}
	if err := h.svc.Subscribe(c.Request.Context(), storeID, req); err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DELETE /v1/crm/subscribers/:id
func (h *Handler) Unsubscribe(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	subID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		h.writeError(c, apperrors.BadRequest("invalid subscriber id"))
		return
	}
	if err := h.svc.Unsubscribe(c.Request.Context(), storeID, subID); err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /v1/crm/subscribers
func (h *Handler) ListSubscribers(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))

	resp, err := h.svc.ListSubscribers(c.Request.Context(), storeID, page, perPage)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// POST /v1/campaigns
func (h *Handler) CreateCampaign(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	var req dto.CreateCampaignReq
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, apperrors.BadRequest(err.Error()))
		return
	}
	resp, err := h.svc.CreateCampaign(c.Request.Context(), storeID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, resp)
}

// GET /v1/campaigns
func (h *Handler) ListCampaigns(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	resp, err := h.svc.ListCampaigns(c.Request.Context(), storeID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// POST /v1/campaigns/:id/send
func (h *Handler) SendCampaign(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	campID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		h.writeError(c, apperrors.BadRequest("invalid campaign id"))
		return
	}
	resp, err := h.svc.SendCampaign(c.Request.Context(), storeID, campID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
