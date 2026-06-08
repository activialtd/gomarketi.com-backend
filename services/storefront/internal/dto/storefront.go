// Package dto defines request and response shapes for the storefront service.
package dto

// ── Store ──────────────────────────────────────────────────────────────────────

// CreateStoreReq is the body for POST /v1/storefront/stores.
// Submitted by the vendor-web StoreSetupForm after initial vendor onboarding.
type CreateStoreReq struct {
	Name       string `json:"name"        validate:"required,min=2,max=200"`
	Slug       string `json:"slug"        validate:"required,min=2,max=40"`
	Category   string `json:"category"    validate:"required,oneof=fashion beauty food electronics home health sports books auto kids jewelry digital agriculture art other"`
	Currency   string `json:"currency"    validate:"required,oneof=NGN"`
	TeamSize   string `json:"team_size"   validate:"required,oneof=solo 2-10 11-50 51-200 200+"`
	StaffRange string `json:"staff_range" validate:"required,oneof=1-3 3-5 5-15 15-30 30-50 50-100 100+"`
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
	IsActive     bool    `json:"is_active"`
	CreatedAt    string  `json:"created_at"`
}

// SlugCheckResp is returned by GET /v1/storefront/slugs/check.
type SlugCheckResp struct {
	Slug      string `json:"slug"`
	Available bool   `json:"available"`
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
