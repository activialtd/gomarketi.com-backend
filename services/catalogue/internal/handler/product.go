package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/services/catalogue/internal/dto"
)

// ListPublicProductsByQuery godoc
// GET /v1/catalogue/public/products?store_id=&page=1&per_page=24&category_id=&q=
// Storefront-facing: store_id comes from a query param instead of the path.
func (h *Handler) ListPublicProductsByQuery(c *gin.Context) {
	storeID, err := uuid.Parse(c.Query("store_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "store_id query param is required"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "24"))

	var catID *string
	if v := c.Query("category_id"); v != "" {
		catID = &v
	}
	var q *string
	if v := c.Query("q"); v != "" {
		q = &v
	}

	resp, err := h.svc.ListProducts(c.Request.Context(), storeID, page, perPage, catID, q, true)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// SearchPublicProducts godoc
// GET /v1/catalogue/public/products/search?q=&store_ids=id1,id2&page=1&per_page=24
// No auth. Cross-vendor search — store_ids is required (comma-separated);
// resolving which stores are eligible (named vendor, named market, or
// nearest-N by distance) happens client-side via the storefront service
// before calling this. An empty store_ids returns an empty page rather
// than scanning every store on the platform.
func (h *Handler) SearchPublicProducts(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "24"))

	var storeIDs []string
	if v := c.Query("store_ids"); v != "" {
		for _, id := range strings.Split(v, ",") {
			if id = strings.TrimSpace(id); id != "" {
				storeIDs = append(storeIDs, id)
			}
		}
	}

	resp, err := h.svc.SearchProducts(c.Request.Context(), storeIDs, c.Query("q"), page, perPage)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// SearchCanonicalProducts godoc
// GET /v1/catalogue/canonical-products/search?q=
// Authenticated (any vendor) — powers the create-product typeahead so a
// vendor can link their listing to an existing canonical product instead
// of minting a duplicate.
func (h *Handler) SearchCanonicalProducts(c *gin.Context) {
	if _, ok := h.callerStoreID(c); !ok {
		return
	}
	resp, err := h.svc.SearchCanonicalProducts(c.Request.Context(), c.Query("q"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetPublicProductByID godoc
// GET /v1/catalogue/public/products/:product_id — no auth, published only, no store_id in path
func (h *Handler) GetPublicProductByID(c *gin.Context) {
	productID, err := uuid.Parse(c.Param("product_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "invalid product_id"})
		return
	}
	resp, err := h.svc.GetPublicProductByID(c.Request.Context(), productID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetPublicProduct godoc
// GET /v1/catalogue/public/stores/:store_id/products/:product_id — no auth, published only
func (h *Handler) GetPublicProduct(c *gin.Context) {
	storeID, err := uuid.Parse(c.Param("store_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "invalid store_id"})
		return
	}
	productID, err := uuid.Parse(c.Param("product_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "invalid product_id"})
		return
	}

	resp, err := h.svc.GetProduct(c.Request.Context(), storeID, productID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	// Public endpoint: only expose published products
	if !resp.IsPublished {
		c.JSON(http.StatusNotFound, dto.ErrorResp{Error: "product not found"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ListPublicProducts godoc
// GET /v1/catalogue/public/stores/:store_id/products — no auth, returns published products only
func (h *Handler) ListPublicProducts(c *gin.Context) {
	storeID, err := uuid.Parse(c.Param("store_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "invalid store_id"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

	var catID *string
	if v := c.Query("category_id"); v != "" {
		catID = &v
	}
	var q *string
	if v := c.Query("q"); v != "" {
		q = &v
	}

	resp, err := h.svc.ListProducts(c.Request.Context(), storeID, page, perPage, catID, q, true)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ListProducts godoc
// GET /v1/catalogue/products?page=1&per_page=20&category_id=...&q=...&published=true
func (h *Handler) ListProducts(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	var categoryID *string
	if v := c.Query("category_id"); v != "" {
		categoryID = &v
	}
	var q *string
	if v := c.Query("q"); v != "" {
		q = &v
	}
	publishedOnly := c.Query("published") == "true"

	resp, err := h.svc.ListProducts(c.Request.Context(), storeID, page, perPage, categoryID, q, publishedOnly)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// CreateProduct godoc
// POST /v1/catalogue/products
func (h *Handler) CreateProduct(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}

	var req dto.CreateProductReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.CreateProduct(c.Request.Context(), storeID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// GetProduct godoc
// GET /v1/catalogue/products/:id
func (h *Handler) GetProduct(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	productID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	resp, err := h.svc.GetProduct(c.Request.Context(), storeID, productID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateProduct godoc
// PATCH /v1/catalogue/products/:id
func (h *Handler) UpdateProduct(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	productID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	var req dto.UpdateProductReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.UpdateProduct(c.Request.Context(), storeID, productID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// DeleteProduct godoc
// DELETE /v1/catalogue/products/:id
func (h *Handler) DeleteProduct(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	productID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	if err := h.svc.DeleteProduct(c.Request.Context(), storeID, productID); err != nil {
		h.writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// PublishProduct godoc
// POST /v1/catalogue/products/:id/publish
func (h *Handler) PublishProduct(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	productID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	resp, err := h.svc.PublishProduct(c.Request.Context(), storeID, productID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// UnpublishProduct godoc
// POST /v1/catalogue/products/:id/unpublish
func (h *Handler) UnpublishProduct(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	productID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	resp, err := h.svc.UnpublishProduct(c.Request.Context(), storeID, productID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
