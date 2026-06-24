package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// Register mounts all storefront routes onto r.
// All routes require an authenticated vendor (X-User-ID header from Envoy).
func Register(r *gin.Engine, h *Handler, log zerolog.Logger, allowedOrigins []string) {
	r.Use(
		middleware.Recovery(log),
		middleware.RequestID(),
		middleware.RequestLogger(log),
		middleware.CORS(allowedOrigins),
		middleware.UserContext(),
	)

	// Public routes — no authentication required.
	pub := r.Group("/v1/storefront/public")
	{
		pub.GET("/stores/by-domain", h.GetStoreByDomain)
		pub.GET("/stores/:slug", h.GetStorePublic)
		pub.POST("/log", h.LogView)
	}

	v1 := r.Group("/v1/storefront")
	v1.Use(middleware.RequireUser())
	{
		// Slug availability — called live as the vendor types in StoreSetupForm
		v1.GET("/slugs/check", h.CheckSlugAvailable)

		// Store lifecycle
		stores := v1.Group("/stores")
		stores.POST("", h.CreateStore)
		stores.GET("/mine", h.GetMyStore)

		store := stores.Group("/:id")
		store.PATCH("", h.UpdateStore)
		store.GET("/views", h.GetStoreViews)

		// Staff management (MERCHANT.STAFF dashboard section)
		staff := store.Group("/staff")
		staff.GET("", h.ListStaff)
		staff.POST("", h.InviteStaff)
		staff.DELETE("/:staff_id", h.RemoveStaff)
	}
}
