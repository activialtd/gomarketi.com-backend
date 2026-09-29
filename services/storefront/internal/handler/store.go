package handler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
)

// CreateStore godoc
// POST /v1/storefront/stores
func (h *Handler) CreateStore(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	var req dto.CreateStoreReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.CreateStore(c.Request.Context(), userID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// GetMyStore godoc
// GET /v1/storefront/stores/mine
func (h *Handler) GetMyStore(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	resp, err := h.svc.GetMyStore(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateStore godoc
// PATCH /v1/storefront/stores/:id
func (h *Handler) UpdateStore(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	storeID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	var req dto.UpdateStoreReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.UpdateStore(c.Request.Context(), userID, storeID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetStorePublic godoc
// GET /v1/storefront/public/stores/:slug — no auth required, for storefront rendering
func (h *Handler) GetStorePublic(c *gin.Context) {
	slug := c.Param("slug")
	resp, err := h.svc.GetStoreBySlug(c.Request.Context(), slug)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetStoreByDomain godoc
// GET /v1/storefront/public/stores/by-domain?domain=cobi.com — no auth required
func (h *Handler) GetStoreByDomain(c *gin.Context) {
	domain := c.Query("domain")
	if domain == "" {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "domain query parameter required"})
		return
	}
	resp, err := h.svc.GetStoreByDomain(c.Request.Context(), domain)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// SearchStores godoc
// GET /v1/storefront/public/stores/search?q=&category=&lat=&lng=&radius_km=&limit=
// No auth required — powers the consumer-app search/voice-search flow.
func (h *Handler) SearchStores(c *gin.Context) {
	req := dto.StoreSearchReq{Limit: 20}

	if q := c.Query("q"); q != "" {
		req.Q = &q
	}
	if category := c.Query("category"); category != "" {
		req.Category = &category
	}
	if lat, ok := queryFloat(c, "lat"); ok {
		req.Lat = &lat
	}
	if lng, ok := queryFloat(c, "lng"); ok {
		req.Lng = &lng
	}
	if radiusKm, ok := queryFloat(c, "radius_km"); ok {
		req.RadiusKm = &radiusKm
	}
	if limit, ok := queryFloat(c, "limit"); ok && limit > 0 {
		req.Limit = int(limit)
	}
	if offset, ok := queryFloat(c, "offset"); ok && offset > 0 {
		req.Offset = int(offset)
	}
	if marketID := c.Query("market_id"); marketID != "" {
		req.MarketID = &marketID
	}

	resp, err := h.svc.SearchStores(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func queryFloat(c *gin.Context, key string) (float64, bool) {
	raw := c.Query(key)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// ListMarkets godoc
// GET /v1/storefront/public/markets?state=&city= — no auth required.
// Populates the vendor-web "which market is your store in?" dropdown, and
// the consumer-app "Popular Markets" browse tab.
func (h *Handler) ListMarkets(c *gin.Context) {
	var req dto.MarketReq
	if state := c.Query("state"); state != "" {
		req.State = &state
	}
	if city := c.Query("city"); city != "" {
		req.City = &city
	}

	resp, err := h.svc.ListMarkets(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// LogView godoc
// POST /v1/storefront/public/log — fire-and-forget analytics pixel, no auth
func (h *Handler) LogView(c *gin.Context) {
	var req dto.LogViewReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Status(http.StatusNoContent)
		return
	}
	ip := c.ClientIP()
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(ip)))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		h.svc.LogView(ctx, req, hash)
	}()
	c.Status(http.StatusNoContent)
}

// GetStoreViews godoc
// GET /v1/storefront/stores/:id/views — authenticated, returns view counts
func (h *Handler) GetStoreViews(c *gin.Context) {
	_, ok := h.callerID(c)
	if !ok {
		return
	}
	storeID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	resp, err := h.svc.GetStoreViews(c.Request.Context(), storeID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// CheckSlugAvailable godoc
// GET /v1/storefront/slugs/check?slug=your-store
func (h *Handler) CheckSlugAvailable(c *gin.Context) {
	slug := c.Query("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "slug query parameter required"})
		return
	}

	resp, err := h.svc.CheckSlugAvailable(c.Request.Context(), slug)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
