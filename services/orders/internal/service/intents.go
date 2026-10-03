package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// intentSweepInterval is how often orphaned paid checkouts are retried.
const intentSweepInterval = 10 * time.Minute

// intentGracePeriod is how long to leave an intent alone before the sweep
// touches it. The browser usually saves the order within a second or two of
// the charge; anything newer than this is still in flight, and racing it
// would just make two attempts at the same reference.
const intentGracePeriod = 3 * time.Minute

// intentMaxAttempts stops a permanently broken payload (a deleted product, a
// store that no longer exists) being retried forever. It stays in the table
// with its last error for someone to look at.
const intentMaxAttempts = 8

// RecordCheckoutIntent stores what the buyer is about to pay for, keyed by
// the payment reference they will pay against.
//
// Deliberately unauthenticated, like the rest of checkout: the row is a plan,
// not money. It becomes an order only when a Paystack charge for that same
// reference is verified, so the worst a forged intent can do is sit unused.
// Re-recording the same reference overwrites the payload, which is what a
// buyer editing their address and paying again should do.
func (s *OrdersService) RecordCheckoutIntent(ctx context.Context, kind string, paymentRef string, payload any) error {
	if paymentRef == "" {
		return apperrors.BadRequest("payment_reference is required")
	}
	if kind != "order" && kind != "checkout" {
		return apperrors.BadRequest("kind must be 'order' or 'checkout'")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode intent payload: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO checkout_intents (payment_reference, kind, payload)
		VALUES ($1, $2, $3)
		ON CONFLICT (payment_reference) DO UPDATE
		SET kind = EXCLUDED.kind, payload = EXCLUDED.payload, updated_at = NOW()
		WHERE checkout_intents.fulfilled_at IS NULL`,
		paymentRef, kind, raw)
	if err != nil {
		return fmt.Errorf("record checkout intent: %w", err)
	}
	return nil
}

// MarkIntentFulfilled is called on the happy path, when the browser saved the
// order itself. Nothing breaks if it is missed — the sweep would find the
// intent, replay it, and CreateOrder's own idempotency would return the
// existing order — but marking it keeps the sweep's working set honest.
func (s *OrdersService) MarkIntentFulfilled(ctx context.Context, paymentRef string) {
	if paymentRef == "" {
		return
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE checkout_intents SET fulfilled_at = NOW(), updated_at = NOW()
		 WHERE payment_reference = $1 AND fulfilled_at IS NULL`, paymentRef); err != nil {
		s.log.Warn().Err(err).Str("reference", paymentRef).Msg("could not mark checkout intent fulfilled")
	}
}

// FulfilIntent replays one stored intent, creating the order(s) the buyer
// already paid for. Safe to call more than once for the same reference:
// checkout_payments claims the reference inside the same transaction as the
// order writes, so a second run returns the orders the first one made rather
// than charging or creating anything twice.
func (s *OrdersService) FulfilIntent(ctx context.Context, paymentRef string) error {
	var kind string
	var raw []byte
	var fulfilled sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT kind, payload, fulfilled_at FROM checkout_intents WHERE payment_reference = $1`,
		paymentRef).Scan(&kind, &raw, &fulfilled)
	if errors.Is(err, sql.ErrNoRows) {
		// No intent: either this payment predates intents, or it is not one
		// of ours (a DVA deposit, say). Nothing to recover from here.
		return nil
	}
	if err != nil {
		return fmt.Errorf("load checkout intent: %w", err)
	}
	if fulfilled.Valid {
		return nil
	}

	switch kind {
	case "order":
		var req dto.CreateOrderReq
		if err := json.Unmarshal(raw, &req); err != nil {
			return s.failIntent(ctx, paymentRef, fmt.Errorf("decode order payload: %w", err))
		}
		req.PaymentRef = paymentRef
		if _, err := s.CreateOrder(ctx, req); err != nil {
			return s.failIntent(ctx, paymentRef, err)
		}
	case "checkout":
		var req dto.CreateCheckoutReq
		if err := json.Unmarshal(raw, &req); err != nil {
			return s.failIntent(ctx, paymentRef, fmt.Errorf("decode checkout payload: %w", err))
		}
		req.PaymentRef = paymentRef
		if _, err := s.CreateCheckout(ctx, req); err != nil {
			return s.failIntent(ctx, paymentRef, err)
		}
	default:
		return s.failIntent(ctx, paymentRef, fmt.Errorf("unknown intent kind %q", kind))
	}

	s.MarkIntentFulfilled(ctx, paymentRef)
	s.log.Info().Str("reference", paymentRef).Str("kind", kind).
		Msg("recovered a paid checkout the buyer's browser never saved")
	return nil
}

func (s *OrdersService) failIntent(ctx context.Context, paymentRef string, cause error) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE checkout_intents SET attempts = attempts + 1, last_error = $2, updated_at = NOW()
		 WHERE payment_reference = $1`, paymentRef, cause.Error()); err != nil {
		s.log.Warn().Err(err).Str("reference", paymentRef).Msg("could not record intent failure")
	}
	return cause
}

// StartIntentSweepLoop runs until ctx is cancelled, finishing checkouts that
// were paid for but never saved.
//
// The webhook is the fast path and this is the safety net: webhooks get
// missed — a deploy mid-delivery, a signature mismatch, Paystack giving up
// after its retries — and a buyer whose money has gone must not depend on one
// HTTP call from a third party arriving.
func (s *OrdersService) StartIntentSweepLoop(ctx context.Context) {
	ticker := time.NewTicker(intentSweepInterval)
	defer ticker.Stop()
	s.sweepUnfulfilledIntents(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepUnfulfilledIntents(ctx)
		}
	}
}

func (s *OrdersService) sweepUnfulfilledIntents(ctx context.Context) {
	rows, err := s.db.QueryxContext(ctx, `
		SELECT payment_reference FROM checkout_intents
		WHERE fulfilled_at IS NULL
		  AND created_at < NOW() - $1::interval
		  AND attempts < $2
		ORDER BY created_at
		LIMIT 100`,
		fmt.Sprintf("%d seconds", int(intentGracePeriod.Seconds())), intentMaxAttempts)
	if err != nil {
		s.log.Warn().Err(err).Msg("checkout intent sweep: query failed")
		middleware.RecordBackgroundError(s.db, s.log, "orders", "checkout intent sweep: query failed: "+err.Error(), nil)
		return
	}
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err == nil {
			refs = append(refs, ref)
		}
	}
	rows.Close()
	if len(refs) == 0 {
		return
	}

	s.log.Info().Int("count", len(refs)).Msg("checkout intent sweep: retrying unsaved checkouts")
	for _, ref := range refs {
		// FulfilIntent verifies the charge with Paystack before creating
		// anything, so an abandoned checkout that was never paid simply
		// fails here and is retried until it ages out of attempts.
		if err := s.FulfilIntent(ctx, ref); err != nil {
			s.log.Warn().Err(err).Str("reference", ref).Msg("checkout intent sweep: still could not save this order")
		}
	}
}
