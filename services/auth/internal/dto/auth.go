// Package dto defines the request and response structs for all auth endpoints.
// Handlers parse requests into these types and return these types as JSON.
// Business logic never lives here — only shape and validation tags.
package dto

// ── Requests ──────────────────────────────────────────────────────────────────

// RegisterReq is the body for POST /v1/auth/register.
// ConfirmPassword is validated against Password in the service layer (cross-field).
type RegisterReq struct {
	FirstName        string `json:"first_name"        validate:"required,min=1,max=100"`
	LastName         string `json:"last_name"         validate:"required,min=1,max=100"`
	Email            string `json:"email"             validate:"required,email"`
	Password         string `json:"password"          validate:"required,min=8"`
	ConfirmPassword  string `json:"confirm_password"  validate:"required"`
	TermsAccepted    bool   `json:"terms_accepted"`
	MarketingConsent bool   `json:"marketing_consent"`
}

// LoginReq is the body for POST /v1/auth/login.
type LoginReq struct {
	Email    string `json:"email"    validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// OTPRequestReq is the body for POST /v1/auth/otp/request.
type OTPRequestReq struct {
	Email string `json:"email" validate:"required,email"`
}

// OTPVerifyReq is the body for POST /v1/auth/otp/verify.
type OTPVerifyReq struct {
	SessionToken string `json:"session_token" validate:"required"`
	OTP          string `json:"otp"           validate:"required,len=6,numeric"`
	DeviceID     string `json:"device_id"`
	UserAgent    string `json:"-"` // populated from the request header, not the body
	IPAddress    string `json:"-"` // populated from the request context, not the body
}

// GoogleAuthReq is the body for POST /v1/auth/oauth/google.
type GoogleAuthReq struct {
	IDToken  string `json:"id_token"  validate:"required"`
	DeviceID string `json:"device_id"`
}

// AppleAuthReq is the body for POST /v1/auth/oauth/apple.
// full_name and email are only present on the very first Apple sign-in.
// On return visits Apple sends neither — the handler must not reject their absence.
type AppleAuthReq struct {
	IdentityToken     string  `json:"identity_token"     validate:"required"`
	AuthorizationCode string  `json:"authorization_code" validate:"required"`
	Email             *string `json:"email"`
	FullName          *string `json:"full_name"`
	DeviceID          string  `json:"device_id"`
}

// ── Responses ─────────────────────────────────────────────────────────────────

// OTPRequestResp is the response for a successful OTP request.
type OTPRequestResp struct {
	SessionToken string `json:"session_token"`
	ExpiresIn    int    `json:"expires_in"` // seconds (always 600)
}

// AuthResp is returned after any successful authentication.
// The refresh token is NOT included here — it is set as an HttpOnly cookie.
type AuthResp struct {
	AccessToken string  `json:"access_token"`
	User        UserDTO `json:"user"`
}

// UserDTO is the user representation returned to clients.
// Never include raw PII (BVN, NIN, account numbers) here.
type UserDTO struct {
	ID               string  `json:"id"`
	Email            *string `json:"email,omitempty"`
	FullName         *string `json:"full_name,omitempty"`
	AvatarURL        *string `json:"avatar_url,omitempty"`
	IsEmailVerified  bool    `json:"is_email_verified"`
	ProfileCompleted bool    `json:"profile_completed"`
	IsBuyer          bool    `json:"is_buyer"`
	IsVendor         bool    `json:"is_vendor"`
}

// ValidateTokenResp is returned by POST /v1/internal/validate-token (for Envoy).
type ValidateTokenResp struct {
	UserID   string   `json:"user_id"`
	IsBuyer  bool     `json:"is_buyer"`
	IsVendor bool     `json:"is_vendor"`
	StoreIDs []string `json:"store_ids"`
}

// ErrorResp is the standard error envelope for all 4xx / 5xx responses.
type ErrorResp struct {
	Error string `json:"error"`
}

// ValidationErrorResp wraps field-level validation errors as a 422 response.
type ValidationErrorResp struct {
	Error  string       `json:"error"`
	Fields []FieldError `json:"fields,omitempty"`
}

// FieldError is a single field-level validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}
