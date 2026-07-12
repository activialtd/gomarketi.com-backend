package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// Register mounts all catalogue routes onto r.
// All routes require an authenticated vendor with at least one store (injected by Envoy).
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

	// Public routes — no auth required
	pub := r.Group("/v1/catalogue/public")
	// Query-param routes used by the storefront API client
	pub.GET("/products", h.ListPublicProductsByQuery)
	pub.GET("/products/:product_id", h.GetPublicProductByID)
	pub.GET("/categories", h.ListPublicCategories)
	pub.GET("/collections", h.ListPublicCollections)
	// Legacy path-param routes (kept for compatibility)
	pub.GET("/stores/:store_id/products", h.ListPublicProducts)
	pub.GET("/stores/:store_id/products/:product_id", h.GetPublicProduct)

	v1 := r.Group("/v1/catalogue")
	v1.Use(middleware.RequireUser())
	{
		// Products (MERCHANT.PRODUCTS dashboard section)
		products := v1.Group("/products")
		products.GET("", h.ListProducts)
		products.POST("", h.CreateProduct)

		product := products.Group("/:id")
		product.GET("", h.GetProduct)
		product.PATCH("", h.UpdateProduct)
		product.DELETE("", h.DeleteProduct)
		product.POST("/publish", h.PublishProduct)
		product.POST("/unpublish", h.UnpublishProduct)

		// Categories (MERCHANT.CATEGORIES dashboard section)
		categories := v1.Group("/categories")
		categories.GET("", h.ListCategories)
		categories.POST("", h.CreateCategory)
		categories.PATCH("/:id", h.UpdateCategory)
		categories.DELETE("/:id", h.DeleteCategory)

		// Collections
		collections := v1.Group("/collections")
		collections.GET("", h.ListCollections)
		collections.POST("", h.CreateCollection)
		col := collections.Group("/:id")
		col.PATCH("", h.UpdateCollection)
		col.DELETE("", h.DeleteCollection)
		col.POST("/publish", h.PublishCollection)
		col.POST("/unpublish", h.UnpublishCollection)
	}
}
