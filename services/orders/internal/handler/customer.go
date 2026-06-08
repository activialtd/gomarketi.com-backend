package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ListCustomers godoc
// GET /v1/crm/customers?page=1&per_page=20&search=...
func (h *Handler) ListCustomers(c *gin.Context) {
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

	var search *string
	if v := c.Query("search"); v != "" {
		search = &v
	}

	resp, err := h.svc.ListCustomers(c.Request.Context(), storeID, page, perPage, search)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetCustomer godoc
// GET /v1/crm/customers/:id
func (h *Handler) GetCustomer(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}
	customerID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	resp, err := h.svc.GetCustomer(c.Request.Context(), storeID, customerID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
