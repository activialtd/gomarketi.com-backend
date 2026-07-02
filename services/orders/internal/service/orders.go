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
	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/sse"
)

type OrdersService struct {
	db     *sqlx.DB
	log    zerolog.Logger
	broker *sse.Broker
}

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
		`SELECT id, store_id, customer_id, customer_name, customer_email, status, total_kobo, delivery_address, created_at, updated_at `+
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

func (s *OrdersService) GetOrder(ctx context.Context, storeID uuid.UUID, orderID uuid.UUID) (dto.OrderResp, error) {
	var r orderRow
	err := s.db.QueryRowxContext(ctx, `
		SELECT id, store_id, customer_id, customer_name, customer_email,
		       status, total_kobo, delivery_address, created_at, updated_at
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
		RETURNING id, store_id, customer_id, customer_name, customer_email,
		          status, total_kobo, delivery_address, created_at, updated_at`,
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
	o := rowToOrder(r)
	o.Items = s.loadItems(ctx, r.ID)
	return o, nil
}

// customerUUID derives a stable UUID from store+email so repeat buyers
// collapse into a single CRM customer record instead of one row per order.
// Storefront buyers aren't authenticated accounts, so email is the only
// durable identity we have at checkout time.
func customerUUID(storeID uuid.UUID, email string) uuid.UUID {
	return uuid.NewSHA1(storeID, []byte(strings.ToLower(strings.TrimSpace(email))))
}

// CreateOrder is called by the storefront checkout after a (simulated)
// successful Paystack charge. It creates the order, its line items, and
// credits the vendor's wallet for the full amount in a single transaction.
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

	custID := customerUUID(storeID, req.CustomerEmail)

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var orderID uuid.UUID
	err = tx.QueryRowContext(ctx, `
		INSERT INTO orders (store_id, customer_id, customer_name, customer_email, status, total_kobo, delivery_address)
		VALUES ($1,$2,$3,$4,'confirmed',$5,$6)
		RETURNING id`,
		storeID, custID, req.CustomerName, req.CustomerEmail, totalKobo, req.DeliveryAddress,
	).Scan(&orderID)
	if err != nil {
		return dto.OrderResp{}, fmt.Errorf("insert order: %w", err)
	}

	for _, it := range req.Items {
		productID, err := uuid.Parse(it.ProductID)
		if err != nil {
			return dto.OrderResp{}, apperrors.BadRequest("invalid product_id in items")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO order_items (order_id, product_id, name, image_url, quantity, price_kobo)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			orderID, productID, it.Name, it.ImageURL, it.Quantity, it.PriceKobo,
		); err != nil {
			return dto.OrderResp{}, fmt.Errorf("insert order item: %w", err)
		}
	}

	// Credit the vendor's wallet for the full order value.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (store_id, type, amount_kobo, description, reference, order_id, status)
		VALUES ($1,'credit',$2,$3,$4,$5,'completed')`,
		storeID, totalKobo, fmt.Sprintf("Sale — order #%s", orderID.String()[:8]), req.PaymentRef, orderID,
	); err != nil {
		return dto.OrderResp{}, fmt.Errorf("credit wallet: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return dto.OrderResp{}, fmt.Errorf("commit: %w", err)
	}

	// Notify any open SSE dashboard connections
	go s.broker.Publish(storeID.String(), sse.Event{
		Type: "order_created",
		Data: fmt.Sprintf(`{"order_id":%q,"total_kobo":%d}`, orderID, totalKobo),
	})

	return s.GetOrder(ctx, storeID, orderID)
}

// ── Wallet ────────────────────────────────────────────────────────────────────

func (s *OrdersService) GetWallet(ctx context.Context, storeID uuid.UUID) (dto.WalletResp, error) {
	var resp dto.WalletResp
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN type='credit' THEN amount_kobo ELSE -amount_kobo END), 0),
			COALESCE(SUM(CASE WHEN type='credit' THEN amount_kobo ELSE 0 END), 0)
		FROM wallet_transactions WHERE store_id=$1 AND status='completed'`, storeID,
	).Scan(&resp.BalanceKobo, &resp.TotalEarned)
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
			COUNT(CASE WHEN status='confirmed' THEN 1 END)::int
		FROM orders WHERE store_id=$1`, storeID).
		Scan(&resp.TotalRevenueKobo, &resp.TotalOrders, &resp.TotalCustomers, &resp.PendingOrders)
	return resp, nil
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
	ID              uuid.UUID `db:"id"`
	StoreID         uuid.UUID `db:"store_id"`
	CustomerID      uuid.UUID `db:"customer_id"`
	CustomerName    string    `db:"customer_name"`
	CustomerEmail   string    `db:"customer_email"`
	Status          string    `db:"status"`
	TotalKobo       int64     `db:"total_kobo"`
	DeliveryAddress string    `db:"delivery_address"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
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
	return dto.OrderResp{
		ID:              r.ID.String(),
		StoreID:         r.StoreID.String(),
		CustomerID:      r.CustomerID.String(),
		CustomerName:    r.CustomerName,
		CustomerEmail:   r.CustomerEmail,
		Status:          dto.OrderStatus(r.Status),
		Items:           []dto.OrderItem{},
		TotalKobo:       r.TotalKobo,
		DeliveryAddress: r.DeliveryAddress,
		CreatedAt:       r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       r.UpdatedAt.UTC().Format(time.RFC3339),
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
