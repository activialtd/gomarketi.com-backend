package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/email"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/sse"
)

type OrdersService struct {
	db     *sqlx.DB
	log    zerolog.Logger
	broker *sse.Broker
}

// orderColumns is the shared SELECT list for every query that scans into
// orderRow — keeps the hub/escrow columns and the wallet_status subquery
// (escrow state — see EscrowStatus in rowToOrder) in exactly one place
// instead of duplicated across five near-identical queries.
const orderColumns = `id, store_id, customer_id, customer_name, customer_email, status,
	total_kobo, delivery_address, payment_reference,
	hub_received_at, dispatched_at, delivered_at, delivery_confirmed_at, cancelled_reason,
	created_at, updated_at,
	(SELECT wt.status FROM wallet_transactions wt WHERE wt.order_id = orders.id LIMIT 1) AS wallet_status`

func New(db *sqlx.DB, log zerolog.Logger, broker *sse.Broker) *OrdersService {
	return &OrdersService{db: db, log: log, broker: broker}
}

// ── Orders ────────────────────────────────────────────────────────────────────

func (s *OrdersService) ListOrders(ctx context.Context, storeID uuid.UUID, page, perPage int, status *string, q *string) (dto.OrderListResp, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	offset := (page - 1) * perPage

	base := `FROM orders WHERE store_id=$1`
	args := []any{storeID}
	i := 2

	if status != nil && *status != "" {
		base += fmt.Sprintf(` AND status=$%d`, i)
		args = append(args, *status)
		i++
	}
	if q != nil && *q != "" {
		base += fmt.Sprintf(` AND (customer_name ILIKE $%d OR customer_email ILIKE $%d)`, i, i)
		args = append(args, "%"+*q+"%")
		i++
	}

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) `+base, args...).Scan(&total); err != nil {
		return dto.OrderListResp{}, fmt.Errorf("count orders: %w", err)
	}

	listArgs := append(args, perPage, offset)
	rows, err := s.db.QueryxContext(ctx,
		`SELECT `+orderColumns+` `+
			base+fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, i, i+1),
		listArgs...)
	if err != nil {
		return dto.OrderListResp{}, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()

	orders := make([]dto.OrderResp, 0)
	for rows.Next() {
		var r orderRow
		if err := rows.StructScan(&r); err != nil {
			return dto.OrderListResp{}, err
		}
		o := rowToOrder(r)
		o.Items = s.loadItems(ctx, r.ID)
		orders = append(orders, o)
	}
	return dto.OrderListResp{Orders: orders, Total: total, Page: page, PerPage: perPage}, nil
}

// GetPublicOrder returns an order for a customer to track — gated by email match, no vendor auth needed.
func (s *OrdersService) GetPublicOrder(ctx context.Context, orderID uuid.UUID, email string) (dto.OrderResp, error) {
	var r orderRow
	err := s.db.QueryRowxContext(ctx, `
		SELECT `+orderColumns+`
		FROM orders WHERE id=$1 AND LOWER(customer_email)=LOWER($2)`, orderID, email).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.OrderResp{}, apperrors.NotFound("order not found")
	}
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("get public order: %w", err)
	}
	o := rowToOrder(r)
	o.Items = s.loadItems(ctx, r.ID)
	return o, nil
}

// resolveUserEmail looks up the authenticated caller's own email directly
// from the shared users table (owned by identity service, read here the
// same way every other cross-service lookup in this codebase works — no
// internal HTTP call). Used so GetMyOrders/GetMyOrder scope strictly to the
// caller's own email server-side, rather than trusting a client-supplied
// one like the public tracking endpoints do.
func (s *OrdersService) resolveUserEmail(ctx context.Context, userID uuid.UUID) (string, error) {
	var email sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT email FROM users WHERE id=$1`, userID).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) || !email.Valid || email.String == "" {
		return "", apperrors.NotFound("no email on file for this account")
	}
	if err != nil {
		return "", fmt.Errorf("resolve user email: %w", err)
	}
	return email.String, nil
}

// GetMyOrders returns every order placed by the authenticated buyer,
// grouped client-side by payment_reference into batches (a multi-vendor
// cart checkout) — mirrors admin-api's batch view, just scoped to one buyer.
func (s *OrdersService) GetMyOrders(ctx context.Context, userID uuid.UUID) ([]dto.OrderResp, error) {
	email, err := s.resolveUserEmail(ctx, userID)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryxContext(ctx, `
		SELECT `+orderColumns+`
		FROM orders WHERE LOWER(customer_email)=LOWER($1) ORDER BY created_at DESC`, email)
	if err != nil {
		return nil, fmt.Errorf("list my orders: %w", err)
	}
	defer rows.Close()

	orders := make([]dto.OrderResp, 0)
	for rows.Next() {
		var r orderRow
		if err := rows.StructScan(&r); err != nil {
			return nil, err
		}
		o := rowToOrder(r)
		o.Items = s.loadItems(ctx, r.ID)
		orders = append(orders, o)
	}
	return orders, nil
}

// GetMyOrder returns a single order, scoped to the authenticated buyer's own
// (server-resolved) email — the same ownership model as GetMyOrders.
func (s *OrdersService) GetMyOrder(ctx context.Context, userID uuid.UUID, orderID uuid.UUID) (dto.OrderResp, error) {
	email, err := s.resolveUserEmail(ctx, userID)
	if err != nil {
		return dto.OrderResp{}, err
	}
	return s.GetPublicOrder(ctx, orderID, email)
}

// ConfirmDelivery is called by the buyer once their (possibly partial —
// see batch dispatch) order has physically arrived, gated by email match —
// the same trust model GetPublicOrder already uses, since orders service
// has no real per-buyer JWT identity wired through checkout today. This is
// the trigger that releases the vendor's held wallet credit: it only
// succeeds from status='shipped' (GoMarketi has actually dispatched the
// order from the hub — see the admin batch-dispatch flow), so a buyer can
// never release funds for something that was never sent.
func (s *OrdersService) ConfirmDelivery(ctx context.Context, orderID uuid.UUID, email string) (dto.OrderResp, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT status FROM orders WHERE id=$1 AND LOWER(customer_email)=LOWER($2) FOR UPDATE`,
		orderID, email,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.OrderResp{}, apperrors.NotFound("order not found")
	}
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("lock order: %w", err)
	}
	if status != string(dto.OrderStatusShipped) {
		return dto.OrderResp{}, apperrors.BadRequest(
			"this order can't be marked received yet — it hasn't been dispatched")
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE orders SET status=$1, delivered_at=NOW(), delivery_confirmed_at=NOW(), updated_at=NOW()
		WHERE id=$2`,
		dto.OrderStatusDelivered, orderID,
	); err != nil {
		return dto.OrderResp{}, fmt.Errorf("mark delivered: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE wallet_transactions SET status='completed', released_at=NOW()
		WHERE order_id=$1 AND status='pending'`,
		orderID,
	); err != nil {
		return dto.OrderResp{}, fmt.Errorf("release escrow: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return dto.OrderResp{}, fmt.Errorf("commit: %w", err)
	}

	return s.GetPublicOrder(ctx, orderID, email)
}

// NoShowRefund cancels one order in a batch and refunds the buyer for that
// portion, because its vendor didn't deliver to the GoMarketi hub in time
// for dispatch. Called by admin-api's batch-dispatch action (the one
// internal service-to-service call in the hub/escrow feature, since this is
// the only step that needs orders service's own Paystack secret key —
// everything else admin-api does as a direct DB write). Idempotent: calling
// it twice on an already-cancelled order is a no-op, not a double refund.
func (s *OrdersService) NoShowRefund(ctx context.Context, orderID uuid.UUID) (dto.OrderResp, error) {
	var status, paymentRef sql.NullString
	var totalKobo int64
	err := s.db.QueryRowContext(ctx,
		`SELECT status, payment_reference, total_kobo FROM orders WHERE id=$1`, orderID,
	).Scan(&status, &paymentRef, &totalKobo)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.OrderResp{}, apperrors.NotFound("order not found")
	}
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("load order: %w", err)
	}
	if status.String == string(dto.OrderStatusCancelled) {
		// Already handled — return the current state rather than refunding twice.
		return s.getOrderByID(ctx, orderID)
	}
	if status.String == string(dto.OrderStatusShipped) || status.String == string(dto.OrderStatusDelivered) {
		return dto.OrderResp{}, apperrors.BadRequest("order has already been dispatched — cannot no-show it now")
	}
	if !paymentRef.Valid || paymentRef.String == "" {
		return dto.OrderResp{}, apperrors.Internal(fmt.Errorf("order %s has no payment_reference to refund", orderID))
	}

	refundRef, err := s.refundPaystackTransaction(ctx, paymentRef.String, totalKobo)
	if err != nil {
		return dto.OrderResp{}, err
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `
		UPDATE orders SET status=$1, cancelled_reason=$2, refund_reference=$3, refunded_at=NOW(), updated_at=NOW()
		WHERE id=$4`,
		dto.OrderStatusCancelled, "vendor_no_show_at_dispatch", refundRef, orderID,
	); err != nil {
		return dto.OrderResp{}, fmt.Errorf("mark cancelled: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallet_transactions SET status='failed' WHERE order_id=$1 AND status='pending'`,
		orderID,
	); err != nil {
		return dto.OrderResp{}, fmt.Errorf("reverse wallet credit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return dto.OrderResp{}, fmt.Errorf("commit: %w", err)
	}

	return s.getOrderByID(ctx, orderID)
}

// getOrderByID fetches an order by ID alone, no store or email scoping —
// only used by internal flows (NoShowRefund) that have already authorized
// the caller by other means.
func (s *OrdersService) getOrderByID(ctx context.Context, orderID uuid.UUID) (dto.OrderResp, error) {
	var r orderRow
	err := s.db.QueryRowxContext(ctx, `SELECT `+orderColumns+` FROM orders WHERE id=$1`, orderID).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.OrderResp{}, apperrors.NotFound("order not found")
	}
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("get order by id: %w", err)
	}
	o := rowToOrder(r)
	o.Items = s.loadItems(ctx, r.ID)
	return o, nil
}

// escrowAutoReleaseWindow is how long a dispatched order can sit unconfirmed
// before its vendor's held funds release automatically — the buyer-never-
// confirms fallback. Matches the 7-day window agreed with the user.
const escrowAutoReleaseWindow = 7 * 24 * time.Hour

// StartAutoReleaseLoop runs forever (until ctx is cancelled), releasing any
// order that's been dispatched for longer than escrowAutoReleaseWindow with
// no buyer confirmation. Never blocks or panics the caller — matches the
// "background goroutine, log and continue" shape used elsewhere in this
// codebase (e.g. identity's Paystack DVA provisioning) since no cron/scheduler
// infra exists anywhere in this backend.
func (s *OrdersService) StartAutoReleaseLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	// Run once immediately on startup too, not just after the first tick.
	s.releaseOverdueEscrow(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.releaseOverdueEscrow(ctx)
		}
	}
}

func (s *OrdersService) releaseOverdueEscrow(ctx context.Context) {
	cutoff := time.Now().Add(-escrowAutoReleaseWindow)

	// No explicit row locking here — the two UPDATEs below are each
	// conditioned on the current status (status='shipped' /
	// wallet_transactions.status='pending'), so a duplicate run (e.g. a
	// second instance on the same tick) is a harmless no-op, not a
	// double-release.
	rows, err := s.db.QueryxContext(ctx, `
		SELECT id FROM orders
		WHERE status='shipped' AND dispatched_at < $1 AND delivery_confirmed_at IS NULL`, cutoff)
	if err != nil {
		s.log.Warn().Err(err).Msg("escrow auto-release: query failed")
		return
	}
	var orderIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			orderIDs = append(orderIDs, id)
		}
	}
	rows.Close()

	for _, id := range orderIDs {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE orders SET status=$1, delivered_at=NOW(), delivery_confirmed_at=NOW(), updated_at=NOW()
			WHERE id=$2 AND status='shipped'`, dto.OrderStatusDelivered, id,
		); err != nil {
			s.log.Warn().Err(err).Str("order_id", id.String()).Msg("escrow auto-release: mark delivered failed")
			continue
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE wallet_transactions SET status='completed', released_at=NOW()
			WHERE order_id=$1 AND status='pending'`, id,
		); err != nil {
			s.log.Warn().Err(err).Str("order_id", id.String()).Msg("escrow auto-release: release wallet failed")
			continue
		}
		s.log.Info().Str("order_id", id.String()).Msg("escrow auto-released after 7-day window")
	}
}

func (s *OrdersService) GetOrder(ctx context.Context, storeID uuid.UUID, orderID uuid.UUID) (dto.OrderResp, error) {
	var r orderRow
	err := s.db.QueryRowxContext(ctx, `
		SELECT `+orderColumns+`
		FROM orders WHERE id=$1 AND store_id=$2`, orderID, storeID).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.OrderResp{}, apperrors.NotFound("order not found")
	}
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("get order: %w", err)
	}
	o := rowToOrder(r)
	o.Items = s.loadItems(ctx, r.ID)
	return o, nil
}

func (s *OrdersService) UpdateOrderStatus(ctx context.Context, storeID uuid.UUID, orderID uuid.UUID, req dto.UpdateOrderStatusReq) (dto.OrderResp, error) {
	var r orderRow
	err := s.db.QueryRowxContext(ctx, `
		UPDATE orders SET status=$1, note=COALESCE($2,note), updated_at=NOW()
		WHERE id=$3 AND store_id=$4
		RETURNING `+orderColumns,
		req.Status, req.Note, orderID, storeID).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.OrderResp{}, apperrors.NotFound("order not found")
	}
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("update status: %w", err)
	}
	go s.broker.Publish(storeID.String(), sse.Event{
		Type: "order_updated",
		Data: fmt.Sprintf(`{"order_id":%q,"status":%q}`, orderID.String(), req.Status),
	})

	// Notify customer of the status change asynchronously.
	if r.CustomerEmail != "" {
		custEmail := r.CustomerEmail
		custName := r.CustomerName
		oidStr := orderID.String()
		statusVal := string(req.Status)
		go func() {
			slug, name := s.getStoreSlugName(context.Background(), storeID)
			if err := email.SendStatusUpdate(
				context.Background(),
				custEmail, custName, oidStr, slug, name, statusVal,
			); err != nil {
				s.log.Warn().Err(err).Str("order_id", oidStr).Msg("status update email failed")
			}
		}()
	}

	o := rowToOrder(r)
	o.Items = s.loadItems(ctx, r.ID)
	return o, nil
}

// customerUUID derives a stable UUID from store+email so repeat buyers
// collapse into a single CRM customer record instead of one row per order.
// Storefront buyers aren't authenticated accounts, so email is the only
// durable identity we have at checkout time. Deliberately store-scoped: one
// buyer gets a separate CRM identity per vendor, so this must be called once
// per store, never hoisted above a multi-store loop.
func customerUUID(storeID uuid.UUID, email string) uuid.UUID {
	return uuid.NewSHA1(storeID, []byte(strings.ToLower(strings.TrimSpace(email))))
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505). This service connects via pgx (sqlx.Open("pgx", ...)),
// so errors surface as *pgconn.PgError — NOT *pq.Error, the type the auth and
// catalogue services check for the same purpose. Auth actually connects via
// lib/pq's "postgres" driver, so that check is correct there; catalogue
// connects via pgx like this service does, so its equivalent check is
// silently dead code. Verified against a real Postgres before trusting this.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// claimPaymentReference inserts a row that can only ever exist once per
// payment_reference, inside the same transaction as the order writes it
// guards — so a committed claim guarantees the corresponding order(s) exist,
// and a crash between the two is impossible. A conflict means this
// reference has already been spent; the caller should roll back and look up
// what was created rather than creating a duplicate.
//
// This MUST be a direct INSERT, never a SELECT-then-INSERT: Postgres
// serializes concurrent inserts of the same key under READ COMMITTED, so the
// bare INSERT is what actually makes this replay-safe. A check-then-insert
// reopens the exact race this exists to close.
func claimPaymentReference(ctx context.Context, tx *sqlx.Tx, ref string, amountKobo int64) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO checkout_payments (payment_reference, amount_kobo) VALUES ($1,$2)`,
		ref, amountKobo,
	)
	if err != nil && isUniqueViolation(err) {
		return apperrors.Conflict("payment reference already used")
	}
	return err
}

// insertOrderTx inserts one order, its line items, and the vendor's wallet
// credit, all within the given transaction. Shared by CreateOrder (one
// store) and CreateCheckout (N stores, one per vendor, sharing one payment).
func insertOrderTx(ctx context.Context, tx *sqlx.Tx, storeID uuid.UUID, customerName, customerEmail, deliveryAddress string, items []dto.CreateOrderItem, paymentRef string) (uuid.UUID, int64, error) {
	var totalKobo int64
	for _, it := range items {
		totalKobo += it.PriceKobo * int64(it.Quantity)
	}
	if totalKobo <= 0 {
		return uuid.Nil, 0, apperrors.BadRequest("order total must be greater than zero")
	}

	custID := customerUUID(storeID, customerEmail)

	var orderID uuid.UUID
	err := tx.QueryRowContext(ctx, `
		INSERT INTO orders (store_id, customer_id, customer_name, customer_email, status, total_kobo, delivery_address, payment_reference)
		VALUES ($1,$2,$3,$4,'confirmed',$5,$6,$7)
		RETURNING id`,
		storeID, custID, customerName, customerEmail, totalKobo, deliveryAddress, paymentRef,
	).Scan(&orderID)
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("insert order: %w", err)
	}

	for _, it := range items {
		productID, err := uuid.Parse(it.ProductID)
		if err != nil {
			return uuid.Nil, 0, apperrors.BadRequest("invalid product_id in items")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO order_items (order_id, product_id, name, image_url, quantity, price_kobo)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			orderID, productID, it.Name, it.ImageURL, it.Quantity, it.PriceKobo,
		); err != nil {
			return uuid.Nil, 0, fmt.Errorf("insert order item: %w", err)
		}
	}

	// Credit the vendor's wallet for the full order value, but held in
	// escrow (status='pending', excluded from GetWallet's available-balance
	// query) until the buyer confirms receipt — see ConfirmDelivery below
	// and the auto-release goroutine in cmd/server/main.go.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (store_id, type, amount_kobo, description, reference, order_id, status)
		VALUES ($1,'credit',$2,$3,$4,$5,'pending')`,
		storeID, totalKobo, fmt.Sprintf("Sale — order #%s", orderID.String()[:8]), paymentRef, orderID,
	); err != nil {
		return uuid.Nil, 0, fmt.Errorf("credit wallet: %w", err)
	}

	return orderID, totalKobo, nil
}

// notifyOrderCreated fires the post-commit side effects for one newly
// created order: the SSE broadcast to the vendor's dashboard, the customer
// invoice email, and the vendor alert email. All three fire asynchronously
// and never block the caller, matching CreateOrder's original behavior.
func (s *OrdersService) notifyOrderCreated(storeID, orderID uuid.UUID, totalKobo int64, customerName, customerEmail, customerPhone, deliveryAddress, storeSlug, storeName string, items []dto.CreateOrderItem) {
	// Notify any open SSE/WebSocket dashboard connections.
	go s.broker.Publish(storeID.String(), sse.Event{
		Type: "order_created",
		Data: fmt.Sprintf(`{"order_id":%q,"total_kobo":%d}`, orderID, totalKobo),
	})

	invoiceItems := make([]email.InvoiceItem, len(items))
	for i, it := range items {
		invoiceItems[i] = email.InvoiceItem{
			Name:      it.Name,
			ImageURL:  it.ImageURL,
			Quantity:  int(it.Quantity),
			PriceKobo: it.PriceKobo,
		}
	}
	if storeName == "" {
		storeName = "GoMarketi Store"
	}
	orderIDStr := orderID.String()

	// Send invoice email to customer asynchronously — never block checkout on email delivery.
	if customerEmail != "" {
		go func() {
			if err := email.SendInvoice(
				context.Background(),
				customerEmail,
				customerName,
				orderIDStr,
				storeSlug,
				storeName,
				totalKobo,
				invoiceItems,
			); err != nil {
				s.log.Warn().Err(err).Str("order_id", orderIDStr).Msg("invoice email failed")
			}
		}()
	}

	// Notify vendor of the new order.
	go func() {
		vendorEmail, err := s.getVendorEmail(context.Background(), storeID)
		if err != nil || vendorEmail == "" {
			s.log.Warn().Err(err).Str("store_id", storeID.String()).Msg("vendor email lookup failed")
			return
		}
		if err := email.SendVendorAlert(
			context.Background(),
			vendorEmail,
			storeName,
			orderIDStr,
			customerName,
			customerEmail,
			customerPhone,
			deliveryAddress,
			totalKobo,
			invoiceItems,
		); err != nil {
			s.log.Warn().Err(err).Str("order_id", orderIDStr).Msg("vendor alert email failed")
		}
	}()
}

// findOrdersByPaymentRef returns every order created against a given
// payment_reference, oldest first — used to answer a replayed checkout
// request with what actually happened, instead of erroring on a purchase
// that already succeeded (e.g. a dropped response on a flaky connection).
func (s *OrdersService) findOrdersByPaymentRef(ctx context.Context, ref string) ([]dto.OrderResp, error) {
	rows, err := s.db.QueryxContext(ctx, `
		SELECT `+orderColumns+`
		FROM orders WHERE payment_reference=$1 ORDER BY created_at ASC`, ref)
	if err != nil {
		return nil, fmt.Errorf("find orders by payment_reference: %w", err)
	}
	defer rows.Close()

	orders := make([]dto.OrderResp, 0)
	for rows.Next() {
		var r orderRow
		if err := rows.StructScan(&r); err != nil {
			return nil, err
		}
		o := rowToOrder(r)
		o.PaymentRef = ref
		o.Items = s.loadItems(ctx, r.ID)
		orders = append(orders, o)
	}
	return orders, nil
}

// CreateOrder is called by the storefront checkout after a (simulated)
// successful Paystack charge. It creates the order, its line items, and
// credits the vendor's wallet for the full amount in a single transaction.
// A replayed payment_reference returns the original order instead of
// erroring or double-crediting.
func (s *OrdersService) CreateOrder(ctx context.Context, req dto.CreateOrderReq) (dto.OrderResp, error) {
	storeID, err := uuid.Parse(req.StoreID)
	if err != nil {
		return dto.OrderResp{}, apperrors.BadRequest("invalid store_id")
	}

	var totalKobo int64
	for _, it := range req.Items {
		totalKobo += it.PriceKobo * int64(it.Quantity)
	}
	if totalKobo <= 0 {
		return dto.OrderResp{}, apperrors.BadRequest("order total must be greater than zero")
	}

	// Verify the Paystack charge before touching the database.
	// In dev mode (no PAYSTACK_SECRET_KEY) this is a no-op with a log warning.
	if err := s.verifyPaystackTransaction(ctx, req.PaymentRef, totalKobo); err != nil {
		return dto.OrderResp{}, err
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := claimPaymentReference(ctx, tx, req.PaymentRef, totalKobo); err != nil {
		if apperrors.IsConflict(err) {
			_ = tx.Rollback()
			if existing, findErr := s.findOrdersByPaymentRef(ctx, req.PaymentRef); findErr == nil && len(existing) > 0 {
				return existing[0], nil
			}
			return dto.OrderResp{}, err
		}
		return dto.OrderResp{}, fmt.Errorf("claim payment reference: %w", err)
	}

	orderID, insertedTotal, err := insertOrderTx(ctx, tx, storeID, req.CustomerName, req.CustomerEmail, req.DeliveryAddress, req.Items, req.PaymentRef)
	if err != nil {
		return dto.OrderResp{}, err
	}

	if err := tx.Commit(); err != nil {
		return dto.OrderResp{}, fmt.Errorf("commit: %w", err)
	}

	s.notifyOrderCreated(storeID, orderID, insertedTotal, req.CustomerName, req.CustomerEmail, req.CustomerPhone, req.DeliveryAddress, req.StoreSlug, req.StoreName, req.Items)

	return s.GetOrder(ctx, storeID, orderID)
}

// CreateCheckout is called by the consumer app when a cart spans more than
// one vendor store. One Paystack charge (req.PaymentRef) is verified once
// against the sum of every store's items, then one order is created per
// store inside a single transaction — either all of them land or none do.
// A replayed payment_reference returns the original orders instead of
// erroring or double-crediting.
func (s *OrdersService) CreateCheckout(ctx context.Context, req dto.CreateCheckoutReq) ([]dto.OrderResp, error) {
	var grandTotal int64
	for _, so := range req.Stores {
		for _, it := range so.Items {
			grandTotal += it.PriceKobo * int64(it.Quantity)
		}
	}
	if grandTotal <= 0 {
		return nil, apperrors.BadRequest("order total must be greater than zero")
	}

	// Cheap latency/cost optimization for an obvious replay (e.g. a
	// double-tapped Pay button) — NOT the idempotency guard itself, which is
	// the transactional claim below. Skipping this check would still be correct.
	var alreadyClaimed bool
	_ = s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM checkout_payments WHERE payment_reference=$1)`,
		req.PaymentRef,
	).Scan(&alreadyClaimed)
	if alreadyClaimed {
		if existing, err := s.findOrdersByPaymentRef(ctx, req.PaymentRef); err == nil && len(existing) > 0 {
			return existing, nil
		}
	}

	// Verify the Paystack charge once, against the full multi-store total.
	if err := s.verifyPaystackTransaction(ctx, req.PaymentRef, grandTotal); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := claimPaymentReference(ctx, tx, req.PaymentRef, grandTotal); err != nil {
		if apperrors.IsConflict(err) {
			_ = tx.Rollback()
			if existing, findErr := s.findOrdersByPaymentRef(ctx, req.PaymentRef); findErr == nil && len(existing) > 0 {
				return existing, nil
			}
			return nil, err
		}
		return nil, fmt.Errorf("claim payment reference: %w", err)
	}

	type createdOrder struct {
		storeID   uuid.UUID
		orderID   uuid.UUID
		totalKobo int64
		storeSlug string
		storeName string
		items     []dto.CreateOrderItem
	}
	created := make([]createdOrder, 0, len(req.Stores))

	for _, so := range req.Stores {
		storeID, err := uuid.Parse(so.StoreID)
		if err != nil {
			return nil, apperrors.BadRequest("invalid store_id")
		}
		orderID, subTotal, err := insertOrderTx(ctx, tx, storeID, req.CustomerName, req.CustomerEmail, req.DeliveryAddress, so.Items, req.PaymentRef)
		if err != nil {
			return nil, err
		}
		created = append(created, createdOrder{storeID, orderID, subTotal, so.StoreSlug, so.StoreName, so.Items})
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	resp := make([]dto.OrderResp, 0, len(created))
	for _, c := range created {
		s.notifyOrderCreated(c.storeID, c.orderID, c.totalKobo, req.CustomerName, req.CustomerEmail, req.CustomerPhone, req.DeliveryAddress, c.storeSlug, c.storeName, c.items)
		o, err := s.GetOrder(ctx, c.storeID, c.orderID)
		if err != nil {
			return nil, err
		}
		resp = append(resp, o)
	}

	return resp, nil
}

// getVendorEmail returns the email of the user who owns the given store.
func (s *OrdersService) getVendorEmail(ctx context.Context, storeID uuid.UUID) (string, error) {
	var vendorEmail string
	err := s.db.QueryRowContext(ctx,
		`SELECT u.email FROM stores s JOIN users u ON u.id = s.vendor_id WHERE s.id = $1`,
		storeID,
	).Scan(&vendorEmail)
	if err != nil {
		return "", fmt.Errorf("vendor email lookup: %w", err)
	}
	return vendorEmail, nil
}

// getStoreSlugName returns the slug and display name for a store.
func (s *OrdersService) getStoreSlugName(ctx context.Context, storeID uuid.UUID) (slug, name string) {
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(slug,''), COALESCE(name,'GoMarketi Store') FROM stores WHERE id = $1`,
		storeID,
	).Scan(&slug, &name)
	if name == "" {
		name = "GoMarketi Store"
	}
	return slug, name
}

// ── Wallet ────────────────────────────────────────────────────────────────────

func (s *OrdersService) GetWallet(ctx context.Context, storeID uuid.UUID) (dto.WalletResp, error) {
	var resp dto.WalletResp
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN status='completed' THEN (CASE WHEN type='credit' THEN amount_kobo ELSE -amount_kobo END) ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status='completed' AND type='credit' THEN amount_kobo ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status='pending'   AND type='credit' THEN amount_kobo ELSE 0 END), 0)
		FROM wallet_transactions WHERE store_id=$1`, storeID,
	).Scan(&resp.BalanceKobo, &resp.TotalEarned, &resp.HeldKobo)
	if err != nil {
		return dto.WalletResp{}, fmt.Errorf("wallet balance: %w", err)
	}

	rows, err := s.db.QueryxContext(ctx, `
		SELECT id, type, amount_kobo, description, COALESCE(reference,'') AS reference, status,
		       COALESCE(bank_name,'') AS bank_name, COALESCE(account_number,'') AS account_number,
		       COALESCE(account_name,'') AS account_name, created_at
		FROM wallet_transactions WHERE store_id=$1 ORDER BY created_at DESC LIMIT 30`, storeID)
	if err != nil {
		return dto.WalletResp{}, fmt.Errorf("wallet transactions: %w", err)
	}
	defer rows.Close()

	resp.Transactions = make([]dto.WalletTransactionResp, 0)
	for rows.Next() {
		var r walletTxRow
		if err := rows.StructScan(&r); err != nil {
			return dto.WalletResp{}, err
		}
		resp.Transactions = append(resp.Transactions, dto.WalletTransactionResp{
			ID:            r.ID.String(),
			Type:          r.Type,
			AmountKobo:    r.AmountKobo,
			Description:   r.Description,
			Reference:     r.Reference,
			Status:        r.Status,
			BankName:      r.BankName,
			AccountNumber: r.AccountNumber,
			AccountName:   r.AccountName,
			CreatedAt:     r.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return resp, nil
}

// Withdraw simulates a Paystack transfer payout: validates the balance,
// debits the wallet, and marks the transaction completed instantly (test-mode
// behaviour — a real integration would go through a pending->webhook flow).
func (s *OrdersService) Withdraw(ctx context.Context, storeID uuid.UUID, req dto.WithdrawReq) (dto.WalletResp, error) {
	wallet, err := s.GetWallet(ctx, storeID)
	if err != nil {
		return dto.WalletResp{}, err
	}
	if req.AmountKobo > wallet.BalanceKobo {
		return dto.WalletResp{}, apperrors.BadRequest("insufficient wallet balance")
	}

	ref := fmt.Sprintf("WD_%s", uuid.New().String()[:12])
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO wallet_transactions
			(store_id, type, amount_kobo, description, reference, status, bank_name, account_number, account_name)
		VALUES ($1,'debit',$2,$3,$4,'completed',$5,$6,$7)`,
		storeID, req.AmountKobo,
		fmt.Sprintf("Withdrawal to %s ••%s", req.BankName, req.AccountNumber[len(req.AccountNumber)-4:]),
		ref, req.BankName, req.AccountNumber, req.AccountName,
	)
	if err != nil {
		return dto.WalletResp{}, fmt.Errorf("debit wallet: %w", err)
	}

	go s.broker.Publish(storeID.String(), sse.Event{
		Type: "wallet_updated",
		Data: `{"reason":"withdrawal"}`,
	})

	return s.GetWallet(ctx, storeID)
}

type walletTxRow struct {
	ID            uuid.UUID `db:"id"`
	Type          string    `db:"type"`
	AmountKobo    int64     `db:"amount_kobo"`
	Description   string    `db:"description"`
	Reference     string    `db:"reference"`
	Status        string    `db:"status"`
	BankName      string    `db:"bank_name"`
	AccountNumber string    `db:"account_number"`
	AccountName   string    `db:"account_name"`
	CreatedAt     time.Time `db:"created_at"`
}

func (s *OrdersService) ListAbandonedCarts(ctx context.Context, storeID uuid.UUID, page, perPage int) ([]dto.AbandonedCartResp, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	offset := (page - 1) * perPage

	rows, err := s.db.QueryxContext(ctx,
		`SELECT id, store_id, customer_id, customer_email, items, total_kobo, abandoned_at
		 FROM abandoned_carts WHERE store_id=$1 ORDER BY abandoned_at DESC LIMIT $2 OFFSET $3`,
		storeID, perPage, offset)
	if err != nil {
		return nil, fmt.Errorf("list abandoned carts: %w", err)
	}
	defer rows.Close()

	out := make([]dto.AbandonedCartResp, 0)
	for rows.Next() {
		var r abandonedRow
		if err := rows.StructScan(&r); err != nil {
			return nil, err
		}
		var items []dto.OrderItem
		_ = json.Unmarshal(r.Items, &items)
		if items == nil {
			items = []dto.OrderItem{}
		}
		resp := dto.AbandonedCartResp{
			ID:          r.ID.String(),
			StoreID:     r.StoreID.String(),
			Items:       items,
			TotalKobo:   r.TotalKobo,
			AbandonedAt: r.AbandonedAt.UTC().Format(time.RFC3339),
		}
		if r.CustomerID.Valid {
			v := r.CustomerID.String
			resp.CustomerID = &v
		}
		if r.CustomerEmail.Valid {
			v := r.CustomerEmail.String
			resp.CustomerEmail = &v
		}
		out = append(out, resp)
	}
	return out, nil
}

// ── Customers (CRM) ───────────────────────────────────────────────────────────

func (s *OrdersService) ListCustomers(ctx context.Context, storeID uuid.UUID, page, perPage int, q *string) (dto.CustomerListResp, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	offset := (page - 1) * perPage

	filter := ""
	args := []any{storeID}
	if q != nil && *q != "" {
		filter = ` AND (customer_name ILIKE $2 OR customer_email ILIKE $2)`
		args = append(args, "%"+*q+"%")
	}

	var total int64
	_ = s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT customer_id) FROM orders WHERE store_id=$1`+filter, args...).Scan(&total)

	listArgs := append(args, perPage, offset)
	ph := fmt.Sprintf(`$%d`, len(args)+1)
	ph2 := fmt.Sprintf(`$%d`, len(args)+2)

	rows, err := s.db.QueryxContext(ctx, `
		SELECT
			customer_id::text  AS id,
			MAX(customer_name) AS full_name,
			customer_email     AS email,
			COUNT(*)::int      AS total_orders,
			SUM(total_kobo)    AS total_spent_kobo,
			MAX(created_at)    AS last_order_at
		FROM orders WHERE store_id=$1`+filter+`
		GROUP BY customer_id, customer_email
		ORDER BY MAX(created_at) DESC
		LIMIT `+ph+` OFFSET `+ph2, listArgs...)
	if err != nil {
		return dto.CustomerListResp{}, fmt.Errorf("list customers: %w", err)
	}
	defer rows.Close()

	customers := make([]dto.CustomerResp, 0)
	for rows.Next() {
		var r struct {
			ID             string    `db:"id"`
			FullName       string    `db:"full_name"`
			Email          string    `db:"email"`
			TotalOrders    int32     `db:"total_orders"`
			TotalSpentKobo int64     `db:"total_spent_kobo"`
			LastOrderAt    time.Time `db:"last_order_at"`
		}
		if err := rows.StructScan(&r); err != nil {
			return dto.CustomerListResp{}, err
		}
		last := r.LastOrderAt.UTC().Format(time.RFC3339)
		customers = append(customers, dto.CustomerResp{
			ID:             r.ID,
			FullName:       r.FullName,
			Email:          r.Email,
			TotalOrders:    r.TotalOrders,
			TotalSpentKobo: r.TotalSpentKobo,
			LastOrderAt:    &last,
		})
	}
	return dto.CustomerListResp{Customers: customers, Total: total, Page: page, PerPage: perPage}, nil
}

func (s *OrdersService) GetCustomer(ctx context.Context, storeID uuid.UUID, customerID uuid.UUID) (dto.CustomerResp, error) {
	var r struct {
		ID             string    `db:"id"`
		FullName       string    `db:"full_name"`
		Email          string    `db:"email"`
		TotalOrders    int32     `db:"total_orders"`
		TotalSpentKobo int64     `db:"total_spent_kobo"`
		LastOrderAt    time.Time `db:"last_order_at"`
	}
	err := s.db.QueryRowxContext(ctx, `
		SELECT customer_id::text AS id, MAX(customer_name) AS full_name,
		       customer_email AS email, COUNT(*)::int AS total_orders,
		       SUM(total_kobo) AS total_spent_kobo, MAX(created_at) AS last_order_at
		FROM orders WHERE store_id=$1 AND customer_id=$2
		GROUP BY customer_id, customer_email`, storeID, customerID).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.CustomerResp{}, apperrors.NotFound("customer not found")
	}
	if err != nil {
		return dto.CustomerResp{}, fmt.Errorf("get customer: %w", err)
	}
	last := r.LastOrderAt.UTC().Format(time.RFC3339)
	return dto.CustomerResp{
		ID:             r.ID,
		FullName:       r.FullName,
		Email:          r.Email,
		TotalOrders:    r.TotalOrders,
		TotalSpentKobo: r.TotalSpentKobo,
		LastOrderAt:    &last,
	}, nil
}

// ── Analytics ─────────────────────────────────────────────────────────────────

func (s *OrdersService) GetAnalyticsOverview(ctx context.Context, storeID uuid.UUID) (dto.AnalyticsOverviewResp, error) {
	var resp dto.AnalyticsOverviewResp
	_ = s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN status != 'cancelled' THEN total_kobo ELSE 0 END), 0),
			COUNT(*)::int,
			COUNT(DISTINCT customer_id)::int,
			COUNT(CASE WHEN status='confirmed' THEN 1 END)::int,
			COALESCE(SUM(discount_kobo), 0)
		FROM orders WHERE store_id=$1`, storeID).
		Scan(&resp.TotalRevenueKobo, &resp.TotalOrders, &resp.TotalCustomers, &resp.PendingOrders, &resp.TotalDiscountsKobo)

	// Total expenses = sum of all wallet debit transactions
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount_kobo), 0) FROM wallet_transactions WHERE store_id=$1 AND type='debit' AND status='completed'`,
		storeID,
	).Scan(&resp.TotalExpensesKobo)

	// Storefront visits in the last 30 days
	_ = s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT session_id) FROM storefront_visits WHERE store_id=$1 AND visited_at >= NOW() - INTERVAL '30 days'`,
		storeID,
	).Scan(&resp.StorefrontVisits30d)

	return resp, nil
}

// TrackVisit records a storefront page view. Upserts on (store_id, session_id, page)
// with a 30-minute window to avoid duplicate counts from refreshes.
func (s *OrdersService) TrackVisit(ctx context.Context, storeID uuid.UUID, sessionID, page string) {
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO storefront_visits (store_id, session_id, page)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, storeID, sessionID, page)
}

// GetRevenueTrend returns daily revenue for the last `days` days (default 30).
// Missing days are filled with zero so the chart is always continuous.
func (s *OrdersService) GetRevenueTrend(ctx context.Context, storeID uuid.UUID, days int) ([]dto.RevenueTrendPoint, error) {
	if days < 1 || days > 365 {
		days = 30
	}

	type row struct {
		Date        string `db:"date"`
		RevenueKobo int64  `db:"revenue_kobo"`
		Orders      int    `db:"orders"`
	}
	rows, err := s.db.QueryxContext(ctx, `
		SELECT
			TO_CHAR(DATE(created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS date,
			COALESCE(SUM(CASE WHEN status != 'cancelled' THEN total_kobo ELSE 0 END), 0) AS revenue_kobo,
			COUNT(*)::int AS orders
		FROM orders
		WHERE store_id = $1
		  AND created_at >= NOW() - (INTERVAL '1 day' * $2::int)
		GROUP BY DATE(created_at AT TIME ZONE 'UTC')
		ORDER BY date ASC`, storeID, days)
	if err != nil {
		return nil, fmt.Errorf("revenue trend: %w", err)
	}
	defer rows.Close()

	// Index the db rows by date
	byDate := map[string]dto.RevenueTrendPoint{}
	for rows.Next() {
		var r row
		if err := rows.StructScan(&r); err != nil {
			continue
		}
		byDate[r.Date] = dto.RevenueTrendPoint{Date: r.Date, RevenueKobo: r.RevenueKobo, Orders: r.Orders}
	}

	// Fill every day in the window so the chart has no gaps
	out := make([]dto.RevenueTrendPoint, days)
	for i := range out {
		d := time.Now().UTC().AddDate(0, 0, -(days-1-i)).Format("2006-01-02")
		if p, ok := byDate[d]; ok {
			out[i] = p
		} else {
			out[i] = dto.RevenueTrendPoint{Date: d}
		}
	}
	return out, nil
}

func (s *OrdersService) GetTopProducts(ctx context.Context, storeID uuid.UUID, limit int) ([]dto.TopProductResp, error) {
	if limit < 1 || limit > 50 {
		limit = 5
	}
	rows, err := s.db.QueryxContext(ctx, `
		SELECT
			oi.product_id::text AS product_id,
			MAX(oi.name)        AS name,
			MAX(oi.image_url)   AS image_url,
			SUM(oi.quantity)    AS units_sold,
			SUM(oi.quantity * oi.price_kobo) AS revenue_kobo
		FROM order_items oi
		JOIN orders o ON o.id = oi.order_id
		WHERE o.store_id = $1
		GROUP BY oi.product_id
		ORDER BY revenue_kobo DESC
		LIMIT $2`, storeID, limit)
	if err != nil {
		return nil, fmt.Errorf("top products: %w", err)
	}
	defer rows.Close()

	out := make([]dto.TopProductResp, 0)
	for rows.Next() {
		var r struct {
			ProductID   string `db:"product_id"`
			Name        string `db:"name"`
			ImageURL    string `db:"image_url"`
			UnitsSold   int64  `db:"units_sold"`
			RevenueKobo int64  `db:"revenue_kobo"`
		}
		if err := rows.StructScan(&r); err != nil {
			return nil, err
		}
		out = append(out, dto.TopProductResp{
			ProductID:   r.ProductID,
			Name:        r.Name,
			ImageURL:    r.ImageURL,
			UnitsSold:   r.UnitsSold,
			RevenueKobo: r.RevenueKobo,
		})
	}
	return out, nil
}

// ── Row types ─────────────────────────────────────────────────────────────────

type orderRow struct {
	ID                  uuid.UUID      `db:"id"`
	StoreID             uuid.UUID      `db:"store_id"`
	CustomerID          uuid.UUID      `db:"customer_id"`
	CustomerName        string         `db:"customer_name"`
	CustomerEmail       string         `db:"customer_email"`
	Status              string         `db:"status"`
	TotalKobo           int64          `db:"total_kobo"`
	DeliveryAddress     string         `db:"delivery_address"`
	PaymentReference    sql.NullString `db:"payment_reference"`
	HubReceivedAt       sql.NullTime   `db:"hub_received_at"`
	DispatchedAt        sql.NullTime   `db:"dispatched_at"`
	DeliveredAt         sql.NullTime   `db:"delivered_at"`
	DeliveryConfirmedAt sql.NullTime   `db:"delivery_confirmed_at"`
	CancelledReason     sql.NullString `db:"cancelled_reason"`
	WalletStatus        sql.NullString `db:"wallet_status"`
	CreatedAt           time.Time      `db:"created_at"`
	UpdatedAt           time.Time      `db:"updated_at"`
}

type abandonedRow struct {
	ID            uuid.UUID      `db:"id"`
	StoreID       uuid.UUID      `db:"store_id"`
	CustomerID    sql.NullString `db:"customer_id"`
	CustomerEmail sql.NullString `db:"customer_email"`
	Items         []byte         `db:"items"`
	TotalKobo     int64          `db:"total_kobo"`
	AbandonedAt   time.Time      `db:"abandoned_at"`
}

func rowToOrder(r orderRow) dto.OrderResp {
	o := dto.OrderResp{
		ID:              r.ID.String(),
		StoreID:         r.StoreID.String(),
		CustomerID:      r.CustomerID.String(),
		CustomerName:    r.CustomerName,
		CustomerEmail:   r.CustomerEmail,
		Status:          dto.OrderStatus(r.Status),
		Items:           []dto.OrderItem{},
		TotalKobo:       r.TotalKobo,
		DeliveryAddress: r.DeliveryAddress,
		EscrowStatus:    escrowStatus(r.WalletStatus),
		CreatedAt:       r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       r.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if r.PaymentReference.Valid {
		o.PaymentRef = r.PaymentReference.String
	}
	if r.HubReceivedAt.Valid {
		s := r.HubReceivedAt.Time.UTC().Format(time.RFC3339)
		o.HubReceivedAt = &s
	}
	if r.DispatchedAt.Valid {
		s := r.DispatchedAt.Time.UTC().Format(time.RFC3339)
		o.DispatchedAt = &s
	}
	if r.DeliveredAt.Valid {
		s := r.DeliveredAt.Time.UTC().Format(time.RFC3339)
		o.DeliveredAt = &s
	}
	if r.DeliveryConfirmedAt.Valid {
		s := r.DeliveryConfirmedAt.Time.UTC().Format(time.RFC3339)
		o.DeliveryConfirmedAt = &s
	}
	if r.CancelledReason.Valid {
		o.CancelledReason = &r.CancelledReason.String
	}
	return o
}

// escrowStatus derives the buyer-visible escrow state from the underlying
// wallet_transactions row's status. No row at all (e.g. an order created
// before this feature, or a data inconsistency) reads as "held" — the safe
// default, since it means we've made no promise the money is available.
func escrowStatus(walletStatus sql.NullString) dto.EscrowStatus {
	switch walletStatus.String {
	case "completed":
		return dto.EscrowReleased
	case "failed":
		return dto.EscrowReversed
	default:
		return dto.EscrowHeld
	}
}

func (s *OrdersService) loadItems(ctx context.Context, orderID uuid.UUID) []dto.OrderItem {
	rows, err := s.db.QueryxContext(ctx,
		`SELECT id, product_id, name, image_url, quantity, price_kobo FROM order_items WHERE order_id=$1`, orderID)
	if err != nil {
		return []dto.OrderItem{}
	}
	defer rows.Close()
	items := make([]dto.OrderItem, 0)
	for rows.Next() {
		var item struct {
			ID        uuid.UUID `db:"id"`
			ProductID uuid.UUID `db:"product_id"`
			Name      string    `db:"name"`
			ImageURL  string    `db:"image_url"`
			Quantity  int32     `db:"quantity"`
			PriceKobo int64     `db:"price_kobo"`
		}
		if err := rows.StructScan(&item); err != nil {
			continue
		}
		items = append(items, dto.OrderItem{
			ID:        item.ID.String(),
			ProductID: item.ProductID.String(),
			Name:      item.Name,
			ImageURL:  item.ImageURL,
			Quantity:  item.Quantity,
			PriceKobo: item.PriceKobo,
		})
	}
	return items
}
