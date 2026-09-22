package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/services/catalogue/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/catalogue/internal/service"
)

// Search godoc
// GET /v1/catalogue/public/search — no auth required.
//
// The unified search: one query, products and vendors back together, plus
// related products and suggestions. `type` narrows it to one section
// ("products" or "vendors"); omit it for both.
//
//	?q=jollof ric&type=all&category_id=&store_ids=&limit=24&offset=0
func (h *Handler) Search(c *gin.Context) {
	params, ok := h.searchParams(c)
	if !ok {
		return
	}
	params.Include = strings.ToLower(c.DefaultQuery("type", "all"))
	switch params.Include {
	case "all", "products", "vendors":
	default:
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "type must be all, products or vendors"})
		return
	}

	resp, err := h.svc.Search(c.Request.Context(), params)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// SearchProducts godoc
// GET /v1/catalogue/public/products/search — no auth required.
//
// Product-only search in the paginated shape the consumer app already
// expects (page/per_page/total). store_ids is optional: without it the
// search covers every published product on the platform.
func (h *Handler) SearchProducts(c *gin.Context) {
	params, ok := h.searchParams(c)
	if !ok {
		return
	}
	params.Include = "products"

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "24"))
	if perPage < 1 || perPage > 50 {
		perPage = 24
	}
	params.Limit = perPage
	params.Offset = (page - 1) * perPage

	resp, err := h.svc.Search(c.Request.Context(), params)
	if err != nil {
		h.writeError(c, err)
		return
	}

	// total is what the caller needs to know whether to fetch another page.
	// Reporting an exact count would cost a second scan, so this reports the
	// end of the list once there is no more to fetch.
	total := int64(params.Offset + len(resp.Products))
	if resp.ProductsHasMore {
		total++
	}
	c.JSON(http.StatusOK, dto.ProductListResp{
		Products: resp.Products,
		Total:    total,
		Page:     page,
		PerPage:  perPage,
	})
}

// searchParams reads the query parameters shared by both search endpoints.
func (h *Handler) searchParams(c *gin.Context) (service.SearchParams, bool) {
	p := service.SearchParams{
		Query:      c.Query("q"),
		CategoryID: c.Query("category_id"),
	}

	if raw := c.Query("store_ids"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := uuid.Parse(part)
			if err != nil {
				c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "invalid store_ids"})
				return service.SearchParams{}, false
			}
			p.StoreIDs = append(p.StoreIDs, id)
		}
	}
	if v := c.Query("category_id"); v != "" {
		if _, err := uuid.Parse(v); err != nil {
			c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "invalid category_id"})
			return service.SearchParams{}, false
		}
	}

	p.Limit, _ = strconv.Atoi(c.Query("limit"))
	p.Offset, _ = strconv.Atoi(c.Query("offset"))
	return p, true
}

// SearchCanonicalProducts godoc
// GET /v1/catalogue/canonical-products/search?q= — vendor product form.
func (h *Handler) SearchCanonicalProducts(c *gin.Context) {
	resp, err := h.svc.SearchCanonicalProducts(c.Request.Context(), c.Query("q"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
