// Package service implements catalogue use-cases.
// All methods are stubs pending database layer wiring.
package service

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/catalogue/internal/dto"
)

// CatalogueService implements all catalogue use-cases.
type CatalogueService struct {
	log zerolog.Logger
}

// New creates a CatalogueService.
func New(log zerolog.Logger) *CatalogueService {
	return &CatalogueService{log: log}
}

var errNotImplemented = apperrors.Wrap(http.StatusNotImplemented, "not yet implemented", nil)

// ── Products ──────────────────────────────────────────────────────────────────

// ListProducts returns a paginated, filterable list of products for a store.
func (s *CatalogueService) ListProducts(_ context.Context, storeID uuid.UUID, page, perPage int, categoryID *string, q *string, publishedOnly bool) (dto.ProductListResp, error) {
	return dto.ProductListResp{}, errNotImplemented
}

// CreateProduct creates a new product (unpublished by default).
func (s *CatalogueService) CreateProduct(_ context.Context, storeID uuid.UUID, req dto.CreateProductReq) (dto.ProductResp, error) {
	return dto.ProductResp{}, errNotImplemented
}

// GetProduct returns a single product by ID, scoped to the store.
func (s *CatalogueService) GetProduct(_ context.Context, storeID uuid.UUID, productID uuid.UUID) (dto.ProductResp, error) {
	return dto.ProductResp{}, errNotImplemented
}

// UpdateProduct applies a partial update to a product.
func (s *CatalogueService) UpdateProduct(_ context.Context, storeID uuid.UUID, productID uuid.UUID, req dto.UpdateProductReq) (dto.ProductResp, error) {
	return dto.ProductResp{}, errNotImplemented
}

// DeleteProduct permanently removes a product.
func (s *CatalogueService) DeleteProduct(_ context.Context, storeID uuid.UUID, productID uuid.UUID) error {
	return errNotImplemented
}

// PublishProduct makes the product visible on the storefront.
func (s *CatalogueService) PublishProduct(_ context.Context, storeID uuid.UUID, productID uuid.UUID) (dto.ProductResp, error) {
	return dto.ProductResp{}, errNotImplemented
}

// UnpublishProduct hides the product from the storefront without deleting it.
func (s *CatalogueService) UnpublishProduct(_ context.Context, storeID uuid.UUID, productID uuid.UUID) (dto.ProductResp, error) {
	return dto.ProductResp{}, errNotImplemented
}

// ── Categories ────────────────────────────────────────────────────────────────

// ListCategories returns all categories for a store.
func (s *CatalogueService) ListCategories(_ context.Context, storeID uuid.UUID) ([]dto.CategoryResp, error) {
	return nil, errNotImplemented
}

// CreateCategory creates a new category (optionally nested under a parent).
func (s *CatalogueService) CreateCategory(_ context.Context, storeID uuid.UUID, req dto.CategoryReq) (dto.CategoryResp, error) {
	return dto.CategoryResp{}, errNotImplemented
}

// UpdateCategory renames a category or changes its parent.
func (s *CatalogueService) UpdateCategory(_ context.Context, storeID uuid.UUID, categoryID uuid.UUID, req dto.CategoryReq) (dto.CategoryResp, error) {
	return dto.CategoryResp{}, errNotImplemented
}

// DeleteCategory removes a category. Products in this category are set to uncategorised.
func (s *CatalogueService) DeleteCategory(_ context.Context, storeID uuid.UUID, categoryID uuid.UUID) error {
	return errNotImplemented
}
