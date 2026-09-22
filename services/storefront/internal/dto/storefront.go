// Package dto defines request and response shapes for the storefront service.
package dto

import "encoding/json"

// ── Store ──────────────────────────────────────────────────────────────────────

// CreateStoreReq is the body for POST /v1/storefront/stores.
// Submitted by the vendor-web StoreSetupForm after initial vendor onboarding.
type CreateStoreReq struct {
	Name         string  `json:"name"          validate:"required,min=2,max=200"`
	Slug         string  `json:"slug"          validate:"required,min=2,max=40"`
	Category     string  `json:"category"      validate:"required,oneof=fashion beauty food electronics home health sports books auto kids jewelry digital agriculture art other"`
	Currency     string  `json:"currency"      validate:"required,oneof=NGN USD"`
	TeamSize     *string `json:"team_size"     validate:"omitempty,oneof=solo 2-10 11-50 51-200 200+"`
	SupportPhone *string `json:"support_phone" validate:"omitempty,min=7,max=20"`
	MarketID     *string `json:"market_id"     validate:"omitempty,uuid"`
}

// UpdateStoreReq is the body for PATCH /v1/storefront/stores/:id.
// All fields are optional (PATCH semantics — omit to leave unchanged).
type UpdateStoreReq struct {
	Name            *string         `json:"name"             validate:"omitempty,min=2,max=200"`
	Tagline         *string         `json:"tagline"          validate:"omitempty,max=300"`
	LogoURL         *string         `json:"logo_url"         validate:"omitempty,url"`
	HeroImageURL    *string         `json:"hero_image_url"   validate:"omitempty,url"`
	SiteDescription *string         `json:"site_description" validate:"omitempty,max=1000"`
	SocialLinks     json.RawMessage `json:"social_links"`
	SupportPhone    *string         `json:"support_phone"    validate:"omitempty,min=7,max=20"`
	Address         *string         `json:"address"          validate:"omitempty,max=500"`
	City            *string         `json:"city"             validate:"omitempty,max=100"`
	State           *string         `json:"state"            validate:"omitempty,max=100"`
	MarketID        *string         `json:"market_id"        validate:"omitempty,uuid"`
	ThemeConfig     json.RawMessage `json:"theme_config"` // raw JSON, stored as JSONB
}

// StoreResp is returned for any store read or write operation.
type StoreResp struct {
	ID                 string          `json:"id"`
	VendorID           string          `json:"vendor_id"`
	Name               string          `json:"name"`
	Slug               string          `json:"slug"`
	Category           string          `json:"category"`
	Currency           string          `json:"currency"`
	TeamSize           *string         `json:"team_size,omitempty"`
	StaffRange         *string         `json:"staff_range,omitempty"`
	Tagline            *string         `json:"tagline,omitempty"`
	LogoURL            *string         `json:"logo_url,omitempty"`
	HeroImageURL       *string         `json:"hero_image_url,omitempty"`
	SiteDescription    *string         `json:"site_description,omitempty"`
	SocialLinks        json.RawMessage `json:"social_links,omitempty"`
	SupportPhone       *string         `json:"support_phone,omitempty"`
	Address            *string         `json:"address,omitempty"`
	City               *string         `json:"city,omitempty"`
	State              *string         `json:"state,omitempty"`
	MarketID           *string         `json:"market_id,omitempty"`
	MarketName         *string         `json:"market_name,omitempty"`
	CustomDomain       *string         `json:"custom_domain,omitempty"`
	CustomDomainStatus string          `json:"custom_domain_status,omitempty"`
	ThemeConfig        json.RawMessage `json:"theme_config,omitempty"` // raw JSON
	IsActive           bool            `json:"is_active"`
	CreatedAt          string          `json:"created_at"`
	// DeliveryOptions is populated on public store reads so checkout can
	// render the vendor's own delivery choices. Omitted on vendor reads.
	DeliveryOptions []DeliveryOptionResp `json:"delivery_options,omitempty"`
}

// SlugCheckResp is returned by GET /v1/storefront/slugs/check.
type SlugCheckResp struct {
	Slug      string `json:"slug"`
	Available bool   `json:"available"`
}

// PresignUploadReq is the body for POST /v1/storefront/uploads/presign.
type PresignUploadReq struct {
	Filename    string `json:"filename"     validate:"required"`
	ContentType string `json:"content_type" validate:"required"`
	Size        int64  `json:"size"         validate:"required,min=1,max=10485760"` // 10 MB max
	Purpose     string `json:"purpose"      validate:"omitempty,oneof=products collections logo banners documents"`
}

// PresignUploadResp is returned by POST /v1/storefront/uploads/presign.
//
// Cloudinary takes a multipart POST rather than a presigned PUT: send `file`
// plus every entry in Fields to UploadURL, then read `secure_url` off the
// reply. PublicURL is therefore not known in advance and stays empty.
type PresignUploadResp struct {
	UploadURL string            `json:"upload_url"`
	PublicURL string            `json:"public_url,omitempty"`
	Key       string            `json:"key"`
	ExpiresIn int               `json:"expires_in"` // seconds
	Provider  string            `json:"provider"`   // "cloudinary"
	Fields    map[string]string `json:"fields"`
}

// LogViewReq is the body for POST /v1/storefront/public/log.
type LogViewReq struct {
	StoreSlug string `json:"slug"     validate:"required"`
	Path      string `json:"path"     validate:"required"`
	Referrer  string `json:"referrer"`
}

// StoreViewsResp is returned by GET /v1/storefront/stores/:id/views.
type StoreViewsResp struct {
	StoreID  string `json:"store_id"`
	Views30d int64  `json:"views_30d"`
	Views7d  int64  `json:"views_7d"`
	ViewsAll int64  `json:"views_all"`
}

// ── Staff ──────────────────────────────────────────────────────────────────────

// InviteStaffReq is the body for POST /v1/storefront/stores/:id/staff.
type InviteStaffReq struct {
	Email string `json:"email" validate:"required,email"`
	Role  string `json:"role"  validate:"required,oneof=manager staff"`
}

// StaffMemberResp is a single staff member in the response.
type StaffMemberResp struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	FullName  string `json:"full_name"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	InvitedAt string `json:"invited_at"`
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

// ── Markets ───────────────────────────────────────────────────────────────────

// MarketResp is a single market in GET /v1/storefront/public/markets.
type MarketResp struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	City  string `json:"city"`
	State string `json:"state"`
}

// StoreSearchResult is a single store in a public search response. It is a
// trimmed StoreResp — enough for a result card, no private fields.
type StoreSearchResult struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Slug       string  `json:"slug"`
	Category   string  `json:"category"`
	Tagline    *string `json:"tagline,omitempty"`
	LogoURL    *string `json:"logo_url,omitempty"`
	HeroImage  *string `json:"hero_image_url,omitempty"`
	Address    *string `json:"address,omitempty"`
	City       *string `json:"city,omitempty"`
	State      *string `json:"state,omitempty"`
	MarketID   *string `json:"market_id,omitempty"`
	MarketName *string `json:"market_name,omitempty"`
}

// StoreSearchResp is returned by GET /v1/storefront/public/stores/search.
// MatchType tells the client which tier resolved the query so it can word the
// results header: a named vendor, a named market, a city, or no match.
type StoreSearchResp struct {
	Stores          []StoreSearchResult `json:"stores"`
	HasMore         bool                `json:"has_more"`
	MatchType       string              `json:"match_type"`
	MatchedStoreID  *string             `json:"matched_store_id,omitempty"`
	MatchedMarketID *string             `json:"matched_market_id,omitempty"`
	RemainingQuery  string              `json:"remaining_query"`
}

// ── Delivery options ──────────────────────────────────────────────────────────

// DeliveryOptionResp is a single vendor-defined delivery choice.
type DeliveryOptionResp struct {
	ID          string `json:"id"`
	StoreID     string `json:"store_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	PriceKobo   int64  `json:"price_kobo"`
	Position    int    `json:"position"`
	IsActive    bool   `json:"is_active"`
	CreatedAt   string `json:"created_at"`
}

// CreateDeliveryOptionReq is the body for
// POST /v1/storefront/stores/:id/delivery-options.
type CreateDeliveryOptionReq struct {
	Title       string `json:"title"       validate:"required,min=2,max=120"`
	Description string `json:"description" validate:"omitempty,max=500"`
	PriceKobo   int64  `json:"price_kobo"  validate:"min=0,max=100000000"`
	Position    *int   `json:"position"    validate:"omitempty,min=0,max=1000"`
}

// UpdateDeliveryOptionReq is the body for
// PATCH /v1/storefront/stores/:id/delivery-options/:option_id.
// All fields optional — omit to leave unchanged.
type UpdateDeliveryOptionReq struct {
	Title       *string `json:"title"       validate:"omitempty,min=2,max=120"`
	Description *string `json:"description" validate:"omitempty,max=500"`
	PriceKobo   *int64  `json:"price_kobo"  validate:"omitempty,min=0,max=100000000"`
	Position    *int    `json:"position"    validate:"omitempty,min=0,max=1000"`
	IsActive    *bool   `json:"is_active"`
}
