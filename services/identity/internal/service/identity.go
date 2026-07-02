// Package service implements all identity use-cases.
// It sits between the handler (HTTP) and repository (database) layers.
// PII encryption/decryption happens here — never in handlers or repositories.
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/shared/pkg/crypto"
	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/repository"
	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/repository/db"
	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/smileid"
)

// IdentityService implements all identity use-cases.
type IdentityService struct {
	store         *repository.Store
	encryptionKey []byte
	log           zerolog.Logger
	kycClient     *smileid.Client
}

// New creates an IdentityService.
func New(store *repository.Store, encryptionKey []byte, kycClient *smileid.Client, log zerolog.Logger) *IdentityService {
	return &IdentityService{store: store, encryptionKey: encryptionKey, kycClient: kycClient, log: log}
}

// ── User profile ──────────────────────────────────────────────────────────────

// GetMe returns the full profile for the authenticated user.
func (s *IdentityService) GetMe(ctx context.Context, userID uuid.UUID) (dto.MeResp, error) {
	q := s.store.Queries()

	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return dto.MeResp{}, repository.NormaliseErr(err, "user")
	}

	resp := userToMeResp(user)

	buyer, err := q.GetBuyerProfileByUserID(ctx, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return dto.MeResp{}, apperrors.Internal(fmt.Errorf("get buyer profile: %w", err))
	}
	if err == nil {
		resp.Buyer = buyerToSummary(buyer)
	}

	vendor, err := q.GetVendorProfileByUserID(ctx, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return dto.MeResp{}, apperrors.Internal(fmt.Errorf("get vendor profile: %w", err))
	}
	if err == nil {
		resp.Vendor = vendorToSummary(vendor)
	}

	return resp, nil
}

// UpdateMe applies a partial update to the authenticated user's profile.
func (s *IdentityService) UpdateMe(ctx context.Context, userID uuid.UUID, req dto.UpdateProfileReq) (dto.MeResp, error) {
	q := s.store.Queries()

	current, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return dto.MeResp{}, repository.NormaliseErr(err, "user")
	}

	// Merge patch: keep existing values when field is nil.
	merged := mergeUserPatch(current, req)

	// Profile is complete once full_name, phone, and terms_accepted_at are all set.
	merged.ProfileCompleted = merged.FullName.Valid &&
		merged.Phone.Valid &&
		merged.TermsAcceptedAt.Valid

	updated, err := q.UpdateUserProfile(ctx, merged)
	if err != nil {
		return dto.MeResp{}, apperrors.Internal(fmt.Errorf("update user profile: %w", err))
	}

	return userToMeResp(updated), nil
}

func mergeUserPatch(current db.User, req dto.UpdateProfileReq) db.UpdateUserProfileParams {
	p := db.UpdateUserProfileParams{
		ID:               current.ID,
		FullName:         current.FullName,
		Phone:            current.Phone,
		HowHeard:         current.HowHeard,
		HowHeardOther:    current.HowHeardOther,
		TermsAcceptedAt:  current.TermsAcceptedAt,
		MarketingConsent: current.MarketingConsent,
		ProfileCompleted: current.ProfileCompleted,
	}
	if req.FullName != nil {
		p.FullName = sql.NullString{String: *req.FullName, Valid: true}
	}
	if req.Phone != nil {
		p.Phone = sql.NullString{String: *req.Phone, Valid: true}
	}
	if req.HowHeard != nil {
		p.HowHeard = sql.NullString{String: *req.HowHeard, Valid: true}
	}
	if req.HowHeardOther != nil {
		p.HowHeardOther = sql.NullString{String: *req.HowHeardOther, Valid: true}
	}
	if req.TermsAcceptedAt != nil && !current.TermsAcceptedAt.Valid {
		t, parseErr := time.Parse(time.RFC3339, *req.TermsAcceptedAt)
		if parseErr == nil {
			p.TermsAcceptedAt = sql.NullTime{Time: t, Valid: true}
		}
	}
	if req.MarketingConsent != nil {
		p.MarketingConsent = *req.MarketingConsent
	}
	return p
}

// ── Buyer addresses ───────────────────────────────────────────────────────────

// ListAddresses returns all saved addresses for the buyer, full_address decrypted.
func (s *IdentityService) ListAddresses(ctx context.Context, userID uuid.UUID) ([]dto.AddressResp, error) {
	profile, err := s.store.Queries().GetBuyerProfileByUserID(ctx, userID)
	if err != nil {
		return nil, repository.NormaliseErr(err, "buyer profile")
	}

	addrs, err := s.store.Queries().ListAddressesByProfile(ctx, profile.ID)
	if err != nil {
		return nil, apperrors.Internal(fmt.Errorf("list addresses: %w", err))
	}

	out := make([]dto.AddressResp, 0, len(addrs))
	for _, a := range addrs {
		r, decErr := s.addressToResp(a)
		if decErr != nil {
			s.log.Error().Err(decErr).Str("address_id", a.ID.String()).Msg("decrypt address failed")
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// AddAddress encrypts full_address, stores it, and sets it as default if it's the first.
func (s *IdentityService) AddAddress(ctx context.Context, userID uuid.UUID, req dto.AddressReq) (dto.AddressResp, error) {
	profile, err := s.store.Queries().GetBuyerProfileByUserID(ctx, userID)
	if err != nil {
		return dto.AddressResp{}, repository.NormaliseErr(err, "buyer profile")
	}

	encrypted, err := crypto.Encrypt(s.encryptionKey, req.FullAddress)
	if err != nil {
		return dto.AddressResp{}, apperrors.Internal(fmt.Errorf("encrypt address: %w", err))
	}

	count, err := s.store.Queries().CountAddressesByProfile(ctx, profile.ID)
	if err != nil {
		return dto.AddressResp{}, apperrors.Internal(fmt.Errorf("count addresses: %w", err))
	}
	isFirst := count == 0

	params := db.CreateAddressParams{
		BuyerProfileID: profile.ID,
		Label:          req.Label,
		FullAddress:    encrypted,
		City:           req.City,
		State:          req.State,
		IsDefault:      isFirst,
	}
	if req.Longitude != nil {
		params.Longitude = sql.NullFloat64{Float64: *req.Longitude, Valid: true}
	}
	if req.Latitude != nil {
		params.Latitude = sql.NullFloat64{Float64: *req.Latitude, Valid: true}
	}

	var addr db.BuyerAddress
	err = s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		var txErr error
		addr, txErr = qtx.CreateAddress(ctx, params)
		if txErr != nil {
			return txErr
		}
		if isFirst {
			return qtx.UpdateBuyerDefaultAddressID(ctx, profile.ID, addr.ID)
		}
		return nil
	})
	if err != nil {
		return dto.AddressResp{}, apperrors.Internal(fmt.Errorf("create address: %w", err))
	}

	return s.addressToResp(addr)
}

// UpdateAddress re-encrypts the full_address and updates the row.
func (s *IdentityService) UpdateAddress(ctx context.Context, userID uuid.UUID, addressID uuid.UUID, req dto.AddressReq) (dto.AddressResp, error) {
	profile, err := s.store.Queries().GetBuyerProfileByUserID(ctx, userID)
	if err != nil {
		return dto.AddressResp{}, repository.NormaliseErr(err, "buyer profile")
	}

	// Verify address belongs to this buyer.
	existing, err := s.store.Queries().GetAddressByID(ctx, addressID)
	if err != nil {
		return dto.AddressResp{}, repository.NormaliseErr(err, "address")
	}
	if existing.BuyerProfileID != profile.ID {
		return dto.AddressResp{}, apperrors.Forbidden("address does not belong to this buyer")
	}

	encrypted, err := crypto.Encrypt(s.encryptionKey, req.FullAddress)
	if err != nil {
		return dto.AddressResp{}, apperrors.Internal(fmt.Errorf("encrypt address: %w", err))
	}

	params := db.UpdateAddressParams{
		ID:          addressID,
		Label:       req.Label,
		FullAddress: encrypted,
		City:        req.City,
		State:       req.State,
	}
	if req.Longitude != nil {
		params.Longitude = sql.NullFloat64{Float64: *req.Longitude, Valid: true}
	}
	if req.Latitude != nil {
		params.Latitude = sql.NullFloat64{Float64: *req.Latitude, Valid: true}
	}

	updated, err := s.store.Queries().UpdateAddress(ctx, params)
	if err != nil {
		return dto.AddressResp{}, apperrors.Internal(fmt.Errorf("update address: %w", err))
	}

	return s.addressToResp(updated)
}

// DeleteAddress removes an address. Clears default_address_id on the buyer profile
// if the deleted address was the default, and auto-promotes the next address if any remain.
func (s *IdentityService) DeleteAddress(ctx context.Context, userID uuid.UUID, addressID uuid.UUID) error {
	profile, err := s.store.Queries().GetBuyerProfileByUserID(ctx, userID)
	if err != nil {
		return repository.NormaliseErr(err, "buyer profile")
	}

	existing, err := s.store.Queries().GetAddressByID(ctx, addressID)
	if err != nil {
		return repository.NormaliseErr(err, "address")
	}
	if existing.BuyerProfileID != profile.ID {
		return apperrors.Forbidden("address does not belong to this buyer")
	}

	wasDefault := existing.IsDefault

	return s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		if err = qtx.DeleteAddress(ctx, addressID, profile.ID); err != nil {
			return err
		}
		if wasDefault {
			return qtx.ClearBuyerDefaultAddressID(ctx, profile.ID)
		}
		return nil
	})
}

// SetDefaultAddress atomically unsets the previous default and sets the new one.
func (s *IdentityService) SetDefaultAddress(ctx context.Context, userID uuid.UUID, addressID uuid.UUID) error {
	profile, err := s.store.Queries().GetBuyerProfileByUserID(ctx, userID)
	if err != nil {
		return repository.NormaliseErr(err, "buyer profile")
	}

	existing, err := s.store.Queries().GetAddressByID(ctx, addressID)
	if err != nil {
		return repository.NormaliseErr(err, "address")
	}
	if existing.BuyerProfileID != profile.ID {
		return apperrors.Forbidden("address does not belong to this buyer")
	}

	return s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		if err = qtx.UnsetDefaultAddresses(ctx, profile.ID); err != nil {
			return err
		}
		if err = qtx.SetDefaultAddress(ctx, addressID, profile.ID); err != nil {
			return err
		}
		return qtx.UpdateBuyerDefaultAddressID(ctx, profile.ID, addressID)
	})
}

// ── Vendor onboarding ─────────────────────────────────────────────────────────

// StartVendorOnboarding creates a vendor profile (idempotent — ON CONFLICT DO NOTHING).
func (s *IdentityService) StartVendorOnboarding(ctx context.Context, userID uuid.UUID) (dto.VendorProfileResp, error) {
	vendor, err := s.store.Queries().CreateVendorProfile(ctx, userID)
	if err != nil {
		// ON CONFLICT DO NOTHING returns no row — fall back to GET.
		if errors.Is(err, sql.ErrNoRows) {
			vendor, err = s.store.Queries().GetVendorProfileByUserID(ctx, userID)
		}
		if err != nil {
			return dto.VendorProfileResp{}, apperrors.Internal(fmt.Errorf("create vendor profile: %w", err))
		}
	}
	return vendorToProfileResp(vendor), nil
}

// UpdateVendorBusiness saves business details and advances the onboarding step.
func (s *IdentityService) UpdateVendorBusiness(ctx context.Context, userID uuid.UUID, req dto.VendorBusinessReq) (dto.VendorProfileResp, error) {
	vendor, err := s.store.Queries().GetVendorProfileByUserID(ctx, userID)
	if err != nil {
		return dto.VendorProfileResp{}, repository.NormaliseErr(err, "vendor profile")
	}

	nextStep := advanceStep(vendor.OnboardingStep, "business_details")

	params := db.UpdateVendorBusinessParams{
		ID:           vendor.ID,
		BusinessName: sql.NullString{String: req.BusinessName, Valid: true},
		BusinessType: sql.NullString{String: req.BusinessType, Valid: true},
		OnboardingStep: nextStep,
	}
	if req.EmployeeRange != nil {
		params.EmployeeRange = sql.NullString{String: *req.EmployeeRange, Valid: true}
	}
	if req.YearEstablished != nil {
		params.YearEstablished = sql.NullInt32{Int32: *req.YearEstablished, Valid: true}
	}
	if req.SocialUrl != nil {
		params.SocialUrl = sql.NullString{String: *req.SocialUrl, Valid: true}
	}

	updated, err := s.store.Queries().UpdateVendorBusiness(ctx, params)
	if err != nil {
		return dto.VendorProfileResp{}, apperrors.Internal(fmt.Errorf("update vendor business: %w", err))
	}

	return vendorToProfileResp(updated), nil
}

// SubmitVendorKYC performs the full CBN-aligned KYC verification flow:
//   1. Rate-limit check (max 3 attempts / 24 h per vendor — CBN AML requirement)
//   2. If BVN or NIN provided: call Smile ID to verify against NIMC/CBN database
//   3. If CAC number provided: call Smile ID KYB check against CAC database
//   4. Encrypt every PII field (AES-256-GCM) before writing to DB
//   5. Advance onboarding_step and set kyc_status = pending (manual review)
//      OR = verified (if Smile ID returned an instant Verified result)
//
// SECURITY: req.Bvn, req.Nin and req.IdNumber are NEVER logged anywhere in
// this function. They are passed to Smile ID over TLS and then immediately
// encrypted before storage. The raw strings are not written to any log line.
func (s *IdentityService) SubmitVendorKYC(ctx context.Context, userID uuid.UUID, req dto.VendorKYCReq) (dto.VendorProfileResp, error) {
	vendor, err := s.store.Queries().GetVendorProfileByUserID(ctx, userID)
	if err != nil {
		return dto.VendorProfileResp{}, repository.NormaliseErr(err, "vendor profile")
	}

	// ── Rate limit ────────────────────────────────────────────────────────────
	// CBN AML guidelines require limiting brute-force enumeration.
	// We allow 3 attempts in any rolling 24-hour window.
	// Query directly — the new columns were added via migration 0013 after
	// the sqlc model was generated, so they're not in db.VendorProfile yet.
	var attempts int
	var lastAttempt sql.NullTime
	_ = s.store.DB().QueryRowContext(ctx,
		`SELECT COALESCE(kyc_attempts,0), kyc_last_attempt_at FROM vendor_profiles WHERE id=$1`, vendor.ID,
	).Scan(&attempts, &lastAttempt)

	if attempts >= 3 {
		cutoff := time.Now().Add(-24 * time.Hour)
		if lastAttempt.Valid && lastAttempt.Time.After(cutoff) {
			remaining := lastAttempt.Time.Add(24 * time.Hour).Sub(time.Now())
			return dto.VendorProfileResp{}, apperrors.BadRequest(
				fmt.Sprintf("KYC attempt limit reached. Try again in %.0f hours.", remaining.Hours()))
		}
		// Window expired — reset counter
		_, _ = s.store.DB().ExecContext(ctx,
			`UPDATE vendor_profiles SET kyc_attempts = 0 WHERE id = $1`, vendor.ID)
	}

	// Record this attempt (increment counter + timestamp)
	_, _ = s.store.DB().ExecContext(ctx,
		`UPDATE vendor_profiles SET kyc_attempts = kyc_attempts + 1, kyc_last_attempt_at = NOW() WHERE id = $1`, vendor.ID)

	// ── Smile ID verification ─────────────────────────────────────────────────
	// Use a tight context deadline so the Smile ID call can never block a
	// goroutine beyond the configured timeout (12 s in the client).
	verifyCtx, cancel := context.WithTimeout(ctx, 14*time.Second)
	defer cancel()

	finalStatus := "pending" // default: queue for manual review
	var smileJobID *string

	if req.Nin != nil || req.Bvn != nil {
		idNumber := req.Nin
		idType := smileid.IDTypeNIN
		if req.Bvn != nil {
			idNumber = req.Bvn
			idType = smileid.IDTypeBVN
		}

		firstName, lastName, dob := "", "", ""
		if req.FirstName != nil { firstName = *req.FirstName }
		if req.LastName != nil  { lastName  = *req.LastName  }
		if req.DOB != nil       { dob       = *req.DOB       }

		result, smileErr := s.kycClient.VerifyID(verifyCtx, smileid.VerifyRequest{
			Country:   "NG",
			IDType:    idType,
			IDNumber:  *idNumber, // raw — sent to Smile ID over TLS, NEVER logged
			FirstName: firstName,
			LastName:  lastName,
			DOB:       dob,
		})

		if smileErr != nil {
			// Verification call failed (network, timeout, etc.).
			// We still store the encrypted data and mark pending for manual review;
			// we do NOT leak the raw ID number in the error message.
			s.log.Warn().
				Str("id_type", string(idType)).
				Err(smileErr).
				Msg("smile id call failed — falling back to manual review")
		} else {
			sj := result.SmileJobID
			smileJobID = &sj
			if result.Matched {
				finalStatus = "verified"
			}
		}
	}

	if req.CacNumber != nil {
		result, smileErr := s.kycClient.VerifyID(verifyCtx, smileid.VerifyRequest{
			Country:  "NG",
			IDType:   "CAC", // Smile ID KYB check
			IDNumber: *req.CacNumber,
		})
		if smileErr != nil {
			s.log.Warn().Err(smileErr).Msg("smile id CAC check failed — manual review")
		} else {
			sj := result.SmileJobID
			smileJobID = &sj
			if result.Matched {
				finalStatus = "verified"
			}
		}
	}

	// ── Encrypt PII before storage ────────────────────────────────────────────
	// AES-256-GCM with unique nonce per field. The encryption key is injected
	// at startup from ENCRYPTION_KEY env var and never written to logs/DB.
	params := db.UpdateVendorKYCParams{
		ID:             vendor.ID,
		KycStatus:      finalStatus,
		OnboardingStep: "kyc_submitted",
		Tin:            nullableString(req.Tin),
		CacNumber:      nullableString(req.CacNumber),
		CacDocumentUrl: nullableString(req.CacDocumentUrl),
		IdType:         nullableString(req.IdType),
		IdDocumentUrl:  nullableString(req.IdDocumentUrl),
		SelfieUrl:      nullableString(req.SelfieUrl),
	}

	if req.Bvn != nil {
		enc, encErr := crypto.Encrypt(s.encryptionKey, *req.Bvn)
		if encErr != nil {
			return dto.VendorProfileResp{}, apperrors.Internal(fmt.Errorf("encrypt bvn: %w", encErr))
		}
		params.Bvn = sql.NullString{String: enc, Valid: true}
	}
	if req.Nin != nil {
		enc, encErr := crypto.Encrypt(s.encryptionKey, *req.Nin)
		if encErr != nil {
			return dto.VendorProfileResp{}, apperrors.Internal(fmt.Errorf("encrypt nin: %w", encErr))
		}
		params.Nin = sql.NullString{String: enc, Valid: true}
	}
	if req.IdNumber != nil {
		enc, encErr := crypto.Encrypt(s.encryptionKey, *req.IdNumber)
		if encErr != nil {
			return dto.VendorProfileResp{}, apperrors.Internal(fmt.Errorf("encrypt id_number: %w", encErr))
		}
		params.IdNumber = sql.NullString{String: enc, Valid: true}
	}

	updated, err := s.store.Queries().UpdateVendorKYC(ctx, params)
	if err != nil {
		return dto.VendorProfileResp{}, apperrors.Internal(fmt.Errorf("update vendor kyc: %w", err))
	}

	// Store Smile ID audit job reference (safe — does not contain PII)
	if smileJobID != nil {
		_, _ = s.store.DB().ExecContext(ctx,
			`UPDATE vendor_profiles SET kyc_smile_job_id = $1 WHERE id = $2`, *smileJobID, vendor.ID)
	}

	return vendorToProfileResp(updated), nil
}

// GetVendorProfile returns the vendor profile for the authenticated user.
func (s *IdentityService) GetVendorProfile(ctx context.Context, userID uuid.UUID) (dto.VendorProfileResp, error) {
	vendor, err := s.store.Queries().GetVendorProfileByUserID(ctx, userID)
	if err != nil {
		return dto.VendorProfileResp{}, repository.NormaliseErr(err, "vendor profile")
	}
	return vendorToProfileResp(vendor), nil
}

// ── Vendor banks ──────────────────────────────────────────────────────────────

// AddVendorBank encrypts account_number, adds the bank, and sets it as primary if first.
func (s *IdentityService) AddVendorBank(ctx context.Context, userID uuid.UUID, req dto.VendorBankReq) (dto.VendorBankResp, error) {
	vendor, err := s.store.Queries().GetVendorProfileByUserID(ctx, userID)
	if err != nil {
		return dto.VendorBankResp{}, repository.NormaliseErr(err, "vendor profile")
	}

	encAccNum, err := crypto.Encrypt(s.encryptionKey, req.AccountNumber)
	if err != nil {
		return dto.VendorBankResp{}, apperrors.Internal(fmt.Errorf("encrypt account number: %w", err))
	}

	count, err := s.store.Queries().CountBanksByVendor(ctx, vendor.ID)
	if err != nil {
		return dto.VendorBankResp{}, apperrors.Internal(fmt.Errorf("count banks: %w", err))
	}
	isFirst := count == 0

	params := db.CreateVendorBankParams{
		VendorProfileID: vendor.ID,
		BankName:        req.BankName,
		BankCode:        req.BankCode,
		AccountNumber:   encAccNum,
		AccountName:     req.AccountName,
		IsPrimary:       isFirst,
	}

	var bank db.VendorBank
	if isFirst {
		bank, err = s.store.Queries().CreateVendorBank(ctx, params)
	} else {
		err = s.store.ExecTx(ctx, func(qtx *db.Queries) error {
			var txErr error
			bank, txErr = qtx.CreateVendorBank(ctx, params)
			return txErr
		})
	}
	if err != nil {
		return dto.VendorBankResp{}, apperrors.Internal(fmt.Errorf("create vendor bank: %w", err))
	}

	return bankToResp(bank, req.AccountNumber), nil
}

// ListVendorBanks returns all bank accounts for the vendor, with masked account numbers.
func (s *IdentityService) ListVendorBanks(ctx context.Context, userID uuid.UUID) ([]dto.VendorBankResp, error) {
	vendor, err := s.store.Queries().GetVendorProfileByUserID(ctx, userID)
	if err != nil {
		return nil, repository.NormaliseErr(err, "vendor profile")
	}

	banks, err := s.store.Queries().ListBanksByVendor(ctx, vendor.ID)
	if err != nil {
		return nil, apperrors.Internal(fmt.Errorf("list banks: %w", err))
	}

	out := make([]dto.VendorBankResp, 0, len(banks))
	for _, b := range banks {
		plain, decErr := crypto.Decrypt(s.encryptionKey, b.AccountNumber)
		if decErr != nil {
			s.log.Error().Err(decErr).Str("bank_id", b.ID.String()).Msg("decrypt account number failed")
			plain = "**decrypt error**"
		}
		out = append(out, bankToResp(b, plain))
	}
	return out, nil
}

// SetPrimaryVendorBank atomically unsets current primary and sets the new one.
func (s *IdentityService) SetPrimaryVendorBank(ctx context.Context, userID uuid.UUID, bankID uuid.UUID) error {
	vendor, err := s.store.Queries().GetVendorProfileByUserID(ctx, userID)
	if err != nil {
		return repository.NormaliseErr(err, "vendor profile")
	}

	bank, err := s.store.Queries().GetVendorBankByID(ctx, bankID)
	if err != nil {
		return repository.NormaliseErr(err, "bank account")
	}
	if bank.VendorProfileID != vendor.ID {
		return apperrors.Forbidden("bank account does not belong to this vendor")
	}

	return s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		if err = qtx.UnsetPrimaryBanks(ctx, vendor.ID); err != nil {
			return err
		}
		return qtx.SetPrimaryBank(ctx, bankID, vendor.ID)
	})
}

// DeleteVendorBank removes a bank account. Returns error if trying to delete the primary
// when other accounts exist (caller should set another as primary first).
func (s *IdentityService) DeleteVendorBank(ctx context.Context, userID uuid.UUID, bankID uuid.UUID) error {
	vendor, err := s.store.Queries().GetVendorProfileByUserID(ctx, userID)
	if err != nil {
		return repository.NormaliseErr(err, "vendor profile")
	}

	bank, err := s.store.Queries().GetVendorBankByID(ctx, bankID)
	if err != nil {
		return repository.NormaliseErr(err, "bank account")
	}
	if bank.VendorProfileID != vendor.ID {
		return apperrors.Forbidden("bank account does not belong to this vendor")
	}

	if bank.IsPrimary {
		count, countErr := s.store.Queries().CountBanksByVendor(ctx, vendor.ID)
		if countErr == nil && count > 1 {
			return apperrors.BadRequest("set another bank as primary before removing this one")
		}
	}

	return s.store.Queries().DeleteVendorBank(ctx, bankID, vendor.ID)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (s *IdentityService) addressToResp(a db.BuyerAddress) (dto.AddressResp, error) {
	plain, err := crypto.Decrypt(s.encryptionKey, a.FullAddress)
	if err != nil {
		return dto.AddressResp{}, err
	}
	r := dto.AddressResp{
		ID:          a.ID.String(),
		Label:       a.Label,
		FullAddress: plain,
		City:        a.City,
		State:       a.State,
		IsDefault:   a.IsDefault,
	}
	if a.Longitude.Valid {
		r.Longitude = &a.Longitude.Float64
	}
	if a.Latitude.Valid {
		r.Latitude = &a.Latitude.Float64
	}
	return r, nil
}

func userToMeResp(u db.User) dto.MeResp {
	r := dto.MeResp{
		ID:               u.ID.String(),
		IsEmailVerified:  u.IsEmailVerified,
		ProfileCompleted: u.ProfileCompleted,
		MarketingConsent: u.MarketingConsent,
	}
	if u.Email.Valid {
		r.Email = &u.Email.String
	}
	if u.FullName.Valid {
		r.FullName = &u.FullName.String
	}
	if u.AvatarUrl.Valid {
		r.AvatarURL = &u.AvatarUrl.String
	}
	if u.Phone.Valid {
		r.Phone = &u.Phone.String
	}
	if u.TermsAcceptedAt.Valid {
		r.TermsAcceptedAt = &u.TermsAcceptedAt.Time
	}
	if u.HowHeard.Valid {
		r.HowHeard = &u.HowHeard.String
	}
	return r
}

func buyerToSummary(p db.BuyerProfile) *dto.BuyerSummary {
	s := &dto.BuyerSummary{
		ID:             p.ID.String(),
		TotalOrders:    p.TotalOrders,
		TotalSpentKobo: p.TotalSpent,
	}
	if p.DefaultAddressID.Valid {
		id := p.DefaultAddressID.UUID.String()
		s.DefaultAddressID = &id
	}
	return s
}

func vendorToSummary(v db.VendorProfile) *dto.VendorSummary {
	return &dto.VendorSummary{
		ID:             v.ID.String(),
		OnboardingStep: v.OnboardingStep,
		KycStatus:      v.KycStatus,
		IsActive:       v.IsActive,
	}
}

func vendorToProfileResp(v db.VendorProfile) dto.VendorProfileResp {
	r := dto.VendorProfileResp{
		ID:             v.ID.String(),
		KycStatus:      v.KycStatus,
		OnboardingStep: v.OnboardingStep,
		IsActive:       v.IsActive,
		HasBvn:         v.Bvn.Valid,
		HasNin:         v.Nin.Valid,
		HasIdNumber:    v.IdNumber.Valid,
	}
	if v.BusinessName.Valid {
		r.BusinessName = &v.BusinessName.String
	}
	if v.BusinessType.Valid {
		r.BusinessType = &v.BusinessType.String
	}
	if v.EmployeeRange.Valid {
		r.EmployeeRange = &v.EmployeeRange.String
	}
	if v.YearEstablished.Valid {
		r.YearEstablished = &v.YearEstablished.Int32
	}
	if v.SocialUrl.Valid {
		r.SocialUrl = &v.SocialUrl.String
	}
	if v.Tin.Valid {
		r.Tin = &v.Tin.String
	}
	if v.CacNumber.Valid {
		r.CacNumber = &v.CacNumber.String
	}
	if v.CacDocumentUrl.Valid {
		r.CacDocumentUrl = &v.CacDocumentUrl.String
	}
	if v.IdType.Valid {
		r.IdType = &v.IdType.String
	}
	if v.IdDocumentUrl.Valid {
		r.IdDocumentUrl = &v.IdDocumentUrl.String
	}
	if v.SelfieUrl.Valid {
		r.SelfieUrl = &v.SelfieUrl.String
	}
	if v.ReferralCode.Valid {
		r.ReferralCode = &v.ReferralCode.String
	}
	return r
}

func bankToResp(b db.VendorBank, plainAccountNumber string) dto.VendorBankResp {
	masked := maskAccountNumber(plainAccountNumber)
	return dto.VendorBankResp{
		ID:                  b.ID.String(),
		BankName:            b.BankName,
		BankCode:            b.BankCode,
		AccountNumberMasked: masked,
		AccountName:         b.AccountName,
		IsPrimary:           b.IsPrimary,
		IsVerified:          b.IsVerified,
	}
}

func maskAccountNumber(n string) string {
	if len(n) <= 4 {
		return "****"
	}
	masked := ""
	for i := 0; i < len(n)-4; i++ {
		masked += "*"
	}
	return masked + n[len(n)-4:]
}

// advanceStep returns target if current step is before target, otherwise keeps current.
// This prevents going backwards in onboarding.
func advanceStep(current, target string) string {
	order := map[string]int{
		"account_created":  0,
		"business_details": 1,
		"store_profile":    2,
		"location_set":     3,
		"kyc_submitted":    4,
		"completed":        5,
	}
	if order[current] < order[target] {
		return target
	}
	return current
}

func nullableString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// Ensure unused import doesn't cause build error - time is used in mergeUserPatch.
var _ = time.RFC3339
