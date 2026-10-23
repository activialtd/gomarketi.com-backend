package dto

// ── Price offers ──────────────────────────────────────────────────────────────

// OfferStatus says whose move it is in a negotiation, or how it ended.
type OfferStatus string

const (
	// OfferPending is a buyer's offer waiting on the vendor.
	OfferPending OfferStatus = "pending"
	// OfferCountered is a vendor's counter waiting on the buyer.
	OfferCountered OfferStatus = "countered"
	// OfferAccepted is a deal: the buyer may check out at AgreedKobo until
	// ExpiresAt.
	OfferAccepted  OfferStatus = "accepted"
	OfferDeclined  OfferStatus = "declined"
	OfferExpired   OfferStatus = "expired"
	OfferRedeemed  OfferStatus = "redeemed"
	OfferWithdrawn OfferStatus = "withdrawn"
)

// OfferEvent is one move in a negotiation, oldest first in OfferResp.Events.
type OfferEvent struct {
	Actor      string  `json:"actor"`
	Action     string  `json:"action"`
	AmountKobo *int64  `json:"amount_kobo,omitempty"`
	Message    *string `json:"message,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

// OfferResp is a negotiation as either side sees it. MinPriceKobo and
// BelowFloor are filled only for the vendor — a buyer who saw the floor
// would simply offer it.
type OfferResp struct {
	ID              string       `json:"id"`
	StoreID         string       `json:"store_id"`
	StoreSlug       string       `json:"store_slug,omitempty"`
	StoreName       string       `json:"store_name,omitempty"`
	ProductID       string       `json:"product_id"`
	ProductName     string       `json:"product_name"`
	ProductImage    *string      `json:"product_image,omitempty"`
	ListPriceKobo   int64        `json:"list_price_kobo"`
	OfferKobo       int64        `json:"offer_kobo"`
	CounterKobo     *int64       `json:"counter_kobo,omitempty"`
	AgreedKobo      *int64       `json:"agreed_kobo,omitempty"`
	Quantity        int32        `json:"quantity"`
	Status          OfferStatus  `json:"status"`
	AutoAccepted    bool         `json:"auto_accepted"`
	BuyerOffersLeft int          `json:"buyer_offers_left"`
	CustomerName    string       `json:"customer_name"`
	CustomerEmail   string       `json:"customer_email"`
	CustomerPhone   *string      `json:"customer_phone,omitempty"`
	Message         *string      `json:"message,omitempty"`
	ExpiresAt       string       `json:"expires_at"`
	CreatedAt       string       `json:"created_at"`
	UpdatedAt       string       `json:"updated_at"`
	Events          []OfferEvent `json:"events"`
	MinPriceKobo    *int64       `json:"min_price_kobo,omitempty"`
	BelowFloor      *bool        `json:"below_floor,omitempty"`
}

// CreateOfferReq is the body for POST /v1/orders/public/offers.
type CreateOfferReq struct {
	ProductID     string `json:"product_id"     validate:"required,uuid"`
	AmountKobo    int64  `json:"amount_kobo"    validate:"required,min=1"`
	Quantity      int32  `json:"quantity"       validate:"omitempty,min=1,max=10"`
	CustomerName  string `json:"customer_name"  validate:"required,max=200"`
	CustomerEmail string `json:"customer_email" validate:"required,email"`
	CustomerPhone string `json:"customer_phone" validate:"omitempty,max=30"`
	Message       string `json:"message"        validate:"omitempty,max=500"`
}

// CreateOfferResp hands the guest buyer the token that lets them see and
// answer the negotiation later. It is returned once and never again.
type CreateOfferResp struct {
	Offer      OfferResp `json:"offer"`
	BuyerToken string    `json:"buyer_token"`
}

// RespondOfferReq is the body for a move on an open negotiation, from
// either side. AmountKobo is required for a counter.
type RespondOfferReq struct {
	Action     string `json:"action"      validate:"required,oneof=accept counter decline withdraw"`
	AmountKobo int64  `json:"amount_kobo" validate:"omitempty,min=1"`
	Message    string `json:"message"     validate:"omitempty,max=500"`
}

// OfferCounts is the number of offers behind each tab of the vendor's inbox.
type OfferCounts struct {
	Pending   int64 `json:"pending"   db:"pending"`
	Countered int64 `json:"countered" db:"countered"`
	Accepted  int64 `json:"accepted"  db:"accepted"`
	Closed    int64 `json:"closed"    db:"closed"`
}

// OfferListResp is the vendor's offers inbox.
type OfferListResp struct {
	Offers  []OfferResp `json:"offers"`
	Total   int64       `json:"total"`
	Page    int         `json:"page"`
	PerPage int         `json:"per_page"`
	Counts  OfferCounts `json:"counts"`
}

// MyOffersResp is a signed-in buyer's negotiations across every store.
type MyOffersResp struct {
	Offers []OfferResp `json:"offers"`
}
