package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/dto"
	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
)

// RequestOTP godoc
// POST /v1/auth/otp/request
func (h *Handler) RequestOTP(c *gin.Context) {
	var req dto.OTPRequestReq
	if !h.bind(c, &req) {
		return
	}

	resp, err := h.svc.RequestOTP(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// VerifyOTP godoc
// POST /v1/auth/otp/verify
func (h *Handler) VerifyOTP(c *gin.Context) {
	var req dto.OTPVerifyReq
	if !h.bind(c, &req) {
		return
	}

	req.UserAgent = c.GetHeader("User-Agent")
	req.IPAddress = c.ClientIP()

	resp, rawToken, err := h.svc.VerifyOTP(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}

	h.respondWithAuth(c, http.StatusOK, resp, rawToken)
}

// writeError maps an AppError to the correct HTTP status and JSON body.
func writeAppError(c *gin.Context, err error) {
	var appErr *apperrors.AppError
	if apperrors.As(err, &appErr) {
		c.JSON(appErr.HTTPStatus(), dto.ErrorResp{Error: appErr.Message})
		return
	}
	c.JSON(http.StatusInternalServerError, dto.ErrorResp{Error: "internal server error"})
}
