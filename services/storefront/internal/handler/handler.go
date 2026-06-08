// Package handler wires HTTP to service calls for the storefront service.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/service"
)

// Handler holds the service and validator.
type Handler struct {
	svc      *service.StorefrontService
	validate *validator.Validate
}

// New creates a Handler.
func New(svc *service.StorefrontService) *Handler {
	return &Handler{
		svc:      svc,
		validate: validator.New(),
	}
}

func (h *Handler) callerID(c *gin.Context) (uuid.UUID, bool) {
	raw := c.GetString(middleware.CtxKeyUserID)
	if raw == "" {
		c.JSON(http.StatusUnauthorized, dto.ErrorResp{Error: "authentication required"})
		return uuid.UUID{}, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResp{Error: "invalid user id"})
		return uuid.UUID{}, false
	}
	return id, true
}

func (h *Handler) pathUUID(c *gin.Context, param string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(param))
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "invalid " + param})
		return uuid.UUID{}, false
	}
	return id, true
}

func (h *Handler) bind(c *gin.Context, req interface{}) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "invalid request body"})
		return false
	}
	if err := h.validate.Struct(req); err != nil {
		var fields []dto.FieldError
		if valErrs, ok := err.(validator.ValidationErrors); ok {
			for _, ve := range valErrs {
				fields = append(fields, dto.FieldError{Field: ve.Field(), Message: ve.Tag()})
			}
		}
		c.JSON(http.StatusUnprocessableEntity, dto.ValidationErrorResp{
			Error:  "validation failed",
			Fields: fields,
		})
		return false
	}
	return true
}

func (h *Handler) writeError(c *gin.Context, err error) {
	var appErr *apperrors.AppError
	if apperrors.As(err, &appErr) {
		c.JSON(appErr.HTTPStatus(), dto.ErrorResp{Error: appErr.Message})
		return
	}
	c.JSON(http.StatusInternalServerError, dto.ErrorResp{Error: "internal server error"})
}
