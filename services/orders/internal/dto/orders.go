// Package dto defines request and response shapes for the orders service.
package dto

// ── Orders ────────────────────────────────────────────────────────────────────

// OrderStatus represents the lifecycle of an order.
type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusConfirmed OrderStatus = "confirmed"
	OrderStatusShipped   OrderStatus = "shipped"
	OrderStatusDelivered OrderStatus = "delivered"
	OrderStatusCancelled OrderStatus = "cancelled"
)

// OrderItem is a single line item within an order.
type OrderItem struct {
	ID        string `json:"id"`
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
	ImageURL  string `json:"image_url,omitempty"`
	Quantity  int32  `json:"quantity"`
	PriceKobo int64  `json:"price_kobo"`
}

// OrderResp is returned for any order read operation.
type OrderResp struct {
	ID              string      `json:"id"`
	StoreID         string      `json:"store_id"`
	CustomerID      string      `json:"customer_id"`
	CustomerName    string      `json:"customer_name"`
	CustomerEmail   string      `json:"customer_email"`
	Status          OrderStatus `json:"status"`
	Items           []OrderItem `json:"items"`
	TotalKobo       int64       `json:"total_kobo"`
	DeliveryAddress string      `json:"delivery_address"`
	CreatedAt       string      `json:"created_at"`
	UpdatedAt       string      `json:"updated_at"`
}

// OrderListResp wraps a paginated list of orders.
type OrderListResp struct {
	Orders  []OrderResp `json:"orders"`
	Total   int64       `json:"total"`
	Page    int         `json:"page"`
	PerPage int         `json:"per_page"`
}

// CreateOrderItem is a single line item in CreateOrderReq.
type CreateOrderItem struct {
	ProductID string `json:"product_id" validate:"required,uuid"`
	Name      string `json:"name"       validate:"required"`
	ImageURL  string `json:"image_url"`
	Quantity  int32  `json:"quantity"   validate:"required,min=1"`
	PriceKobo int64  `json:"price_kobo" validate:"min=0"`
}

// CreateOrderReq is the body for POST /v1/orders/public — called directly
// from the storefront checkout after a successful (simulated) Paystack charge.
type CreateOrderReq struct {
	StoreID         string            `json:"store_id"         validate:"required,uuid"`
	CustomerName    string            `json:"customer_name"    validate:"required"`
	CustomerEmail   string            `json:"customer_email"   validate:"required,email"`
	CustomerPhone   string            `json:"customer_phone"`
	DeliveryAddress string            `json:"delivery_address"`
	Items           []CreateOrderItem `json:"items"             validate:"required,min=1,dive"`
	PaymentRef      string            `json:"payment_reference" validate:"required"`
}

// UpdateOrderStatusReq is the body for PATCH /v1/orders/:id/status.
type UpdateOrderStatusReq struct {
	Status OrderStatus `json:"status" validate:"required,oneof=confirmed shipped delivered cancelled"`
	Note   *string     `json:"note"`
}

// AbandonedCartResp is a single abandoned cart entry.
type AbandonedCartResp struct {
	ID            string      `json:"id"`
	StoreID       string      `json:"store_id"`
	CustomerID    *string     `json:"customer_id,omitempty"`
	CustomerEmail *string     `json:"customer_email,omitempty"`
	Items         []OrderItem `json:"items"`
	TotalKobo     int64       `json:"total_kobo"`
	AbandonedAt   string      `json:"abandoned_at"`
}

// ── Customers (CRM) ───────────────────────────────────────────────────────────

// CustomerResp is a single customer in the CRM list.
type CustomerResp struct {
	ID             string  `json:"id"`
	FullName       string  `json:"full_name"`
	Email          string  `json:"email"`
	Phone          *string `json:"phone,omitempty"`
	TotalOrders    int32   `json:"total_orders"`
	TotalSpentKobo int64   `json:"total_spent_kobo"`
	LastOrderAt    *string `json:"last_order_at,omitempty"`
}

// CustomerListResp wraps a paginated customer list.
type CustomerListResp struct {
	Customers []CustomerResp `json:"customers"`
	Total     int64          `json:"total"`
	Page      int            `json:"page"`
	PerPage   int            `json:"per_page"`
}

// ── Analytics ─────────────────────────────────────────────────────────────────

// AnalyticsOverviewResp is returned by GET /v1/analytics/overview.
// All monetary values are in kobo to match the absolute rules.
type AnalyticsOverviewResp struct {
	TotalRevenueKobo int64 `json:"total_revenue_kobo"`
	TotalOrders      int32 `json:"total_orders"`
	TotalCustomers   int32 `json:"total_customers"`
	PendingOrders    int32 `json:"pending_orders"`
	LowStockProducts int32 `json:"low_stock_products"`
}

// TopProductResp is a single entry in the top-selling products list,
// aggregated from order_items across all of a store's orders.
type TopProductResp struct {
	ProductID    string `json:"product_id"`
	Name         string `json:"name"`
	ImageURL     string `json:"image_url,omitempty"`
	UnitsSold    int64  `json:"units_sold"`
	RevenueKobo  int64  `json:"revenue_kobo"`
}

// ── Wallet ────────────────────────────────────────────────────────────────────

// WalletTransactionResp is a single ledger entry.
type WalletTransactionResp struct {
	ID            string `json:"id"`
	Type          string `json:"type"` // credit | debit
	AmountKobo    int64  `json:"amount_kobo"`
	Description   string `json:"description"`
	Reference     string `json:"reference,omitempty"`
	Status        string `json:"status"`
	BankName      string `json:"bank_name,omitempty"`
	AccountNumber string `json:"account_number,omitempty"`
	AccountName   string `json:"account_name,omitempty"`
	CreatedAt     string `json:"created_at"`
}

// WalletResp is returned by GET /v1/wallet.
type WalletResp struct {
	BalanceKobo  int64                    `json:"balance_kobo"`
	TotalEarned  int64                    `json:"total_earned_kobo"`
	Transactions []WalletTransactionResp `json:"transactions"`
}

// WithdrawReq is the body for POST /v1/wallet/withdraw.
type WithdrawReq struct {
	AmountKobo    int64  `json:"amount_kobo"    validate:"required,min=100"`
	BankName      string `json:"bank_name"      validate:"required"`
	AccountNumber string `json:"account_number" validate:"required,len=10"`
	AccountName   string `json:"account_name"   validate:"required"`
}

// ── Shared ────────────────────────────────────────────────────────────────────

// ErrorResp is the standard error envelope.
type ErrorResp struct {
	Error string `json:"error"`
}

// ValidationErrorResp wraps field-level validation failures.
type ValidationErrorResp struct {
	Error  string       `json:"error"`
	Fields []FieldError `json:"fields,omitempty"`
}

// FieldError is a single field validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}
