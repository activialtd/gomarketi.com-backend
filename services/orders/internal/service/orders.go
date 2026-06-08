// Package service implements orders, CRM, and analytics use-cases.
// All methods are stubs pending database layer wiring.
package service

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// OrdersService implements orders, CRM, and analytics use-cases.
type OrdersService struct {
	log zerolog.Logger
}

// New creates an OrdersService.
func New(log zerolog.Logger) *OrdersService {
	return &OrdersService{log: log}
}

var errNotImplemented = apperrors.Wrap(http.StatusNotImplemented, "not yet implemented", nil)

// ── Orders ────────────────────────────────────────────────────────────────────

// ListOrders returns a paginated, filterable list of orders for a store.
func (s *OrdersService) ListOrders(_ context.Context, storeID uuid.UUID, page, perPage int, status *string, search *string) (dto.OrderListResp, error) {
	return dto.OrderListResp{}, errNotImplemented
}

// GetOrder returns a single order by ID, scoped to the store.
func (s *OrdersService) GetOrder(_ context.Context, storeID uuid.UUID, orderID uuid.UUID) (dto.OrderResp, error) {
	return dto.OrderResp{}, errNotImplemented
}

// UpdateOrderStatus advances or cancels an order.
func (s *OrdersService) UpdateOrderStatus(_ context.Context, storeID uuid.UUID, orderID uuid.UUID, req dto.UpdateOrderStatusReq) (dto.OrderResp, error) {
	return dto.OrderResp{}, errNotImplemented
}

// ListAbandonedCarts returns carts that were not converted to orders.
func (s *OrdersService) ListAbandonedCarts(_ context.Context, storeID uuid.UUID, page, perPage int) ([]dto.AbandonedCartResp, error) {
	return nil, errNotImplemented
}

// ── Customers (CRM) ───────────────────────────────────────────────────────────

// ListCustomers returns a paginated list of buyers who have ordered from the store.
func (s *OrdersService) ListCustomers(_ context.Context, storeID uuid.UUID, page, perPage int, search *string) (dto.CustomerListResp, error) {
	return dto.CustomerListResp{}, errNotImplemented
}

// GetCustomer returns a single customer's profile and order history summary.
func (s *OrdersService) GetCustomer(_ context.Context, storeID uuid.UUID, customerID uuid.UUID) (dto.CustomerResp, error) {
	return dto.CustomerResp{}, errNotImplemented
}

// ── Analytics ─────────────────────────────────────────────────────────────────

// GetAnalyticsOverview returns dashboard-level KPIs for the vendor's store.
func (s *OrdersService) GetAnalyticsOverview(_ context.Context, storeID uuid.UUID) (dto.AnalyticsOverviewResp, error) {
	return dto.AnalyticsOverviewResp{}, errNotImplemented
}
