package handler

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// requireInternalKey protects service-to-service routes that aren't part of
// the public gateway surface (see admin-api's batch-dispatch flow, the only
// caller of these today) — a shared-secret header, not a user JWT, and
// deliberately reached by direct service networking, not through the
// gateway. INTERNAL_API_KEY unset means the check is skipped with a loud
// warning per request, matching this codebase's existing dev-mode-friendly
// pattern for PAYSTACK_SECRET_KEY.
func requireInternalKey(log zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := os.Getenv("INTERNAL_API_KEY")
		if key == "" {
			log.Warn().Msg("INTERNAL_API_KEY not set — internal routes are unprotected (dev mode)")
			c.Next()
			return
		}
		if c.GetHeader("X-Internal-Key") != key {
			c.AbortWithStatusJSON(http.StatusUnauthorized, dto.ErrorResp{Error: "invalid internal key"})
			return
		}
		c.Next()
	}
}

// Register mounts all orders, CRM, and analytics routes onto r.
// All routes require an authenticated vendor with at least one store (injected by Envoy).
func Register(r *gin.Engine, h *Handler, log zerolog.Logger, allowedOrigins []string, db *sqlx.DB) {
	// Health check — load balancer target group probe. Registered before any
	// middleware so it never depends on CORS/auth/recovery being healthy.
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.Use(
		middleware.Recovery(log, db, "orders"),
		middleware.RequestID(),
		middleware.RequestLogger(log),
		middleware.CORS(allowedOrigins),
		middleware.UserContext(),
	)

	// Public — no auth.
	pub := r.Group("/v1/orders/public")
	pub.POST("", h.CreateOrder)
	pub.POST("/checkout", h.CreateCheckout)              // multi-store cart: one payment, one order per vendor
	pub.GET("/:id", h.GetPublicOrder)                    // customer order tracking — gated by email param
	pub.POST("/:id/confirm-delivery", h.ConfirmDelivery) // buyer confirms receipt — releases vendor escrow
	pub.POST("/:id/report-missing", h.ReportMissing)     // buyer flags a dispatched order as never received
	pub.POST("/visit", h.TrackVisit)                     // lightweight storefront page-view beacon
	pub.POST("/subscribe", h.Subscribe)                  // storefront newsletter opt-in
	pub.GET("/gateways/:store_id", h.GetPublicGateways)  // active payment gateways for checkout
	pub.POST("/cart-email", h.SendCartInvoice)           // pre-payment cart summary email
	pub.POST("/webhooks/paystack", h.PaystackWebhook)    // Paystack calls this directly — signature-verified, not JWT

	// Internal — service-to-service only, reached by direct networking (not
	// the public gateway), protected by a shared secret instead of a user JWT.
	internal := r.Group("/v1/orders/internal")
	internal.Use(requireInternalKey(log))
	internal.POST("/no-show-refund", h.NoShowRefund)
	internal.POST("/dispute-refund", h.DisputeRefund)

	v1 := r.Group("/v1")
	v1.Use(middleware.RequireUser())
	{
		// Real-time WebSocket stream — vendor dashboard subscribes once per session
		v1.GET("/orders/ws", h.WsEvents)

		// Buyer's own order history (consumer app) — RequireUser() only,
		// deliberately not under the vendor-scoped orders group below since
		// a buyer has no store (callerStoreID would always 403 them).
		v1.GET("/orders/mine", h.GetMyOrders)
		v1.GET("/orders/mine/:id", h.GetMyOrder)

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
