package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/dto"
)

// StartVendorOnboarding godoc
// POST /v1/identity/vendor/onboard
func (h *Handler) StartVendorOnboarding(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	resp, err := h.svc.StartVendorOnboarding(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateVendorBusiness godoc
// PATCH /v1/identity/vendor/onboard/business
func (h *Handler) UpdateVendorBusiness(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	var req dto.VendorBusinessReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.UpdateVendorBusiness(c.Request.Context(), userID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// SubmitVendorKYC godoc
// POST /v1/identity/vendor/onboard/kyc
func (h *Handler) SubmitVendorKYC(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	var req dto.VendorKYCReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.SubmitVendorKYC(c.Request.Context(), userID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetVendorProfile godoc
// GET /v1/identity/vendor/profile
func (h *Handler) GetVendorProfile(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	resp, err := h.svc.GetVendorProfile(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// AddVendorBank godoc
// POST /v1/identity/vendor/banks
func (h *Handler) AddVendorBank(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	var req dto.VendorBankReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.AddVendorBank(c.Request.Context(), userID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// ListVendorBanks godoc
// GET /v1/identity/vendor/banks
func (h *Handler) ListVendorBanks(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	banks, err := h.svc.ListVendorBanks(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"banks": banks})
}

// SetPrimaryVendorBank godoc
// POST /v1/identity/vendor/banks/:id/set-primary
func (h *Handler) SetPrimaryVendorBank(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	bankID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	if err := h.svc.SetPrimaryVendorBank(c.Request.Context(), userID, bankID); err != nil {
		h.writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// DeleteVendorBank godoc
// DELETE /v1/identity/vendor/banks/:id
func (h *Handler) DeleteVendorBank(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	bankID, ok := h.pathUUID(c, "id")
	if !ok {
		return
	}

	if err := h.svc.DeleteVendorBank(c.Request.Context(), userID, bankID); err != nil {
		h.writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
