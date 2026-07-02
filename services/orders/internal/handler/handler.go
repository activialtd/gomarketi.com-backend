// Package handler wires HTTP to service calls for the orders service.
package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/service"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/sse"
)

// Handler holds the service and validator.
type Handler struct {
	svc      *service.OrdersService
	validate *validator.Validate
	log      zerolog.Logger
	broker   *sse.Broker
}

// New creates a Handler.
func New(svc *service.OrdersService, log zerolog.Logger, broker *sse.Broker) *Handler {
	return &Handler{
		svc:      svc,
		validate: validator.New(),
		log:      log,
		broker:   broker,
	}
}

// callerStoreID reads the first store ID from the Envoy-injected X-Store-IDs header.
func (h *Handler) callerStoreID(c *gin.Context) (uuid.UUID, bool) {
	raw, ok := c.Get(middleware.CtxKeyStoreIDs)
	if !ok {
		c.JSON(http.StatusForbidden, dto.ErrorResp{Error: "no store associated with this account"})
		return uuid.UUID{}, false
	}
	ids, ok := raw.([]string)
	if !ok || len(ids) == 0 || strings.TrimSpace(ids[0]) == "" {
		c.JSON(http.StatusForbidden, dto.ErrorResp{Error: "no store associated with this account"})
		return uuid.UUID{}, false
	}
	storeID, err := uuid.Parse(ids[0])
	if err != nil {
		c.JSON(http.StatusForbidden, dto.ErrorResp{Error: "invalid store id"})
		return uuid.UUID{}, false
	}
	return storeID, true
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
	h.log.Error().Err(err).Str("path", c.FullPath()).Msg("unhandled orders error")
	c.JSON(http.StatusInternalServerError, dto.ErrorResp{Error: "internal server error"})
}
