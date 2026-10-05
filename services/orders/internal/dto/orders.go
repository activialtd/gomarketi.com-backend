// Package dto defines request and response shapes for the orders service.
package dto

// ── Orders ────────────────────────────────────────────────────────────────────

// OrderStatus represents the lifecycle of an order.
type OrderStatus string

const (
	// OrderStatusAwaitingPayment is an order that exists but has not been paid
	// for. Created at checkout, before the charge, so a payment can never
	// arrive with nothing attached to it.
	OrderStatusAwaitingPayment OrderStatus = "awaiting_payment"
	// OrderStatusAbandoned is an awaiting_payment order the buyer never came
	// back to. Not cancelled: they chose items, entered their details and
	// picked a delivery option, which is exactly what the vendor's abandoned
	// page is for.
	OrderStatusAbandoned OrderStatus = "abandoned"
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusConfirmed OrderStatus = "confirmed"
	// OrderStatusAtHub means the vendor has delivered this order's items to
	// the central GoMarketi office (admin-confirmed hub intake).
	OrderStatusAtHub OrderStatus = "at_hub"
	// OrderStatusShipped means GoMarketi has dispatched the (possibly
	// multi-vendor) consolidated batch from the hub to the customer — not
	// that the vendor shipped it themselves.
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

// EscrowStatus summarizes the held/released/reversed state of the vendor's
// wallet credit for an order — derived from wallet_transactions.status, not
// stored directly.
type EscrowStatus string

const (
	EscrowHeld     EscrowStatus = "held"
	EscrowReleased EscrowStatus = "released"
	EscrowReversed EscrowStatus = "reversed"
)

// DisputeStatus tracks a buyer's "I never received this" report — orthogonal
// to OrderStatus, which stays 'shipped'/'delivered' for fulfillment tracking
// even while a dispute is open. Reported disputes block the 7-day escrow
// auto-release (see releaseOverdueEscrow) until an admin resolves them.
type DisputeStatus string

const (
	DisputeReported  DisputeStatus = "reported"
	DisputeRefunded  DisputeStatus = "refunded"
	DisputeDismissed DisputeStatus = "dismissed"
)

// OrderResp is returned for any order read operation.
// Fulfilment says who physically delivers an order, which decides what the
// vendor is allowed to do with it.
//
// A basket spanning several vendors becomes several order rows sharing one
// payment_reference. Those go through GoMarketi's hub: each vendor brings
// their part in, and GoMarketi consolidates and dispatches one delivery, so
// the vendor's last step is at_hub. An order that is the only one on its
// reference is that vendor's own delivery end to end, so they mark it
// shipped themselves and never see a hub step.
type Fulfilment string

const (
	FulfilmentVendor    Fulfilment = "vendor"
	FulfilmentGoMarketi Fulfilment = "gomarketi"
)

type OrderResp struct {
	ID                  string         `json:"id"`
	StoreID             string         `json:"store_id"`
	CustomerID          string         `json:"customer_id"`
	CustomerName        string         `json:"customer_name"`
	CustomerEmail       string         `json:"customer_email"`
	Status              OrderStatus    `json:"status"`
	Fulfilment          Fulfilment     `json:"fulfilment"`
	Items               []OrderItem    `json:"items"`
	TotalKobo           int64          `json:"total_kobo"`
	DeliveryFeeKobo     int64          `json:"delivery_fee_kobo"`
	DeliveryAddress     string         `json:"delivery_address"`
	PaymentRef          string         `json:"payment_reference,omitempty"`
	HubReceivedAt       *string        `json:"hub_received_at,omitempty"`
	DispatchedAt        *string        `json:"dispatched_at,omitempty"`
	DeliveredAt         *string        `json:"delivered_at,omitempty"`
	DeliveryConfirmedAt *string        `json:"delivery_confirmed_at,omitempty"`
	CancelledReason     *string        `json:"cancelled_reason,omitempty"`
	EscrowStatus        EscrowStatus   `json:"escrow_status"`
	DisputeStatus       *DisputeStatus `json:"dispute_status,omitempty"`
	DisputeReason       *string        `json:"dispute_reason,omitempty"`
	DisputedAt          *string        `json:"disputed_at,omitempty"`
	CreatedAt           string         `json:"created_at"`
	UpdatedAt           string         `json:"updated_at"`
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
	StoreSlug       string            `json:"store_slug"`
	StoreName       string            `json:"store_name"`
	CustomerName    string            `json:"customer_name"    validate:"required"`
	CustomerEmail   string            `json:"customer_email"   validate:"required,email"`
	CustomerPhone   string            `json:"customer_phone"`
	DeliveryAddress string            `json:"delivery_address"`
	Items           []CreateOrderItem `json:"items"             validate:"required,min=1,dive"`
	// Optional: the server mints one when the order is placed.
	PaymentRef string `json:"payment_reference"`
	// DeliveryOptionID names one of the store's delivery options. When set,
	// the price is read from that option — DeliveryFeeKobo is only trusted as
	// a fallback for stores that have not configured any options.
	DeliveryOptionID string `json:"delivery_option_id" validate:"omitempty,uuid"`
	DeliveryFeeKobo  int64  `json:"delivery_fee_kobo"  validate:"min=0"`
}

// CreateCheckoutStoreOrder is one vendor's slice of a multi-store checkout —
// same shape as CreateOrderReq but without its own payment_reference, since
// one payment covers every store in the checkout.
type CreateCheckoutStoreOrder struct {
	StoreID   string            `json:"store_id"   validate:"required,uuid"`
	StoreSlug string            `json:"store_slug"`
	StoreName string            `json:"store_name"`
	Items     []CreateOrderItem `json:"items"       validate:"required,min=1,dive"`
}

// CreateCheckoutReq is the body for POST /v1/orders/public/checkout — used
// when a single checkout spans more than one vendor store. One Paystack
// charge (payment_reference) is verified once against the sum of every
// store's items, then one order per store is created atomically.
type CreateCheckoutReq struct {
	CustomerName    string `json:"customer_name"     validate:"required"`
	CustomerEmail   string `json:"customer_email"    validate:"required,email"`
	CustomerPhone   string `json:"customer_phone"`
	DeliveryAddress string `json:"delivery_address"`
	// Optional: the server mints one when the order is placed. Accepted only
	// so a client replaying a known reference can be made idempotent.
	PaymentRef string                     `json:"payment_reference"`
	Stores     []CreateCheckoutStoreOrder `json:"stores"            validate:"required,min=1,dive"`
	// Delivery is charged once for the whole basket, however many vendors it
	// spans, because the hub sends one consolidated trip. The option may
	// belong to any store in the basket; the price is read from that row.
	DeliveryOptionID string `json:"delivery_option_id" validate:"omitempty,uuid"`
}

// CreateCheckoutResp returns one order per store in the same order the
// request's Stores array was given in.
type CreateCheckoutResp struct {
	Orders []OrderResp `json:"orders"`
}

// UpdateOrderStatusReq is the body for PATCH /v1/orders/:id/status.
//
// Vendors may set confirmed, at_hub, shipped and cancelled. at_hub and
// shipped were previously admin-only, on the reasoning that a vendor should
// not be able to self-report progress — shipped sets dispatched_at, which
// starts the seven-day escrow auto-release clock, so a vendor marking their
// own order shipped starts their own payout timer. That restriction left
// buyers with no visible progress at all, because the admin surface that was
// meant to set these (admin-api's hub intake and batch dispatch) is not
// deployed. Opened up deliberately, with the trade-off accepted by the
// product owner.
//
// delivered is still NOT settable here. It is the one status that releases
// escrow immediately rather than after a window, so it stays with the buyer
// (POST /v1/orders/public/:id/confirm-delivery) or the auto-release sweep.
type UpdateOrderStatusReq struct {
	Status OrderStatus `json:"status" validate:"required,oneof=confirmed at_hub shipped cancelled"`
	Note   *string     `json:"note"`
}

// ConfirmPaymentReq is the body for POST /v1/orders/public/confirm-payment.
type ConfirmPaymentReq struct {
	PaymentRef string `json:"payment_reference" validate:"required"`
}

// ConfirmDeliveryReq is the body for POST /v1/orders/public/:id/confirm-delivery.
type ConfirmDeliveryReq struct {
	Email string `json:"email" validate:"required,email"`
}

// ReportMissingReq is the body for POST /v1/orders/public/:id/report-missing —
// a buyer flagging that a specific order within a batch (possibly already
// dispatched or delivered) never actually arrived. Same email-gated trust
// model as ConfirmDeliveryReq, since checkout has no real buyer JWT identity.
type ReportMissingReq struct {
	Email  string  `json:"email"  validate:"required,email"`
	Reason *string `json:"reason"`
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
	TotalRevenueKobo    int64 `json:"total_revenue_kobo"`
	TotalOrders         int32 `json:"total_orders"`
	TotalCustomers      int32 `json:"total_customers"`
	PendingOrders       int32 `json:"pending_orders"`
	LowStockProducts    int32 `json:"low_stock_products"`
	TotalDiscountsKobo  int64 `json:"total_discounts_kobo"`
	TotalExpensesKobo   int64 `json:"total_expenses_kobo"`
	StorefrontVisits30d int64 `json:"storefront_visits_30d"`
}

// RevenueTrendPoint is one day's aggregated revenue for the trend chart.
type RevenueTrendPoint struct {
	Date        string `json:"date"` // "2026-07-03"
	RevenueKobo int64  `json:"revenue_kobo"`
	Orders      int    `json:"orders"`
}

// TopProductResp is a single entry in the top-selling products list,
// aggregated from order_items across all of a store's orders.
type TopProductResp struct {
	ProductID   string `json:"product_id"`
	Name        string `json:"name"`
	ImageURL    string `json:"image_url,omitempty"`
	UnitsSold   int64  `json:"units_sold"`
	RevenueKobo int64  `json:"revenue_kobo"`
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
	BalanceKobo  int64                   `json:"balance_kobo"`
	TotalEarned  int64                   `json:"total_earned_kobo"`
	HeldKobo     int64                   `json:"held_kobo"` // credits still in escrow — not yet withdrawable
	Transactions []WalletTransactionResp `json:"transactions"`
}

// WithdrawReq is the body for POST /v1/wallet/withdraw.
type WithdrawReq struct {
	AmountKobo    int64  `json:"amount_kobo"    validate:"required,min=100"`
	BankName      string `json:"bank_name"      validate:"required"`
	AccountNumber string `json:"account_number" validate:"required,len=10"`
	AccountName   string `json:"account_name"   validate:"required"`
}

// ── Cart email ────────────────────────────────────────────────────────────────

// CartEmailItem is one line item for SendCartEmailReq.
type CartEmailItem struct {
	Name      string `json:"name"       validate:"required"`
	ImageURL  string `json:"image_url"`
	Quantity  int32  `json:"quantity"   validate:"required,min=1"`
	PriceKobo int64  `json:"price_kobo" validate:"min=1"`
}

// SendCartEmailReq is the body for POST /v1/orders/public/cart-email.
// Fired when the customer clicks Pay — before payment completes — so they
// have a copy of what they ordered even if the browser closes mid-flow.
type SendCartEmailReq struct {
	Email        string          `json:"email"         validate:"required,email"`
	CustomerName string          `json:"customer_name" validate:"required"`
	StoreSlug    string          `json:"store_slug"    validate:"required"`
	StoreName    string          `json:"store_name"    validate:"required"`
	Items        []CartEmailItem `json:"items"         validate:"required,min=1,dive"`
	TotalKobo    int64           `json:"total_kobo"    validate:"min=1"`
}

// ── Visit tracking ────────────────────────────────────────────────────────────

// TrackVisitReq is the body for POST /v1/orders/public/visit.
type TrackVisitReq struct {
	StoreID   string `json:"store_id"   validate:"required,uuid"`
	SessionID string `json:"session_id" validate:"required"`
	Page      string `json:"page"`
}

// ── Newsletter ────────────────────────────────────────────────────────────────

// SubscribeReq is the body for POST /v1/orders/public/subscribe.
type SubscribeReq struct {
	StoreID string `json:"store_id" validate:"required,uuid"`
	Email   string `json:"email"    validate:"required,email"`
	Name    string `json:"name"`
}

// SubscriberResp is a single newsletter subscriber.
type SubscriberResp struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	SubscribedAt string `json:"subscribed_at"`
	Unsubscribed bool   `json:"unsubscribed"`
}

// SubscriberListResp wraps a paginated subscriber list.
type SubscriberListResp struct {
	Subscribers []SubscriberResp `json:"subscribers"`
	Total       int64            `json:"total"`
	Page        int              `json:"page"`
	PerPage     int              `json:"per_page"`
}

// CreateCampaignReq is the body for POST /v1/campaigns.
type CreateCampaignReq struct {
	Subject  string `json:"subject"   validate:"required"`
	BodyHTML string `json:"body_html" validate:"required"`
}

// CampaignResp is a single email campaign.
type CampaignResp struct {
	ID              string  `json:"id"`
	Subject         string  `json:"subject"`
	Status          string  `json:"status"`
	RecipientsCount int     `json:"recipients_count"`
	CreatedAt       string  `json:"created_at"`
	SentAt          *string `json:"sent_at,omitempty"`
}

// CampaignListResp wraps a list of campaigns.
type CampaignListResp struct {
	Campaigns []CampaignResp `json:"campaigns"`
}

// ── Payment gateways ──────────────────────────────────────────────────────────

// PaymentGatewayResp describes one payment method for a store.
type PaymentGatewayResp struct {
	Gateway   string         `json:"gateway"`
	Enabled   bool           `json:"enabled"`
	Config    map[string]any `json:"config"`
	UpdatedAt string         `json:"updated_at"`
}

// UpsertPaymentGatewayReq is the body for PUT /v1/store/payment-gateways/:gateway.
type UpsertPaymentGatewayReq struct {
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config"`
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

// ── Multi-vendor checkout ─────────────────────────────────────────────────────

// CheckoutStoreOrder is one vendor's share of a multi-vendor cart.
type CheckoutStoreOrder struct {
	StoreID   string            `json:"store_id"   validate:"required,uuid"`
	StoreSlug string            `json:"store_slug"`
	StoreName string            `json:"store_name"`
	Items     []CreateOrderItem `json:"items"      validate:"required,min=1,dive"`
}

// CheckoutResp is what the consumer app receives back: the per-vendor orders
// plus the checkout-level totals.
type CheckoutResp struct {
	CheckoutID          string      `json:"checkout_id"`
	Orders              []OrderResp `json:"orders"`
	ItemsKobo           int64       `json:"items_kobo"`
	DeliveryFeeKobo     int64       `json:"delivery_fee_kobo"`
	DeliveryOptionTitle string      `json:"delivery_option_title,omitempty"`
	TotalKobo           int64       `json:"total_kobo"`
}
