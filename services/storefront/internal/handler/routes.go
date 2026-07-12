package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// Register mounts all storefront routes onto r.
// All routes require an authenticated vendor (X-User-ID header from Envoy).
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

		// File uploads — presign a PUT URL for direct-to-R2 upload
		v1.POST("/uploads/presign", h.PresignUpload)

		// Store lifecycle
		stores := v1.Group("/stores")
		stores.POST("", h.CreateStore)
		stores.GET("/mine", h.GetMyStore)
		stores.POST("/upload", h.UploadStoreAsset)

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
