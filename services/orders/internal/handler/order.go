package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// ListOrders godoc
// GET /v1/orders?page=1&per_page=20&status=pending&search=...
func (h *Handler) ListOrders(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	var status *string
	if v := c.Query("status"); v != "" {
		status = &v
	}
	var search *string
	if v := c.Query("search"); v != "" {
		search = &v
	}

	resp, err := h.svc.ListOrders(c.Request.Context(), storeID, page, perPage, status, search)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetOrder godoc
// GET /v1/orders/:id
func (h *Handler) GetOrder(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	orderID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	resp, err := h.svc.GetOrder(c.Request.Context(), storeID, orderID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateOrderStatus godoc
// PATCH /v1/orders/:id/status
func (h *Handler) UpdateOrderStatus(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	orderID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	var req dto.UpdateOrderStatusReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.UpdateOrderStatus(c.Request.Context(), storeID, orderID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ListAbandonedCarts godoc
// GET /v1/orders/abandoned?page=1&per_page=20
func (h *Handler) ListAbandonedCarts(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	carts, err := h.svc.ListAbandonedCarts(c.Request.Context(), storeID, page, perPage)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"carts": carts})
}
