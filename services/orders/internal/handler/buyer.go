package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ListMyOrders godoc
// GET /v1/orders/mine?page=&per_page= — signed-in buyer.
//
// The buyer's own order history across every vendor. Matched on the email
// their account carries, since public checkout records no user id.
func (h *Handler) ListMyOrders(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

	resp, err := h.svc.ListMyOrders(c.Request.Context(), userID, page, perPage)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetMyOrder godoc
// GET /v1/orders/mine/:id — signed-in buyer.
func (h *Handler) GetMyOrder(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	orderID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	resp, err := h.svc.GetMyOrder(c.Request.Context(), userID, orderID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
