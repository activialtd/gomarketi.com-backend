package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/sse"
	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// paystackWebhookEvent is the subset of Paystack's webhook payload this
// handler cares about. Only "charge.success" with channel "dedicated_nuban"
// represents a deposit into a vendor's Dedicated Virtual Account (identity
// service's CreateDedicatedAccount) — every other event type is ignored.
type paystackWebhookEvent struct {
	Event string `json:"event"`
	Data  struct {
		Reference string `json:"reference"`
		Amount    int64  `json:"amount"` // already kobo — Paystack's smallest-unit convention matches ours
		Channel   string `json:"channel"`
		Customer  struct {
			CustomerCode string `json:"customer_code"`
		} `json:"customer"`
	} `json:"data"`
}

// VerifyPaystackWebhookSignature checks the X-Paystack-Signature header —
// HMAC-SHA512 of the raw request body using the account's secret key. This
// is the ONLY thing standing between this endpoint and anyone on the
// internet crediting a vendor's wallet for free, since it's otherwise
// unauthenticated (Paystack calls it directly, not through a user session).
func VerifyPaystackWebhookSignature(body []byte, signature string) bool {
	secretKey := os.Getenv("PAYSTACK_SECRET_KEY")
	if secretKey == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha512.New, []byte(secretKey))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// HandlePaystackWebhook processes a verified webhook event. Deliberately
// narrow for now: the only event this credits is a real bank-transfer
// deposit into a vendor's own DVA — separate money from, and not subject to
// escrow like, an order-checkout credit (there's no delivery to hold it
// hostage against; the money is already unconditionally the vendor's).
// Idempotent via wallet_transactions' UNIQUE(reference) constraint: a
// Paystack webhook retry (it retries anything but a fast 200) hits the same
// reference and is treated as already-processed, not double-credited.
func (s *OrdersService) HandlePaystackWebhook(ctx context.Context, body []byte) error {
	var evt paystackWebhookEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		return fmt.Errorf("decode webhook payload: %w", err)
	}

	if evt.Event != "charge.success" {
		return nil
	}

	// A successful card charge that is not a DVA deposit is a buyer paying for
	// a checkout. Paystack has been telling us about every one of these all
	// along and we discarded them — and this is the signal that matters when
	// the buyer's own browser never came back. The order already exists, so
	// this only has to flip it to paid; in the normal case the browser got
	// there first and this is a no-op.
	if evt.Data.Channel != "dedicated_nuban" {
		if evt.Data.Reference == "" {
			return nil
		}
		if _, err := s.ConfirmPayment(ctx, evt.Data.Reference); err != nil {
			s.log.Warn().Err(err).Str("reference", evt.Data.Reference).
				Msg("charge.success webhook: could not confirm the paid order")
			// Swallowed deliberately: returning an error makes Paystack retry
			// the webhook, which rarely helps, and the failure is recorded for
			// the admin error queue either way.
			middleware.RecordBackgroundError(s.db, s.log, "orders",
				"charge.success webhook: could not confirm the paid order: "+err.Error(),
				map[string]any{"reference": evt.Data.Reference})
		}
		return nil
	}
	if evt.Data.Customer.CustomerCode == "" || evt.Data.Reference == "" || evt.Data.Amount <= 0 {
		s.log.Warn().Interface("event", evt).Msg("dva deposit webhook: missing required fields")
		return nil
	}

	var storeID, storeName string
	err := s.db.QueryRowContext(ctx, `
		SELECT st.id, st.name
		FROM vendor_profiles vp
		JOIN stores st ON st.vendor_id = vp.user_id
		WHERE vp.paystack_customer_code = $1
		LIMIT 1`,
		evt.Data.Customer.CustomerCode,
	).Scan(&storeID, &storeName)
	if errors.Is(err, sql.ErrNoRows) {
		s.log.Warn().Str("customer_code", evt.Data.Customer.CustomerCode).
			Msg("dva deposit webhook: no store matches this Paystack customer")
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve store for dva deposit: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO wallet_transactions (store_id, type, amount_kobo, description, reference, status)
		VALUES ($1, 'credit', $2, 'Direct bank transfer to your GoMarketi account', $3, 'completed')
		ON CONFLICT (reference) DO NOTHING`,
		storeID, evt.Data.Amount, evt.Data.Reference,
	)
	if err != nil {
		return fmt.Errorf("credit dva deposit: %w", err)
	}

	go s.broker.Publish(storeID, sse.Event{
		Type: "wallet_updated",
		Data: `{"reason":"dva_deposit"}`,
	})

	return nil
}
