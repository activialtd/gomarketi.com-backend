package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// Register mounts all identity routes onto r.
// All routes are protected by Envoy JWT gate (reads X-User-ID injected header).
func Register(r *gin.Engine, h *Handler, log zerolog.Logger, allowedOrigins []string) {
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

		// Vendor onboarding
		onboard := v1.Group("/vendor/onboard")
		onboard.POST("", h.StartVendorOnboarding)
		onboard.PATCH("/business", h.UpdateVendorBusiness)
		onboard.POST("/kyc", h.SubmitVendorKYC)

		// Vendor profile & banks
		vendor := v1.Group("/vendor")
		vendor.GET("/profile", h.GetVendorProfile)
		vendor.POST("/banks", h.AddVendorBank)
		vendor.GET("/banks", h.ListVendorBanks)
		vendor.POST("/banks/:id/set-primary", h.SetPrimaryVendorBank)
		vendor.DELETE("/banks/:id", h.DeleteVendorBank)
	}
}
