package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// Register mounts all identity routes onto r.
// All routes are protected by Envoy JWT gate (reads X-User-ID injected header).
func Register(r *gin.Engine, h *Handler, log zerolog.Logger, allowedOrigins []string) {
	// Health check — load balancer target group probe. Registered before any
	// middleware so it never depends on CORS/auth/recovery being healthy.
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.Use(
		middleware.Recovery(log),
		middleware.RequestID(),
		middleware.RequestLogger(log),
		middleware.CORS(allowedOrigins),
		middleware.UserContext(),
	)

	v1 := r.Group("/v1/identity")
	v1.Use(middleware.RequireUser())
	{
		// User profile
		v1.GET("/me", h.GetMe)
		v1.PATCH("/me", h.UpdateMe)

		// Buyer addresses
		me := v1.Group("/me/addresses")
		me.GET("", h.ListAddresses)
		me.POST("", h.AddAddress)
		me.PATCH("/:id", h.UpdateAddress)
		me.DELETE("/:id", h.DeleteAddress)
		me.POST("/:id/set-default", h.SetDefaultAddress)

		// Plans (authenticated but no store required)
		v1.GET("/plans", h.ListPlans)

		// Vendor onboarding
		onboard := v1.Group("/vendor/onboard")
		onboard.POST("", h.StartVendorOnboarding)
		onboard.PATCH("/business", h.UpdateVendorBusiness)
		onboard.POST("/kyc", h.SubmitVendorKYC)

		// Vendor profile, plan & banks
		vendor := v1.Group("/vendor")
		vendor.GET("/profile", h.GetVendorProfile)
		vendor.POST("/plan", h.SelectPlan)
		vendor.GET("/subscription", h.GetSubscription)
		vendor.POST("/banks", h.AddVendorBank)
		vendor.GET("/banks", h.ListVendorBanks)
		vendor.POST("/banks/:id/set-primary", h.SetPrimaryVendorBank)
		vendor.DELETE("/banks/:id", h.DeleteVendorBank)

		// Staff management (owner/manager only — role gate is enforced by the frontend)
		staff := v1.Group("/vendor/staff")
		staff.GET("", h.ListStaff)
		staff.POST("", h.CreateStaff)
		staff.PATCH("/:id", h.UpdateStaff)
		staff.DELETE("/:id", h.DeleteStaff)
	}
}
