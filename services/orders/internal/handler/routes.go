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

	// Public — no auth.
	pub := r.Group("/v1/orders/public")
	pub.POST("", h.CreateOrder)
	pub.GET("/:id", h.GetPublicOrder)                   // customer order tracking — gated by email param
	pub.POST("/visit", h.TrackVisit)                    // lightweight storefront page-view beacon
	pub.POST("/subscribe", h.Subscribe)                 // storefront newsletter opt-in
	pub.GET("/gateways/:store_id", h.GetPublicGateways) // active payment gateways for checkout
	pub.POST("/cart-email", h.SendCartInvoice)          // pre-payment cart summary email

	v1 := r.Group("/v1")
	v1.Use(middleware.RequireUser())
	{
		// Real-time WebSocket stream — vendor dashboard subscribes once per session
		v1.GET("/orders/ws", h.WsEvents)

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

		// Newsletter subscribers
		subscribers := v1.Group("/crm/subscribers")
		subscribers.GET("", h.ListSubscribers)
		subscribers.DELETE("/:id", h.Unsubscribe)

		// Email campaigns
		campaigns := v1.Group("/campaigns")
		campaigns.GET("", h.ListCampaigns)
		campaigns.POST("", h.CreateCampaign)
		campaigns.POST("/:id/send", h.SendCampaign)

		// Analytics (MERCHANT.ANALYTICS dashboard section)
		analytics := v1.Group("/analytics")
		analytics.GET("/overview", h.GetAnalyticsOverview)
		analytics.GET("/top-products", h.GetTopProducts)
		analytics.GET("/revenue-trend", h.GetRevenueTrend)

		// Payment gateway settings
		gateways := v1.Group("/store/payment-gateways")
		gateways.GET("", h.ListPaymentGateways)
		gateways.PUT("/:gateway", h.UpsertPaymentGateway)

		// Wallet (MERCHANT.WALLET dashboard section)
		wallet := v1.Group("/wallet")
		wallet.GET("/balance", h.GetWallet)
		wallet.POST("/withdraw", h.Withdraw)
	}
}
