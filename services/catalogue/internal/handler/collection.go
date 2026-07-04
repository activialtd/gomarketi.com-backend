package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/services/catalogue/internal/dto"
)

// ListPublicCollections godoc
// GET /v1/catalogue/public/collections?store_id= — no auth
func (h *Handler) ListPublicCollections(c *gin.Context) {
	storeID, err := uuid.Parse(c.Query("store_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "store_id query param is required"})
		return
	}
	resp, err := h.svc.ListCollections(c.Request.Context(), storeID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GET /v1/catalogue/collections
func (h *Handler) ListCollections(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	resp, err := h.svc.ListCollections(c.Request.Context(), storeID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// POST /v1/catalogue/collections
func (h *Handler) CreateCollection(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	var req dto.CreateCollectionReq
	if !h.bind(c, &req) {
		return
	}
	col, err := h.svc.CreateCollection(c.Request.Context(), storeID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, col)
}

// PATCH /v1/catalogue/collections/:id
func (h *Handler) UpdateCollection(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	colID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	var req dto.UpdateCollectionReq
	if !h.bind(c, &req) {
		return
	}
	col, err := h.svc.UpdateCollection(c.Request.Context(), storeID, colID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, col)
}

// DELETE /v1/catalogue/collections/:id
func (h *Handler) DeleteCollection(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	colID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteCollection(c.Request.Context(), storeID, colID); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// POST /v1/catalogue/collections/:id/publish
func (h *Handler) PublishCollection(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	colID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	col, err := h.svc.PublishCollection(c.Request.Context(), storeID, colID, true)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, col)
}

// POST /v1/catalogue/collections/:id/unpublish
func (h *Handler) UnpublishCollection(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	colID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}
	col, err := h.svc.PublishCollection(c.Request.Context(), storeID, colID, false)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, col)
}
