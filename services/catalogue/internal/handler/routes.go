package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// Register mounts all catalogue routes onto r.
// All routes require an authenticated vendor with at least one store (injected by Envoy).
func Register(r *gin.Engine, h *Handler, log zerolog.Logger, allowedOrigins []string) {
	r.Use(
		middleware.Recovery(log),
		middleware.RequestID(),
		middleware.RequestLogger(log),
		middleware.CORS(allowedOrigins),
		middleware.UserContext(),
	)

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
	}
}
