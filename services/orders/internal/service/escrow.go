package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/sse"
	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
)

// AutoReleaseAfter is how long a dispatched order waits before its escrow
// releases on its own. Without it, a buyer who never taps "I've received
// this" would leave the vendor unpaid forever.
const AutoReleaseAfter = 7 * 24 * time.Hour

// escrowSweepInterval is how often the background sweep looks for orders past
// AutoReleaseAfter. Hourly is far finer than a 7-day window needs.
const escrowSweepInterval = time.Hour

// ConfirmDelivery is the buyer saying the order arrived. It releases the
// vendor's held credit, which is the moment that money becomes withdrawable.
//
// Gated by email rather than a buyer JWT, matching the public checkout trust
// model: the buyer proves who they are with the address the order was placed
// under.
func (s *OrdersService) ConfirmDelivery(ctx context.Context, orderID uuid.UUID, email string) (dto.OrderResp, error) {
	if strings.TrimSpace(email) == "" {
		return dto.OrderResp{}, apperrors.BadRequest("email is required")
	}

	var (
		storeID      uuid.UUID
		escrowStatus string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT store_id, escrow_status FROM orders
		WHERE id = $1 AND LOWER(customer_email) = LOWER($2)`,
		orderID, email,
	).Scan(&storeID, &escrowStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.OrderResp{}, apperrors.NotFound("order not found")
	}
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("lookup order: %w", err)
	}
	if escrowStatus == "reversed" {
		return dto.OrderResp{}, apperrors.BadRequest("this order was refunded and cannot be confirmed")
	}

	if escrowStatus == "held" {
		if err := s.releaseEscrow(ctx, orderID); err != nil {
			return dto.OrderResp{}, err
		}
	}

	// Confirming also completes fulfilment, whatever the vendor last set.
	var r orderRow
	err = s.db.QueryRowxContext(ctx, `
		UPDATE orders
		SET status = 'delivered',
		    delivered_at = COALESCE(delivered_at, NOW()),
		    delivery_confirmed_at = COALESCE(delivery_confirmed_at, NOW()),
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, store_id, customer_id, customer_name, customer_email,
		          status, total_kobo, delivery_address, delivery_fee_kobo,
		          delivery_option_title, escrow_status, hub_received_at, dispatched_at, delivered_at,
		          delivery_confirmed_at, dispute_status, dispute_reason, disputed_at,
		       created_at, updated_at`,
		orderID).StructScan(&r)
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("confirm delivery: %w", err)
	}

	go s.broker.Publish(storeID.String(), sse.Event{
		Type: "order_updated",
		Data: fmt.Sprintf(`{"order_id":%q,"status":"delivered","escrow_status":"released"}`, orderID),
	})
	go s.broker.Publish(storeID.String(), sse.Event{
		Type: "wallet_updated",
		Data: `{"reason":"escrow_released"}`,
	})

	o := rowToOrder(r)
	o.Items = s.loadItems(ctx, r.ID)
	return o, nil
}

// releaseEscrow flips the order and its pending sale credit together, so the
// ledger and the order can never disagree about whether a vendor has been
// paid.
func (s *OrdersService) releaseEscrow(ctx context.Context, orderID uuid.UUID) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		UPDATE orders SET escrow_status = 'released', updated_at = NOW()
		WHERE id = $1 AND escrow_status = 'held'`, orderID)
	if err != nil {
		return fmt.Errorf("release escrow: %w", err)
	}
	// Another release (the sweep, a second tap) got there first.
	if n, _ := res.RowsAffected(); n == 0 {
		return tx.Rollback()
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE wallet_transactions SET status = 'completed'
		WHERE order_id = $1 AND type = 'credit' AND status = 'pending'`, orderID,
	); err != nil {
		return fmt.Errorf("complete wallet credit: %w", err)
	}

	return tx.Commit()
}

// ReverseEscrow takes a held credit back — the order was cancelled or never
// arrived, so the vendor is not paid and the buyer is owed a refund. The
// refund itself is a Paystack operation and is not automated here.
func (s *OrdersService) ReverseEscrow(ctx context.Context, orderID uuid.UUID) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		UPDATE orders SET escrow_status = 'reversed', updated_at = NOW()
		WHERE id = $1 AND escrow_status = 'held'`, orderID)
	if err != nil {
		return fmt.Errorf("reverse escrow: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Already released or already reversed — released money is not
		// clawed back automatically, that is a manual decision.
		return tx.Rollback()
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE wallet_transactions SET status = 'failed'
		WHERE order_id = $1 AND type = 'credit' AND status = 'pending'`, orderID,
	); err != nil {
		return fmt.Errorf("fail wallet credit: %w", err)
	}

	return tx.Commit()
}

// StartEscrowReleaser runs the auto-release sweep until ctx is cancelled.
// Called once at startup. Safe to run on several instances: each release is
// guarded by `escrow_status = 'held'`, so the first one wins and the rest
// are no-ops.
func (s *OrdersService) StartEscrowReleaser(ctx context.Context) {
	ticker := time.NewTicker(escrowSweepInterval)
	defer ticker.Stop()

	// Sweep once at startup so a restart doesn't delay overdue releases by a
	// whole interval.
	s.sweepEscrow(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepEscrow(ctx)
		}
	}
}

func (s *OrdersService) sweepEscrow(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM orders
		WHERE escrow_status = 'held'
		  AND dispute_status IS DISTINCT FROM 'reported'
		  AND dispatched_at IS NOT NULL
		  AND dispatched_at < NOW() - $1::interval
		LIMIT 500`,
		fmt.Sprintf("%d hours", int(AutoReleaseAfter.Hours())),
	)
	if err != nil {
		s.log.Warn().Err(err).Msg("escrow sweep: query failed")
		return
	}
	defer rows.Close()

	var due []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			s.log.Warn().Err(err).Msg("escrow sweep: scan failed")
			return
		}
		due = append(due, id)
	}
	if len(due) == 0 {
		return
	}

	released := 0
	for _, id := range due {
		if err := s.releaseEscrow(ctx, id); err != nil {
			s.log.Warn().Err(err).Str("order_id", id.String()).Msg("escrow sweep: release failed")
			continue
		}
		released++
	}
	s.log.Info().
		Int("released", released).
		Dur("after", AutoReleaseAfter).
		Msg("escrow sweep: auto-released dispatched orders")
}

// ReportMissing is the buyer saying a dispatched order never reached them.
//
// It does not change the order's status — the vendor did dispatch, and
// fulfilment tracking should still say so. What it changes is the money: a
// reported order is skipped by the auto-release sweep, so escrow stays held
// until someone resolves the claim. Resolution and the refund itself are
// manual.
func (s *OrdersService) ReportMissing(ctx context.Context, orderID uuid.UUID, email, reason string) (dto.OrderResp, error) {
	if strings.TrimSpace(email) == "" {
		return dto.OrderResp{}, apperrors.BadRequest("email is required")
	}

	var r orderRow
	err := s.db.QueryRowxContext(ctx, `
		UPDATE orders
		SET dispute_status = 'reported',
		    dispute_reason = NULLIF($3, ''),
		    disputed_at    = COALESCE(disputed_at, NOW()),
		    updated_at     = NOW()
		WHERE id = $1 AND LOWER(customer_email) = LOWER($2)
		  -- Already-resolved claims are not reopened by tapping again.
		  AND (dispute_status IS NULL OR dispute_status = 'reported')
		RETURNING id, store_id, customer_id, customer_name, customer_email,
		          status, total_kobo, delivery_address, delivery_fee_kobo,
		          delivery_option_title, escrow_status, hub_received_at, dispatched_at,
		          delivered_at, delivery_confirmed_at, dispute_status, dispute_reason,
		          disputed_at, created_at, updated_at`,
		orderID, email, strings.TrimSpace(reason)).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.OrderResp{}, apperrors.NotFound("order not found")
	}
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("report missing: %w", err)
	}

	// The vendor should see the claim on their dashboard immediately.
	go s.broker.Publish(r.StoreID.String(), sse.Event{
		Type: "order_updated",
		Data: fmt.Sprintf(`{"order_id":%q,"dispute_status":"reported"}`, orderID),
	})

	o := rowToOrder(r)
	o.Items = s.loadItems(ctx, r.ID)
	return o, nil
}
