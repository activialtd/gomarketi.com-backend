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
