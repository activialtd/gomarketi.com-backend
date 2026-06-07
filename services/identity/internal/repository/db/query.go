package db

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ── users ─────────────────────────────────────────────────────────────────────

const getUserByID = `
SELECT id, email, full_name, avatar_url, phone,
       is_email_verified, is_phone_verified, is_active,
       profile_completed, how_heard, how_heard_other,
       terms_accepted_at, marketing_consent, last_login_at,
       created_at, updated_at
FROM users WHERE id = $1`

func (q *Queries) GetUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	row := q.db.QueryRowContext(ctx, getUserByID, id)
	return scanUser(row)
}

type UpdateUserProfileParams struct {
	ID               uuid.UUID
	FullName         sql.NullString
	Phone            sql.NullString
	HowHeard         sql.NullString
	HowHeardOther    sql.NullString
	TermsAcceptedAt  sql.NullTime
	MarketingConsent bool
	ProfileCompleted bool
}

const updateUserProfile = `
UPDATE users SET
    full_name         = $2,
    phone             = $3,
    how_heard         = CASE WHEN $4::text IS NOT NULL THEN $4::how_heard_source ELSE how_heard END,
    how_heard_other   = $5,
    terms_accepted_at = CASE WHEN terms_accepted_at IS NULL THEN $6 ELSE terms_accepted_at END,
    marketing_consent = $7,
    profile_completed = $8,
    updated_at        = now()
WHERE id = $1
RETURNING id, email, full_name, avatar_url, phone,
          is_email_verified, is_phone_verified, is_active,
          profile_completed, how_heard, how_heard_other,
          terms_accepted_at, marketing_consent, last_login_at,
          created_at, updated_at`

func (q *Queries) UpdateUserProfile(ctx context.Context, arg UpdateUserProfileParams) (User, error) {
	row := q.db.QueryRowContext(ctx, updateUserProfile,
		arg.ID, arg.FullName, arg.Phone,
		arg.HowHeard, arg.HowHeardOther,
		arg.TermsAcceptedAt, arg.MarketingConsent, arg.ProfileCompleted,
	)
	return scanUser(row)
}

func scanUser(row *sql.Row) (User, error) {
	var u User
	err := row.Scan(
		&u.ID, &u.Email, &u.FullName, &u.AvatarUrl, &u.Phone,
		&u.IsEmailVerified, &u.IsPhoneVerified, &u.IsActive,
		&u.ProfileCompleted, &u.HowHeard, &u.HowHeardOther,
		&u.TermsAcceptedAt, &u.MarketingConsent, &u.LastLoginAt,
		&u.CreatedAt, &u.UpdatedAt,
	)
	return u, err
}

// ── buyer_profiles ────────────────────────────────────────────────────────────

const getBuyerProfileByUserID = `
SELECT id, user_id, default_address_id,
       saved_store_ids, preferred_category_ids,
       total_orders, total_spent, created_at, updated_at
FROM buyer_profiles WHERE user_id = $1`

func (q *Queries) GetBuyerProfileByUserID(ctx context.Context, userID uuid.UUID) (BuyerProfile, error) {
	row := q.db.QueryRowContext(ctx, getBuyerProfileByUserID, userID)
	var p BuyerProfile
	err := row.Scan(
		&p.ID, &p.UserID, &p.DefaultAddressID,
		pq.Array(&p.SavedStoreIDs), pq.Array(&p.PreferredCategoryIDs),
		&p.TotalOrders, &p.TotalSpent, &p.CreatedAt, &p.UpdatedAt,
	)
	return p, err
}

// ── buyer_addresses ───────────────────────────────────────────────────────────

const listAddressesByProfile = `
SELECT id, buyer_profile_id, label, full_address, city, state,
       ST_X(coordinates::geometry) AS longitude,
       ST_Y(coordinates::geometry) AS latitude,
       is_default, created_at
FROM buyer_addresses
WHERE buyer_profile_id = $1
ORDER BY is_default DESC, created_at ASC`

func (q *Queries) ListAddressesByProfile(ctx context.Context, profileID uuid.UUID) ([]BuyerAddress, error) {
	rows, err := q.db.QueryContext(ctx, listAddressesByProfile, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var addrs []BuyerAddress
	for rows.Next() {
		var a BuyerAddress
		if err = rows.Scan(
			&a.ID, &a.BuyerProfileID, &a.Label, &a.FullAddress,
			&a.City, &a.State, &a.Longitude, &a.Latitude, &a.IsDefault, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		addrs = append(addrs, a)
	}
	return addrs, rows.Err()
}

const getAddressByID = `
SELECT id, buyer_profile_id, label, full_address, city, state,
       ST_X(coordinates::geometry) AS longitude,
       ST_Y(coordinates::geometry) AS latitude,
       is_default, created_at
FROM buyer_addresses WHERE id = $1`

func (q *Queries) GetAddressByID(ctx context.Context, id uuid.UUID) (BuyerAddress, error) {
	row := q.db.QueryRowContext(ctx, getAddressByID, id)
	return scanAddress(row)
}

type CreateAddressParams struct {
	BuyerProfileID uuid.UUID
	Label          string
	FullAddress    string // pre-encrypted by service layer
	City           string
	State          string
	Longitude      sql.NullFloat64
	Latitude       sql.NullFloat64
	IsDefault      bool
}

const createAddress = `
INSERT INTO buyer_addresses (buyer_profile_id, label, full_address, city, state, coordinates, is_default)
VALUES ($1, $2, $3, $4, $5,
        CASE WHEN $6::float8 IS NOT NULL AND $7::float8 IS NOT NULL
             THEN ST_SetSRID(ST_MakePoint($6, $7), 4326)
             ELSE NULL END,
        $8)
RETURNING id, buyer_profile_id, label, full_address, city, state,
          ST_X(coordinates::geometry) AS longitude,
          ST_Y(coordinates::geometry) AS latitude,
          is_default, created_at`

func (q *Queries) CreateAddress(ctx context.Context, arg CreateAddressParams) (BuyerAddress, error) {
	row := q.db.QueryRowContext(ctx, createAddress,
		arg.BuyerProfileID, arg.Label, arg.FullAddress,
		arg.City, arg.State,
		arg.Longitude, arg.Latitude, arg.IsDefault,
	)
	return scanAddress(row)
}

type UpdateAddressParams struct {
	ID          uuid.UUID
	Label       string
	FullAddress string
	City        string
	State       string
	Longitude   sql.NullFloat64
	Latitude    sql.NullFloat64
}

const updateAddress = `
UPDATE buyer_addresses SET
    label        = $2,
    full_address = $3,
    city         = $4,
    state        = $5,
    coordinates  = CASE WHEN $6::float8 IS NOT NULL AND $7::float8 IS NOT NULL
                        THEN ST_SetSRID(ST_MakePoint($6, $7), 4326)
                        ELSE coordinates END
WHERE id = $1
RETURNING id, buyer_profile_id, label, full_address, city, state,
          ST_X(coordinates::geometry) AS longitude,
          ST_Y(coordinates::geometry) AS latitude,
          is_default, created_at`

func (q *Queries) UpdateAddress(ctx context.Context, arg UpdateAddressParams) (BuyerAddress, error) {
	row := q.db.QueryRowContext(ctx, updateAddress,
		arg.ID, arg.Label, arg.FullAddress, arg.City, arg.State, arg.Longitude, arg.Latitude,
	)
	return scanAddress(row)
}

const deleteAddress = `DELETE FROM buyer_addresses WHERE id = $1 AND buyer_profile_id = $2`

func (q *Queries) DeleteAddress(ctx context.Context, id, profileID uuid.UUID) error {
	_, err := q.db.ExecContext(ctx, deleteAddress, id, profileID)
	return err
}

const unsetDefaultAddresses = `UPDATE buyer_addresses SET is_default = false WHERE buyer_profile_id = $1`

func (q *Queries) UnsetDefaultAddresses(ctx context.Context, profileID uuid.UUID) error {
	_, err := q.db.ExecContext(ctx, unsetDefaultAddresses, profileID)
	return err
}

const setDefaultAddress = `UPDATE buyer_addresses SET is_default = true WHERE id = $1 AND buyer_profile_id = $2`

func (q *Queries) SetDefaultAddress(ctx context.Context, id, profileID uuid.UUID) error {
	_, err := q.db.ExecContext(ctx, setDefaultAddress, id, profileID)
	return err
}

const updateBuyerDefaultAddressID = `UPDATE buyer_profiles SET default_address_id = $2, updated_at = now() WHERE id = $1`

func (q *Queries) UpdateBuyerDefaultAddressID(ctx context.Context, profileID, addressID uuid.UUID) error {
	_, err := q.db.ExecContext(ctx, updateBuyerDefaultAddressID, profileID, addressID)
	return err
}

const clearBuyerDefaultAddressID = `UPDATE buyer_profiles SET default_address_id = NULL, updated_at = now() WHERE id = $1`

func (q *Queries) ClearBuyerDefaultAddressID(ctx context.Context, profileID uuid.UUID) error {
	_, err := q.db.ExecContext(ctx, clearBuyerDefaultAddressID, profileID)
	return err
}

func scanAddress(row *sql.Row) (BuyerAddress, error) {
	var a BuyerAddress
	err := row.Scan(
		&a.ID, &a.BuyerProfileID, &a.Label, &a.FullAddress,
		&a.City, &a.State, &a.Longitude, &a.Latitude, &a.IsDefault, &a.CreatedAt,
	)
	return a, err
}

// ── vendor_profiles ───────────────────────────────────────────────────────────

const getVendorProfileByUserID = `
SELECT id, user_id, business_name, business_type, employee_range, year_established,
       social_url, bvn, nin, tin, cac_number, cac_document_url,
       id_type, id_number, id_document_url, selfie_url,
       kyc_status, onboarding_step, is_active, referral_code, created_at, updated_at
FROM vendor_profiles WHERE user_id = $1`

func (q *Queries) GetVendorProfileByUserID(ctx context.Context, userID uuid.UUID) (VendorProfile, error) {
	row := q.db.QueryRowContext(ctx, getVendorProfileByUserID, userID)
	return scanVendor(row)
}

const createVendorProfile = `
INSERT INTO vendor_profiles (user_id)
VALUES ($1)
ON CONFLICT (user_id) DO NOTHING
RETURNING id, user_id, business_name, business_type, employee_range, year_established,
          social_url, bvn, nin, tin, cac_number, cac_document_url,
          id_type, id_number, id_document_url, selfie_url,
          kyc_status, onboarding_step, is_active, referral_code, created_at, updated_at`

func (q *Queries) CreateVendorProfile(ctx context.Context, userID uuid.UUID) (VendorProfile, error) {
	row := q.db.QueryRowContext(ctx, createVendorProfile, userID)
	return scanVendor(row)
}

type UpdateVendorBusinessParams struct {
	ID              uuid.UUID
	BusinessName    sql.NullString
	BusinessType    sql.NullString
	EmployeeRange   sql.NullString
	YearEstablished sql.NullInt32
	SocialUrl       sql.NullString
	OnboardingStep  string
}

const updateVendorBusiness = `
UPDATE vendor_profiles SET
    business_name    = $2,
    business_type    = CASE WHEN $3::text IS NOT NULL THEN $3::business_type ELSE business_type END,
    employee_range   = $4,
    year_established = $5,
    social_url       = $6,
    onboarding_step  = $7::onboarding_step,
    updated_at       = now()
WHERE id = $1
RETURNING id, user_id, business_name, business_type, employee_range, year_established,
          social_url, bvn, nin, tin, cac_number, cac_document_url,
          id_type, id_number, id_document_url, selfie_url,
          kyc_status, onboarding_step, is_active, referral_code, created_at, updated_at`

func (q *Queries) UpdateVendorBusiness(ctx context.Context, arg UpdateVendorBusinessParams) (VendorProfile, error) {
	row := q.db.QueryRowContext(ctx, updateVendorBusiness,
		arg.ID, arg.BusinessName, arg.BusinessType, arg.EmployeeRange,
		arg.YearEstablished, arg.SocialUrl, arg.OnboardingStep,
	)
	return scanVendor(row)
}

type UpdateVendorKYCParams struct {
	ID             uuid.UUID
	Bvn            sql.NullString
	Nin            sql.NullString
	Tin            sql.NullString
	CacNumber      sql.NullString
	CacDocumentUrl sql.NullString
	IdType         sql.NullString
	IdNumber       sql.NullString
	IdDocumentUrl  sql.NullString
	SelfieUrl      sql.NullString
	KycStatus      string
	OnboardingStep string
}

const updateVendorKYC = `
UPDATE vendor_profiles SET
    bvn              = COALESCE($2, bvn),
    nin              = COALESCE($3, nin),
    tin              = COALESCE($4, tin),
    cac_number       = COALESCE($5, cac_number),
    cac_document_url = COALESCE($6, cac_document_url),
    id_type          = COALESCE($7, id_type),
    id_number        = COALESCE($8, id_number),
    id_document_url  = COALESCE($9, id_document_url),
    selfie_url       = COALESCE($10, selfie_url),
    kyc_status       = $11::kyc_status,
    onboarding_step  = $12::onboarding_step,
    updated_at       = now()
WHERE id = $1
RETURNING id, user_id, business_name, business_type, employee_range, year_established,
          social_url, bvn, nin, tin, cac_number, cac_document_url,
          id_type, id_number, id_document_url, selfie_url,
          kyc_status, onboarding_step, is_active, referral_code, created_at, updated_at`

func (q *Queries) UpdateVendorKYC(ctx context.Context, arg UpdateVendorKYCParams) (VendorProfile, error) {
	row := q.db.QueryRowContext(ctx, updateVendorKYC,
		arg.ID, arg.Bvn, arg.Nin, arg.Tin, arg.CacNumber, arg.CacDocumentUrl,
		arg.IdType, arg.IdNumber, arg.IdDocumentUrl, arg.SelfieUrl,
		arg.KycStatus, arg.OnboardingStep,
	)
	return scanVendor(row)
}

func scanVendor(row *sql.Row) (VendorProfile, error) {
	var v VendorProfile
	err := row.Scan(
		&v.ID, &v.UserID, &v.BusinessName, &v.BusinessType, &v.EmployeeRange,
		&v.YearEstablished, &v.SocialUrl, &v.Bvn, &v.Nin, &v.Tin,
		&v.CacNumber, &v.CacDocumentUrl, &v.IdType, &v.IdNumber,
		&v.IdDocumentUrl, &v.SelfieUrl, &v.KycStatus, &v.OnboardingStep,
		&v.IsActive, &v.ReferralCode, &v.CreatedAt, &v.UpdatedAt,
	)
	return v, err
}

// ── vendor_banks ──────────────────────────────────────────────────────────────

const listBanksByVendor = `
SELECT id, vendor_profile_id, bank_name, bank_code, account_number,
       account_name, is_primary, is_verified, created_at
FROM vendor_banks WHERE vendor_profile_id = $1
ORDER BY is_primary DESC, created_at ASC`

func (q *Queries) ListBanksByVendor(ctx context.Context, vendorProfileID uuid.UUID) ([]VendorBank, error) {
	rows, err := q.db.QueryContext(ctx, listBanksByVendor, vendorProfileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var banks []VendorBank
	for rows.Next() {
		var b VendorBank
		if err = rows.Scan(
			&b.ID, &b.VendorProfileID, &b.BankName, &b.BankCode,
			&b.AccountNumber, &b.AccountName, &b.IsPrimary, &b.IsVerified, &b.CreatedAt,
		); err != nil {
			return nil, err
		}
		banks = append(banks, b)
	}
	return banks, rows.Err()
}

const getVendorBankByID = `
SELECT id, vendor_profile_id, bank_name, bank_code, account_number,
       account_name, is_primary, is_verified, created_at
FROM vendor_banks WHERE id = $1`

func (q *Queries) GetVendorBankByID(ctx context.Context, id uuid.UUID) (VendorBank, error) {
	row := q.db.QueryRowContext(ctx, getVendorBankByID, id)
	return scanBank(row)
}

type CreateVendorBankParams struct {
	VendorProfileID uuid.UUID
	BankName        string
	BankCode        string
	AccountNumber   string // pre-encrypted
	AccountName     string
	IsPrimary       bool
}

const createVendorBank = `
INSERT INTO vendor_banks (vendor_profile_id, bank_name, bank_code, account_number, account_name, is_primary)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, vendor_profile_id, bank_name, bank_code, account_number,
          account_name, is_primary, is_verified, created_at`

func (q *Queries) CreateVendorBank(ctx context.Context, arg CreateVendorBankParams) (VendorBank, error) {
	row := q.db.QueryRowContext(ctx, createVendorBank,
		arg.VendorProfileID, arg.BankName, arg.BankCode,
		arg.AccountNumber, arg.AccountName, arg.IsPrimary,
	)
	return scanBank(row)
}

const unsetPrimaryBanks = `UPDATE vendor_banks SET is_primary = false WHERE vendor_profile_id = $1`

func (q *Queries) UnsetPrimaryBanks(ctx context.Context, vendorProfileID uuid.UUID) error {
	_, err := q.db.ExecContext(ctx, unsetPrimaryBanks, vendorProfileID)
	return err
}

const setPrimaryBank = `UPDATE vendor_banks SET is_primary = true WHERE id = $1 AND vendor_profile_id = $2`

func (q *Queries) SetPrimaryBank(ctx context.Context, id, vendorProfileID uuid.UUID) error {
	_, err := q.db.ExecContext(ctx, setPrimaryBank, id, vendorProfileID)
	return err
}

const deleteVendorBank = `DELETE FROM vendor_banks WHERE id = $1 AND vendor_profile_id = $2`

func (q *Queries) DeleteVendorBank(ctx context.Context, id, vendorProfileID uuid.UUID) error {
	_, err := q.db.ExecContext(ctx, deleteVendorBank, id, vendorProfileID)
	return err
}

func scanBank(row *sql.Row) (VendorBank, error) {
	var b VendorBank
	err := row.Scan(
		&b.ID, &b.VendorProfileID, &b.BankName, &b.BankCode,
		&b.AccountNumber, &b.AccountName, &b.IsPrimary, &b.IsVerified, &b.CreatedAt,
	)
	return b, err
}

// countBanksByVendor is used to decide whether a new bank should auto-become primary.
const countBanksByVendor = `SELECT COUNT(*) FROM vendor_banks WHERE vendor_profile_id = $1`

func (q *Queries) CountBanksByVendor(ctx context.Context, vendorProfileID uuid.UUID) (int64, error) {
	var n int64
	err := q.db.QueryRowContext(ctx, countBanksByVendor, vendorProfileID).Scan(&n)
	return n, err
}

// countAddressesByProfile is used to decide whether a new address should auto-become default.
const countAddressesByProfile = `SELECT COUNT(*) FROM buyer_addresses WHERE buyer_profile_id = $1`

func (q *Queries) CountAddressesByProfile(ctx context.Context, profileID uuid.UUID) (int64, error) {
	var n int64
	err := q.db.QueryRowContext(ctx, countAddressesByProfile, profileID).Scan(&n)
	return n, err
}

// getDefaultAddressIDForProfile fetches the current default address id (nullable).
const getDefaultAddressIDForProfile = `
SELECT default_address_id FROM buyer_profiles WHERE id = $1`

func (q *Queries) GetDefaultAddressIDForProfile(ctx context.Context, profileID uuid.UUID) (uuid.NullUUID, error) {
	var id uuid.NullUUID
	err := q.db.QueryRowContext(ctx, getDefaultAddressIDForProfile, profileID).Scan(&id)
	return id, err
}

// getAddressCount returns the number of addresses for a profile after deleting one,
// used to clear default_address_id on buyer_profiles when no addresses remain.
const getAddressCount = `SELECT COUNT(*) FROM buyer_addresses WHERE buyer_profile_id = $1`

func (q *Queries) GetAddressCount(ctx context.Context, profileID uuid.UUID) (int64, error) {
	var n int64
	err := q.db.QueryRowContext(ctx, getAddressCount, profileID).Scan(&n)
	return n, err
}
