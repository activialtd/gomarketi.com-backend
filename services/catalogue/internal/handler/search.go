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
