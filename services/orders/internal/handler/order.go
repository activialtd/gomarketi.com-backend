package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/email"
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

// GetPublicOrder godoc
// GET /v1/orders/public/:id?email=customer@example.com
// Returns an order for a customer to track their purchase.
// Gated by the customer's email address — no vendor auth required.
func (h *Handler) GetPublicOrder(c *gin.Context) {
	orderID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	email := c.Query("email")
	if email == "" {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "email query param is required"})
		return
	}

	resp, err := h.svc.GetPublicOrder(c.Request.Context(), orderID, email)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetMyOrders godoc
// GET /v1/orders/mine
// Every order placed by the authenticated buyer — the consumer app's real
// order history, scoped server-side by the caller's own email (see
// OrdersService.resolveUserEmail), not a client-supplied one.
func (h *Handler) GetMyOrders(c *gin.Context) {
	userID, ok := h.callerUserID(c)
	if !ok {
		return
	}

	orders, err := h.svc.GetMyOrders(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"orders": orders})
}

// GetMyOrder godoc
// GET /v1/orders/mine/:id
func (h *Handler) GetMyOrder(c *gin.Context) {
	userID, ok := h.callerUserID(c)
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

// ConfirmDelivery godoc
// POST /v1/orders/public/:id/confirm-delivery
// Buyer confirms their order has physically arrived — releases the vendor's
// held wallet credit. Gated by email match, same trust model as GetPublicOrder.
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
// POST /v1/orders/public/:id/report-missing — buyer flags one order within a
// batch as never having arrived, even though it was dispatched.
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

// CreateOrder godoc
// POST /v1/orders/public — no auth, called by the storefront checkout after
// a successful (simulated) Paystack charge.
func (h *Handler) CreateOrder(c *gin.Context) {
	var req dto.CreateOrderReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.CreateOrder(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// SendCartInvoice godoc
// POST /v1/orders/public/cart-email — no auth.
// Fires immediately when the customer clicks Pay (before Paystack opens).
// Sends a cart summary email so the customer has a record even if they abandon.
func (h *Handler) SendCartInvoice(c *gin.Context) {
	var req dto.SendCartEmailReq
	if !h.bind(c, &req) {
		return
	}

	items := make([]email.InvoiceItem, len(req.Items))
	for i, it := range req.Items {
		items[i] = email.InvoiceItem{
			Name:      it.Name,
			ImageURL:  it.ImageURL,
			Quantity:  int(it.Quantity),
			PriceKobo: it.PriceKobo,
		}
	}

	// Fire async — never block the checkout flow on email delivery.
	go func() {
		if err := email.SendCartSummary(
			context.Background(),
			req.Email,
			req.CustomerName,
			req.StoreSlug,
			req.StoreName,
			req.TotalKobo,
			items,
		); err != nil {
			h.log.Warn().Err(err).Str("email", req.Email).Msg("cart summary email failed")
		}
	}()

	c.JSON(http.StatusAccepted, gin.H{"ok": true})
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

// NoShowRefundReq is the body for POST /v1/orders/internal/no-show-refund.
type NoShowRefundReq struct {
	OrderID string `json:"order_id" validate:"required,uuid"`
}

// NoShowRefund godoc
// POST /v1/orders/internal/no-show-refund
// Called by admin-api's batch-dispatch action for any order whose vendor
// didn't deliver to the hub in time — cancels the order and refunds the
// buyer via Paystack. Protected by a shared-secret header, not a user JWT:
// this is the one internal service-to-service call in the hub/escrow
// feature (see NoShowRefund in the service layer for why).
func (h *Handler) NoShowRefund(c *gin.Context) {
	var req NoShowRefundReq
	if !h.bind(c, &req) {
		return
	}
	orderID, err := uuid.Parse(req.OrderID)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "invalid order_id"})
		return
	}

	resp, err := h.svc.NoShowRefund(c.Request.Context(), orderID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// DisputeRefundReq is the body for POST /v1/orders/internal/dispute-refund.
type DisputeRefundReq struct {
	OrderID string `json:"order_id" validate:"required,uuid"`
}

// DisputeRefund godoc
// POST /v1/orders/internal/dispute-refund
// Called by admin-api when an admin resolves a reported "buyer never
// received this" dispute by refunding them — the claw-back path for an
// order already past dispatch. Same internal shared-secret protection as
// NoShowRefund, same reason: this is the one step that needs orders
// service's own Paystack secret key.
func (h *Handler) DisputeRefund(c *gin.Context) {
	var req DisputeRefundReq
	if !h.bind(c, &req) {
		return
	}
	orderID, err := uuid.Parse(req.OrderID)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "invalid order_id"})
		return
	}

	resp, err := h.svc.DisputeRefund(c.Request.Context(), orderID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
