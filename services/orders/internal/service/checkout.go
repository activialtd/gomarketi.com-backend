package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/email"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/sse"
	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
)

// CreateCheckout turns one payment into one order per vendor.
//
// How the money splits:
//   - Each vendor's order carries only their own items, and their wallet is
//     credited only that amount.
//   - Delivery is charged once for the whole basket, however many vendors are
//     in it, because the buyer receives one consolidated delivery from the
//     hub. The fee lives on the checkout row and is never credited to a
//     vendor — the platform does the delivering, so it keeps that money.
//   - The checkout's total_kobo (items + delivery) is what Paystack must have
//     collected, and what the charge is verified against.
//
// Retrying the same payment_reference returns the orders already created
// rather than charging the cart twice.
func (s *OrdersService) CreateCheckout(ctx context.Context, req dto.CreateCheckoutReq) (dto.CheckoutResp, error) {
	if existing, err := s.checkoutByReference(ctx, req.PaymentRef); err == nil {
		return existing, nil
	} else if !apperrors.IsNotFound(err) {
		return dto.CheckoutResp{}, err
	}

	// Parse every store id up front so a bad one fails before any payment
	// verification or writes.
	storeIDs := make([]uuid.UUID, 0, len(req.Stores))
	itemsByStore := make(map[uuid.UUID][]dto.CreateOrderItem, len(req.Stores))
	storeTotals := make(map[uuid.UUID]int64, len(req.Stores))
	var itemsKobo int64

	for _, sto := range req.Stores {
		storeID, err := uuid.Parse(sto.StoreID)
		if err != nil {
			return dto.CheckoutResp{}, apperrors.BadRequest("invalid store_id in stores")
		}
		if _, seen := itemsByStore[storeID]; seen {
			return dto.CheckoutResp{}, apperrors.BadRequest("duplicate store_id in stores")
		}
		var storeTotal int64
		for _, it := range sto.Items {
			if _, err := uuid.Parse(it.ProductID); err != nil {
				return dto.CheckoutResp{}, apperrors.BadRequest("invalid product_id in items")
			}
			storeTotal += it.PriceKobo * int64(it.Quantity)
		}
		if storeTotal <= 0 {
			return dto.CheckoutResp{}, apperrors.BadRequest("each store's items must total more than zero")
		}
		storeIDs = append(storeIDs, storeID)
		itemsByStore[storeID] = sto.Items
		storeTotals[storeID] = storeTotal
		itemsKobo += storeTotal
	}

	deliveryFeeKobo, deliveryTitle, deliveryOptionID, err := s.resolveCheckoutDelivery(ctx, storeIDs, req)
	if err != nil {
		return dto.CheckoutResp{}, err
	}

	totalKobo := itemsKobo + deliveryFeeKobo
	if err := s.verifyPaystackTransaction(ctx, req.PaymentRef, totalKobo); err != nil {
		return dto.CheckoutResp{}, err
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return dto.CheckoutResp{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var checkoutID uuid.UUID
	err = tx.QueryRowContext(ctx, `
		INSERT INTO checkouts (customer_name, customer_email, customer_phone, delivery_address,
		                       payment_reference, delivery_option_id, delivery_option_title,
		                       delivery_fee_kobo, items_kobo, total_kobo)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id`,
		req.CustomerName, req.CustomerEmail, req.CustomerPhone, req.DeliveryAddress,
		req.PaymentRef, deliveryOptionID, deliveryTitle,
		deliveryFeeKobo, itemsKobo, totalKobo,
	).Scan(&checkoutID)
	if err != nil {
		return dto.CheckoutResp{}, fmt.Errorf("insert checkout: %w", err)
	}

	custID := customerUUID(storeIDs[0], req.CustomerEmail)
	orderIDs := make([]uuid.UUID, 0, len(storeIDs))

	for _, storeID := range storeIDs {
		storeTotal := storeTotals[storeID]

		var orderID uuid.UUID
		// delivery_fee_kobo stays 0 on each order: the fee is a property of
		// the checkout, and duplicating it here would double-count it in any
		// sum over orders.
		err = tx.QueryRowContext(ctx, `
			INSERT INTO orders (store_id, customer_id, customer_name, customer_email, status,
			                    total_kobo, delivery_address, checkout_id)
			VALUES ($1,$2,$3,$4,'confirmed',$5,$6,$7)
			RETURNING id`,
			storeID, custID, req.CustomerName, req.CustomerEmail,
			storeTotal, req.DeliveryAddress, checkoutID,
		).Scan(&orderID)
		if err != nil {
			return dto.CheckoutResp{}, fmt.Errorf("insert order: %w", err)
		}

		for _, it := range itemsByStore[storeID] {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO order_items (order_id, product_id, name, image_url, quantity, price_kobo)
				VALUES ($1,$2,$3,$4,$5,$6)`,
				orderID, it.ProductID, it.Name, it.ImageURL, it.Quantity, it.PriceKobo,
			); err != nil {
				return dto.CheckoutResp{}, fmt.Errorf("insert order item: %w", err)
			}
		}

		// Items only — the delivery fee is the platform's, not the vendor's.
		// Held in escrow ('pending') until the buyer confirms delivery.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO wallet_transactions (store_id, type, amount_kobo, description, reference, order_id, status)
			VALUES ($1,'credit',$2,$3,$4,$5,'pending')`,
			storeID, storeTotal,
			fmt.Sprintf("Sale — order #%s", orderID.String()[:8]),
			req.PaymentRef, orderID,
		); err != nil {
			return dto.CheckoutResp{}, fmt.Errorf("credit wallet: %w", err)
		}

		orderIDs = append(orderIDs, orderID)
	}

	if err := tx.Commit(); err != nil {
		return dto.CheckoutResp{}, fmt.Errorf("commit: %w", err)
	}

	// Each vendor's dashboard hears about its own order only.
	for i, storeID := range storeIDs {
		sid, oid, total := storeID.String(), orderIDs[i].String(), storeTotals[storeID]
		go s.broker.Publish(sid, sse.Event{
			Type: "order_created",
			Data: fmt.Sprintf(`{"order_id":%q,"total_kobo":%d}`, oid, total),
		})
	}

	s.sendCheckoutEmails(req, storeIDs, orderIDs, storeTotals, itemsByStore, deliveryFeeKobo, deliveryTitle, totalKobo)

	return s.checkoutResp(ctx, checkoutID)
}

// resolveCheckoutDelivery prices delivery for the whole basket. The option may
// belong to any store in it — the buyer picks one delivery, not one per
// vendor. Returns the fee, its title, and the option id (nil when none).
func (s *OrdersService) resolveCheckoutDelivery(ctx context.Context, storeIDs []uuid.UUID, req dto.CreateCheckoutReq) (int64, string, any, error) {
	ids := make([]string, len(storeIDs))
	for i, id := range storeIDs {
		ids[i] = id.String()
	}

	if req.DeliveryOptionID != "" {
		optionID, err := uuid.Parse(req.DeliveryOptionID)
		if err != nil {
			return 0, "", nil, apperrors.BadRequest("invalid delivery_option_id")
		}
		var (
			priceKobo int64
			title     string
		)
		query, args, err := sqlx.In(`
			SELECT price_kobo, title FROM store_delivery_options
			WHERE id = ? AND store_id IN (?) AND is_active = TRUE`, optionID, ids)
		if err != nil {
			return 0, "", nil, fmt.Errorf("build delivery lookup: %w", err)
		}
		err = s.db.QueryRowContext(ctx, s.db.Rebind(query), args...).Scan(&priceKobo, &title)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", nil, apperrors.BadRequest("delivery option does not belong to any store in this checkout")
		}
		if err != nil {
			return 0, "", nil, fmt.Errorf("lookup delivery option: %w", err)
		}
		return priceKobo, title, optionID, nil
	}

	if req.DeliveryFeeKobo <= 0 {
		return 0, "", nil, nil
	}

	// A bare fee is only trusted when no vendor in the basket has published
	// options — otherwise the client should have picked one.
	query, args, err := sqlx.In(`
		SELECT COUNT(*) FROM store_delivery_options
		WHERE store_id IN (?) AND is_active = TRUE`, ids)
	if err != nil {
		return 0, "", nil, fmt.Errorf("build delivery count: %w", err)
	}
	var configured int
	if err := s.db.QueryRowContext(ctx, s.db.Rebind(query), args...).Scan(&configured); err != nil {
		return 0, "", nil, fmt.Errorf("count delivery options: %w", err)
	}
	if configured > 0 {
		return 0, "", nil, apperrors.BadRequest("delivery_option_id is required for this checkout")
	}
	return req.DeliveryFeeKobo, "", nil, nil
}

// sendCheckoutEmails sends the buyer one invoice for the whole basket and
// each vendor an alert for their own order. Fired in the background —
// checkout never waits on mail.
func (s *OrdersService) sendCheckoutEmails(
	req dto.CreateCheckoutReq,
	storeIDs []uuid.UUID,
	orderIDs []uuid.UUID,
	storeTotals map[uuid.UUID]int64,
	itemsByStore map[uuid.UUID][]dto.CreateOrderItem,
	deliveryFeeKobo int64,
	deliveryTitle string,
	totalKobo int64,
) {
	// One invoice covering every vendor's items, since the buyer made one
	// payment and receives one delivery.
	allItems := make([]email.InvoiceItem, 0)
	for _, storeID := range storeIDs {
		for _, it := range itemsByStore[storeID] {
			allItems = append(allItems, email.InvoiceItem{
				Name:      it.Name,
				ImageURL:  it.ImageURL,
				Quantity:  int(it.Quantity),
				PriceKobo: it.PriceKobo,
			})
		}
	}

	if req.CustomerEmail != "" {
		firstOrder := orderIDs[0].String()
		storeSlug, storeName := "", "GoMarketi"
		if len(req.Stores) == 1 {
			storeSlug, storeName = req.Stores[0].StoreSlug, req.Stores[0].StoreName
		}
		go func() {
			if err := email.SendInvoice(
				context.Background(), req.CustomerEmail, req.CustomerName, firstOrder,
				storeSlug, storeName, totalKobo, deliveryFeeKobo, deliveryTitle, allItems,
			); err != nil {
				s.log.Warn().Err(err).Str("checkout_ref", req.PaymentRef).Msg("checkout invoice email failed")
			}
		}()
	}

	for i, storeID := range storeIDs {
		storeID, orderID, storeTotal := storeID, orderIDs[i].String(), storeTotals[storeID]
		storeName := "GoMarketi Store"
		for _, sto := range req.Stores {
			if sto.StoreID == storeID.String() && sto.StoreName != "" {
				storeName = sto.StoreName
			}
		}
		vendorItems := make([]email.InvoiceItem, 0, len(itemsByStore[storeID]))
		for _, it := range itemsByStore[storeID] {
			vendorItems = append(vendorItems, email.InvoiceItem{
				Name:      it.Name,
				ImageURL:  it.ImageURL,
				Quantity:  int(it.Quantity),
				PriceKobo: it.PriceKobo,
			})
		}
		go func() {
			vendorEmail, err := s.getVendorEmail(context.Background(), storeID)
			if err != nil || vendorEmail == "" {
				s.log.Warn().Err(err).Str("store_id", storeID.String()).Msg("vendor email lookup failed")
				return
			}
			// The vendor sees their own items and their own total — delivery
			// is the platform's line, not theirs.
			if err := email.SendVendorAlert(
				context.Background(), vendorEmail, storeName, orderID,
				req.CustomerName, req.CustomerEmail, req.CustomerPhone,
				req.DeliveryAddress, storeTotal, 0, "", vendorItems,
			); err != nil {
				s.log.Warn().Err(err).Str("order_id", orderID).Msg("vendor alert email failed")
			}
		}()
	}
}

// checkoutByReference returns an already-created checkout for a payment
// reference, so a retry is idempotent instead of charging the cart twice.
func (s *OrdersService) checkoutByReference(ctx context.Context, ref string) (dto.CheckoutResp, error) {
	var id uuid.UUID
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM checkouts WHERE payment_reference = $1`, ref).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.CheckoutResp{}, apperrors.NotFound("checkout not found")
	}
	if err != nil {
		return dto.CheckoutResp{}, fmt.Errorf("lookup checkout: %w", err)
	}
	return s.checkoutResp(ctx, id)
}

func (s *OrdersService) checkoutResp(ctx context.Context, checkoutID uuid.UUID) (dto.CheckoutResp, error) {
	var (
		resp  dto.CheckoutResp
		title sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, items_kobo, delivery_fee_kobo, delivery_option_title, total_kobo
		FROM checkouts WHERE id = $1`, checkoutID,
	).Scan(&resp.CheckoutID, &resp.ItemsKobo, &resp.DeliveryFeeKobo, &title, &resp.TotalKobo)
	if err != nil {
		return dto.CheckoutResp{}, fmt.Errorf("read checkout: %w", err)
	}
	resp.DeliveryOptionTitle = title.String

	rows, err := s.db.QueryxContext(ctx, `
		SELECT id, store_id, customer_id, customer_name, customer_email,
		       status, total_kobo, delivery_address, delivery_fee_kobo,
		       delivery_option_title, escrow_status, hub_received_at, dispatched_at, delivered_at,
		       delivery_confirmed_at, dispute_status, dispute_reason, disputed_at,
		       created_at, updated_at
		FROM orders WHERE checkout_id = $1 ORDER BY created_at`, checkoutID)
	if err != nil {
		return dto.CheckoutResp{}, fmt.Errorf("read checkout orders: %w", err)
	}
	defer rows.Close()

	resp.Orders = make([]dto.OrderResp, 0)
	for rows.Next() {
		var r orderRow
		if err := rows.StructScan(&r); err != nil {
			return dto.CheckoutResp{}, fmt.Errorf("scan order: %w", err)
		}
		o := rowToOrder(r)
		o.Items = s.loadItems(ctx, r.ID)
		resp.Orders = append(resp.Orders, o)
	}
	return resp, rows.Err()
}
