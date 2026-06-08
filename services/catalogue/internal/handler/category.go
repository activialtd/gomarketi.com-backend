package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/catalogue/internal/dto"
)

// ListCategories godoc
// GET /v1/catalogue/categories
func (h *Handler) ListCategories(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}

	cats, err := h.svc.ListCategories(c.Request.Context(), storeID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"categories": cats})
}

// CreateCategory godoc
// POST /v1/catalogue/categories
func (h *Handler) CreateCategory(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}

	var req dto.CategoryReq
	if !h.bind(c, &req) {
		return
	}

	cat, err := h.svc.CreateCategory(c.Request.Context(), storeID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, cat)
}

// UpdateCategory godoc
// PATCH /v1/catalogue/categories/:id
func (h *Handler) UpdateCategory(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	categoryID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	var req dto.CategoryReq
	if !h.bind(c, &req) {
		return
	}

	cat, err := h.svc.UpdateCategory(c.Request.Context(), storeID, categoryID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, cat)
}

// DeleteCategory godoc
// DELETE /v1/catalogue/categories/:id
func (h *Handler) DeleteCategory(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	categoryID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	if err := h.svc.DeleteCategory(c.Request.Context(), storeID, categoryID); err != nil {
		h.writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
