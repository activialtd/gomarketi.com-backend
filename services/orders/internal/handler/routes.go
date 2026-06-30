package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// Register mounts all orders, CRM, and analytics routes onto r.
// All routes require an authenticated vendor with at least one store (injected by Envoy).
func Register(r *gin.Engine, h *Handler, log zerolog.Logger, allowedOrigins []string) {
	r.Use(
		middleware.Recovery(log),
		middleware.RequestID(),
		middleware.RequestLogger(log),
		middleware.CORS(allowedOrigins),
		middleware.UserContext(),
	)

	// Public — no auth. Called directly from the storefront checkout.
	pub := r.Group("/v1/orders/public")
	pub.POST("", h.CreateOrder)

	v1 := r.Group("/v1")
	v1.Use(middleware.RequireUser())
	{
		// Orders (MERCHANT.ORDERS dashboard section)
		orders := v1.Group("/orders")
		orders.GET("", h.ListOrders)
		orders.GET("/abandoned", h.ListAbandonedCarts)
		orders.GET("/:id", h.GetOrder)
		orders.PATCH("/:id/status", h.UpdateOrderStatus)

		// Customers / CRM (MERCHANT.CUSTOMERS dashboard section)
		customers := v1.Group("/crm/customers")
		customers.GET("", h.ListCustomers)
		customers.GET("/:id", h.GetCustomer)

		// Analytics (MERCHANT.ANALYTICS dashboard section)
		analytics := v1.Group("/analytics")
		analytics.GET("/overview", h.GetAnalyticsOverview)
		analytics.GET("/top-products", h.GetTopProducts)

		// Wallet (MERCHANT.WALLET dashboard section)
		wallet := v1.Group("/wallet")
		wallet.GET("/balance", h.GetWallet)
		wallet.POST("/withdraw", h.Withdraw)
	}
}
