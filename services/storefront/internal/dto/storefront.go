// Package dto defines request and response shapes for the storefront service.
package dto

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
}

// UpdateStoreReq is the body for PATCH /v1/storefront/stores/:id.
// All fields are optional (PATCH semantics — omit to leave unchanged).
type UpdateStoreReq struct {
	Name         *string `json:"name"          validate:"omitempty,min=2,max=200"`
	Tagline      *string `json:"tagline"       validate:"omitempty,max=300"`
	LogoURL      *string `json:"logo_url"      validate:"omitempty,url"`
	SupportPhone *string `json:"support_phone" validate:"omitempty,min=7,max=20"`
	Address      *string `json:"address"       validate:"omitempty,max=500"`
	City         *string `json:"city"          validate:"omitempty,max=100"`
	State        *string `json:"state"         validate:"omitempty,max=100"`
	ThemeConfig  *string `json:"theme_config,omitempty"` // raw JSON, stored as JSONB
}

// StoreResp is returned for any store read or write operation.
type StoreResp struct {
	ID           string  `json:"id"`
	VendorID     string  `json:"vendor_id"`
	Name         string  `json:"name"`
	Slug         string  `json:"slug"`
	Category     string  `json:"category"`
	Currency     string  `json:"currency"`
	TeamSize     *string `json:"team_size,omitempty"`
	StaffRange   *string `json:"staff_range,omitempty"`
	Tagline      *string `json:"tagline,omitempty"`
	LogoURL      *string `json:"logo_url,omitempty"`
	SupportPhone *string `json:"support_phone,omitempty"`
	Address      *string `json:"address,omitempty"`
	City         *string `json:"city,omitempty"`
	State        *string `json:"state,omitempty"`
	CustomDomain       *string `json:"custom_domain,omitempty"`
	CustomDomainStatus string  `json:"custom_domain_status,omitempty"`
	ThemeConfig        *string `json:"theme_config,omitempty"` // raw JSON
	IsActive           bool    `json:"is_active"`
	CreatedAt          string  `json:"created_at"`
}

// SlugCheckResp is returned by GET /v1/storefront/slugs/check.
type SlugCheckResp struct {
	Slug      string `json:"slug"`
	Available bool   `json:"available"`
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
