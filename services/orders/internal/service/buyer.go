package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
)

// Buyer-facing order history.
//
// Orders are not linked to a user account: checkout is public, and
// customer_id is derived from store+email (see customerUUID) so repeat
// buyers collapse into one CRM record per store. The durable identity across
// every store is therefore the buyer's email, which is how a signed-in buyer
// finds the orders they placed — including any placed before they had an
// account, as long as they used the same address.

// buyerEmail resolves the signed-in user's email from the shared users table.
func (s *OrdersService) buyerEmail(ctx context.Context, userID uuid.UUID) (string, error) {
	var email string
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(email, '') FROM users WHERE id = $1`, userID).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", apperrors.NotFound("user not found")
	}
	if err != nil {
		return "", fmt.Errorf("lookup buyer email: %w", err)
	}
	if email == "" {
		// An account with no email (phone-only signup) has no way to match
		// orders, rather than matching everything.
		return "", apperrors.BadRequest("your account has no email address, so orders cannot be matched to it")
	}
	return email, nil
}

// ListMyOrders returns every order the signed-in buyer has placed, newest
// first, across all vendors.
func (s *OrdersService) ListMyOrders(ctx context.Context, userID uuid.UUID, page, perPage int) (dto.OrderListResp, error) {
	email, err := s.buyerEmail(ctx, userID)
	if err != nil {
		return dto.OrderListResp{}, err
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	offset := (page - 1) * perPage

	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orders WHERE LOWER(customer_email) = LOWER($1)`, email,
	).Scan(&total); err != nil {
		return dto.OrderListResp{}, fmt.Errorf("count buyer orders: %w", err)
	}

	rows, err := s.db.QueryxContext(ctx, `
		SELECT id, store_id, customer_id, customer_name, customer_email,
		       status, total_kobo, delivery_address, delivery_fee_kobo, delivery_option_title,
		       escrow_status, hub_received_at, dispatched_at, delivered_at,
		       delivery_confirmed_at, dispute_status, dispute_reason, disputed_at,
		       created_at, updated_at
		FROM orders
		WHERE LOWER(customer_email) = LOWER($1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`, email, perPage, offset)
	if err != nil {
		return dto.OrderListResp{}, fmt.Errorf("list buyer orders: %w", err)
	}
	defer rows.Close()

	orders := make([]dto.OrderResp, 0)
	for rows.Next() {
		var r orderRow
		if err := rows.StructScan(&r); err != nil {
			return dto.OrderListResp{}, fmt.Errorf("scan buyer order: %w", err)
		}
		o := rowToOrder(r)
		o.Items = s.loadItems(ctx, r.ID)
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return dto.OrderListResp{}, fmt.Errorf("scan buyer orders: %w", err)
	}

	return dto.OrderListResp{Orders: orders, Total: total, Page: page, PerPage: perPage}, nil
}

// GetMyOrder returns one of the buyer's own orders. Scoping the lookup to
// their email means another buyer's order id reads as not found rather than
// leaking whether it exists.
func (s *OrdersService) GetMyOrder(ctx context.Context, userID, orderID uuid.UUID) (dto.OrderResp, error) {
	email, err := s.buyerEmail(ctx, userID)
	if err != nil {
		return dto.OrderResp{}, err
	}

	var r orderRow
	err = s.db.QueryRowxContext(ctx, `
		SELECT id, store_id, customer_id, customer_name, customer_email,
		       status, total_kobo, delivery_address, delivery_fee_kobo, delivery_option_title,
		       escrow_status, hub_received_at, dispatched_at, delivered_at,
		       delivery_confirmed_at, dispute_status, dispute_reason, disputed_at,
		       created_at, updated_at
		FROM orders
		WHERE id = $1 AND LOWER(customer_email) = LOWER($2)`, orderID, email).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.OrderResp{}, apperrors.NotFound("order not found")
	}
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("get buyer order: %w", err)
	}

	o := rowToOrder(r)
	o.Items = s.loadItems(ctx, r.ID)
	return o, nil
}
