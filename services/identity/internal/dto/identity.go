// Package dto defines request and response shapes for the identity service.
package dto

import "time"

// ── User profile ──────────────────────────────────────────────────────────────

// UpdateProfileReq is the body for PATCH /v1/identity/me.
// All fields are optional (PATCH semantics — omit to leave unchanged).
type UpdateProfileReq struct {
	FullName         *string `json:"full_name"`
	Phone            *string `json:"phone"            validate:"omitempty,min=7,max=20"`
	HowHeard         *string `json:"how_heard"`
	HowHeardOther    *string `json:"how_heard_other"`
	TermsAcceptedAt  *string `json:"terms_accepted_at"` // RFC3339; only accepted on first call
	MarketingConsent *bool   `json:"marketing_consent"`
}

// MeResp is returned by GET /v1/identity/me.
type MeResp struct {
	ID               string          `json:"id"`
	Email            *string         `json:"email,omitempty"`
	FullName         *string         `json:"full_name,omitempty"`
	AvatarURL        *string         `json:"avatar_url,omitempty"`
	Phone            *string         `json:"phone,omitempty"`
	IsEmailVerified  bool            `json:"is_email_verified"`
	ProfileCompleted bool            `json:"profile_completed"`
	MarketingConsent bool            `json:"marketing_consent"`
	TermsAcceptedAt  *time.Time      `json:"terms_accepted_at,omitempty"`
	HowHeard         *string         `json:"how_heard,omitempty"`
	Buyer            *BuyerSummary   `json:"buyer,omitempty"`
	Vendor           *VendorSummary  `json:"vendor,omitempty"`
}

// BuyerSummary is the lightweight buyer section of MeResp.
type BuyerSummary struct {
	ID               string  `json:"id"`
	TotalOrders      int32   `json:"total_orders"`
	TotalSpentKobo   int64   `json:"total_spent_kobo"`
	DefaultAddressID *string `json:"default_address_id,omitempty"`
}

// VendorSummary is the lightweight vendor section of MeResp.
type VendorSummary struct {
	ID             string `json:"id"`
	OnboardingStep string `json:"onboarding_step"`
	KycStatus      string `json:"kyc_status"`
	IsActive       bool   `json:"is_active"`
}

// ── Buyer addresses ───────────────────────────────────────────────────────────

// AddressReq is the body for POST and PATCH /v1/identity/me/addresses[/:id].
type AddressReq struct {
	Label       string   `json:"label"        validate:"required,min=1,max=50"`
	FullAddress string   `json:"full_address" validate:"required,min=5,max=500"`
	City        string   `json:"city"         validate:"required,min=1,max=100"`
	State       string   `json:"state"        validate:"required,min=1,max=100"`
	Latitude    *float64 `json:"latitude"     validate:"omitempty,min=-90,max=90"`
	Longitude   *float64 `json:"longitude"    validate:"omitempty,min=-180,max=180"`
}

// AddressResp is a single address returned to the client.
// full_address is returned decrypted — never log this struct.
type AddressResp struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	FullAddress string `json:"full_address"`
	City      string   `json:"city"`
	State     string   `json:"state"`
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
	IsDefault bool     `json:"is_default"`
}

// ── Vendor onboarding ─────────────────────────────────────────────────────────

// VendorBusinessReq is the body for PATCH /v1/identity/vendor/onboard/business.
type VendorBusinessReq struct {
	BusinessName    string  `json:"business_name"    validate:"required,min=2,max=200"`
	BusinessType    string  `json:"business_type"    validate:"required,oneof=sole_trader partnership limited_company ngo"`
	EmployeeRange   *string `json:"employee_range"`
	YearEstablished *int32  `json:"year_established" validate:"omitempty,min=1900,max=2100"`
	SocialUrl       *string `json:"social_url"       validate:"omitempty,url"`
}

// VendorKYCReq is the body for POST /v1/identity/vendor/onboard/kyc.
// PII fields are encrypted by the service before DB write.
// NEVER log or echo this struct — it contains raw BVN/NIN.
type VendorKYCReq struct {
	// Identity fields used for Smile ID matching (sent to Smile ID, never logged)
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
	DOB       *string `json:"dob"` // YYYY-MM-DD

	// One of bvn/nin for Tier 1 individual verification
	Bvn *string `json:"bvn" validate:"omitempty,len=11,numeric"`
	Nin *string `json:"nin" validate:"omitempty,len=11,numeric"`

	// Business fields for Tier 2 KYB
	Tin            *string `json:"tin"`
	CacNumber      *string `json:"cac_number"`
	CacDocumentUrl *string `json:"cac_document_url" validate:"omitempty,url"`

	// Optional supporting docs
	IdType        *string `json:"id_type"         validate:"omitempty,oneof=national_id passport drivers_license voters_card"`
	IdNumber      *string `json:"id_number"`
	IdDocumentUrl *string `json:"id_document_url" validate:"omitempty,url"`
	SelfieUrl     *string `json:"selfie_url"      validate:"omitempty,url"`
}

// VendorProfileResp is returned for GET /v1/identity/vendor/profile.
// BVN/NIN/IdNumber are NEVER included in responses — only their masked presence.
type VendorProfileResp struct {
	ID              string  `json:"id"`
	BusinessName    *string `json:"business_name,omitempty"`
	BusinessType    *string `json:"business_type,omitempty"`
	EmployeeRange   *string `json:"employee_range,omitempty"`
	YearEstablished *int32  `json:"year_established,omitempty"`
	SocialUrl       *string `json:"social_url,omitempty"`
	HasBvn          bool    `json:"has_bvn"`
	HasNin          bool    `json:"has_nin"`
	Tin             *string `json:"tin,omitempty"`
	CacNumber       *string `json:"cac_number,omitempty"`
	CacDocumentUrl  *string `json:"cac_document_url,omitempty"`
	IdType          *string `json:"id_type,omitempty"`
	HasIdNumber     bool    `json:"has_id_number"`
	IdDocumentUrl   *string `json:"id_document_url,omitempty"`
	SelfieUrl       *string `json:"selfie_url,omitempty"`
	KycStatus       string  `json:"kyc_status"`
	OnboardingStep  string  `json:"onboarding_step"`
	IsActive        bool    `json:"is_active"`
	ReferralCode    *string `json:"referral_code,omitempty"`
}

// ── Vendor banks ──────────────────────────────────────────────────────────────

// VendorBankReq is the body for POST /v1/identity/vendor/banks.
type VendorBankReq struct {
	BankName      string `json:"bank_name"      validate:"required,min=2,max=100"`
	BankCode      string `json:"bank_code"      validate:"required,min=2,max=10"`
	AccountNumber string `json:"account_number" validate:"required,len=10,numeric"`
	AccountName   string `json:"account_name"   validate:"required,min=2,max=200"`
}

// VendorBankResp is a single bank account in the response.
// account_number is masked (only last 4 digits shown).
type VendorBankResp struct {
	ID                  string `json:"id"`
	BankName            string `json:"bank_name"`
	BankCode            string `json:"bank_code"`
	AccountNumberMasked string `json:"account_number_masked"` // e.g. "******1234"
	AccountName         string `json:"account_name"`
	IsPrimary           bool   `json:"is_primary"`
	IsVerified          bool   `json:"is_verified"`
}

// ── Plans & Subscriptions ─────────────────────────────────────────────────────

// PlanResp is a single vendor subscription plan.
type PlanResp struct {
	ID           string   `json:"id"`
	Slug         string   `json:"slug"`
	DisplayName  string   `json:"display_name"`
	Description  string   `json:"description"`
	PriceKobo    int64    `json:"price_kobo"`
	BillingCycle string   `json:"billing_cycle"`
	ProductLimit int      `json:"product_limit"`
	StoreLimit   int      `json:"store_limit"`
	TeamLimit    int      `json:"team_limit"`
	Features     []string `json:"features"`
	SortOrder    int      `json:"sort_order"`
}

// SelectPlanReq is the body for POST /v1/identity/vendor/plan.
type SelectPlanReq struct {
	PlanID           string  `json:"plan_id"           validate:"required,uuid"`
	PaymentReference *string `json:"payment_reference"` // required for paid plans
}

// SubscriptionResp is returned for GET /v1/identity/vendor/subscription.
type SubscriptionResp struct {
	ID                  string   `json:"id"`
	PlanID              string   `json:"plan_id"`
	Plan                PlanResp `json:"plan"`
	Status              string   `json:"status"`
	PaymentReference    *string  `json:"payment_reference,omitempty"`
	CurrentPeriodStart  string   `json:"current_period_start"`
	CurrentPeriodEnd    *string  `json:"current_period_end,omitempty"`
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
