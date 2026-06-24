package handler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
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
