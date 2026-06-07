package db

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// User mirrors the users table.
type User struct {
	ID               uuid.UUID      `db:"id"`
	Email            sql.NullString `db:"email"`
	FullName         sql.NullString `db:"full_name"`
	AvatarUrl        sql.NullString `db:"avatar_url"`
	Phone            sql.NullString `db:"phone"`
	IsEmailVerified  bool           `db:"is_email_verified"`
	IsPhoneVerified  bool           `db:"is_phone_verified"`
	IsActive         bool           `db:"is_active"`
	ProfileCompleted bool           `db:"profile_completed"`
	HowHeard         sql.NullString `db:"how_heard"`
	HowHeardOther    sql.NullString `db:"how_heard_other"`
	TermsAcceptedAt  sql.NullTime   `db:"terms_accepted_at"`
	MarketingConsent bool           `db:"marketing_consent"`
	LastLoginAt      sql.NullTime   `db:"last_login_at"`
	CreatedAt        time.Time      `db:"created_at"`
	UpdatedAt        time.Time      `db:"updated_at"`
}

// BuyerProfile mirrors buyer_profiles (array columns scanned via pq.Array).
type BuyerProfile struct {
	ID                   uuid.UUID      `db:"id"`
	UserID               uuid.UUID      `db:"user_id"`
	DefaultAddressID     uuid.NullUUID  `db:"default_address_id"`
	SavedStoreIDs        []string       // scanned via pq.Array
	PreferredCategoryIDs []string       // scanned via pq.Array
	TotalOrders          int32          `db:"total_orders"`
	TotalSpent           int64          `db:"total_spent"`
	CreatedAt            time.Time      `db:"created_at"`
	UpdatedAt            time.Time      `db:"updated_at"`
}

// BuyerAddress mirrors buyer_addresses.
// Longitude and Latitude are extracted by ST_X / ST_Y in queries.
type BuyerAddress struct {
	ID            uuid.UUID      `db:"id"`
	BuyerProfileID uuid.UUID     `db:"buyer_profile_id"`
	Label         string         `db:"label"`
	FullAddress   string         `db:"full_address"` // AES-256-GCM encrypted in DB
	City          string         `db:"city"`
	State         string         `db:"state"`
	Longitude     sql.NullFloat64 `db:"longitude"`
	Latitude      sql.NullFloat64 `db:"latitude"`
	IsDefault     bool           `db:"is_default"`
	CreatedAt     time.Time      `db:"created_at"`
}

// VendorProfile mirrors vendor_profiles.
// PII fields (Bvn, Nin, IdNumber) are AES-256-GCM encrypted in DB.
type VendorProfile struct {
	ID              uuid.UUID      `db:"id"`
	UserID          uuid.UUID      `db:"user_id"`
	BusinessName    sql.NullString `db:"business_name"`
	BusinessType    sql.NullString `db:"business_type"`
	EmployeeRange   sql.NullString `db:"employee_range"`
	YearEstablished sql.NullInt32  `db:"year_established"`
	SocialUrl       sql.NullString `db:"social_url"`
	Bvn             sql.NullString `db:"bvn"`      // encrypted
	Nin             sql.NullString `db:"nin"`      // encrypted
	Tin             sql.NullString `db:"tin"`
	CacNumber       sql.NullString `db:"cac_number"`
	CacDocumentUrl  sql.NullString `db:"cac_document_url"`
	IdType          sql.NullString `db:"id_type"`
	IdNumber        sql.NullString `db:"id_number"` // encrypted
	IdDocumentUrl   sql.NullString `db:"id_document_url"`
	SelfieUrl       sql.NullString `db:"selfie_url"`
	KycStatus       string         `db:"kyc_status"`
	OnboardingStep  string         `db:"onboarding_step"`
	IsActive        bool           `db:"is_active"`
	ReferralCode    sql.NullString `db:"referral_code"`
	CreatedAt       time.Time      `db:"created_at"`
	UpdatedAt       time.Time      `db:"updated_at"`
}

// VendorBank mirrors vendor_banks.
// AccountNumber is AES-256-GCM encrypted in DB.
type VendorBank struct {
	ID              uuid.UUID `db:"id"`
	VendorProfileID uuid.UUID `db:"vendor_profile_id"`
	BankName        string    `db:"bank_name"`
	BankCode        string    `db:"bank_code"`
	AccountNumber   string    `db:"account_number"` // encrypted
	AccountName     string    `db:"account_name"`
	IsPrimary       bool      `db:"is_primary"`
	IsVerified      bool      `db:"is_verified"`
	CreatedAt       time.Time `db:"created_at"`
}
