package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/paystack"
	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/repository/db"
	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// ── Plans ──────────────────────────────────────────────────────────────────────

func (s *IdentityService) ListPlans(ctx context.Context) ([]dto.PlanResp, error) {
	rows, err := s.store.DB().QueryxContext(ctx,
		`SELECT id, slug, display_name, description, price_kobo, billing_cycle,
		        product_limit, store_limit, team_limit, features, sort_order
		 FROM plans WHERE is_active = TRUE ORDER BY sort_order`)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()

	out := make([]dto.PlanResp, 0)
	for rows.Next() {
		var r planRow
		if err := rows.StructScan(&r); err != nil {
			return nil, err
		}
		out = append(out, planRowToResp(r))
	}
	return out, nil
}

func (s *IdentityService) GetPlan(ctx context.Context, planID uuid.UUID) (dto.PlanResp, error) {
	var r planRow
	err := s.store.DB().QueryRowxContext(ctx,
		`SELECT id, slug, display_name, description, price_kobo, billing_cycle,
		        product_limit, store_limit, team_limit, features, sort_order
		 FROM plans WHERE id=$1 AND is_active=TRUE`, planID).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.PlanResp{}, apperrors.NotFound("plan not found")
	}
	if err != nil {
		return dto.PlanResp{}, fmt.Errorf("get plan: %w", err)
	}
	return planRowToResp(r), nil
}

// ── Subscriptions ──────────────────────────────────────────────────────────────

func (s *IdentityService) SelectPlan(ctx context.Context, userID uuid.UUID, req dto.SelectPlanReq) (dto.SubscriptionResp, error) {
	planID, err := uuid.Parse(req.PlanID)
	if err != nil {
		return dto.SubscriptionResp{}, apperrors.BadRequest("invalid plan_id")
	}

	plan, err := s.GetPlan(ctx, planID)
	if err != nil {
		return dto.SubscriptionResp{}, err
	}

	// Paid plans require a payment reference — and the reference must be a
	// real, successful Paystack charge for exactly the plan's price. Before
	// this check, any non-empty string was accepted with no verification at
	// all, so a vendor could get a paid plan's limits for free by calling
	// this endpoint directly instead of going through the real checkout UI.
	if plan.PriceKobo > 0 {
		if req.PaymentReference == nil || *req.PaymentReference == "" {
			return dto.SubscriptionResp{}, apperrors.BadRequest("payment_reference is required for paid plans")
		}
		if err := s.paystackClient.VerifyTransaction(ctx, *req.PaymentReference, plan.PriceKobo); err != nil {
			return dto.SubscriptionResp{}, apperrors.BadRequest("payment verification failed: " + err.Error())
		}
	}

	// Get vendor profile.
	vendor, err := s.store.Queries().GetVendorProfileByUserID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.SubscriptionResp{}, apperrors.NotFound("vendor profile not found — call /v1/identity/vendor/onboard first")
	}
	if err != nil {
		return dto.SubscriptionResp{}, fmt.Errorf("get vendor: %w", err)
	}

	// Compute period end (1 month for monthly, nil for free).
	var periodEnd *time.Time
	if plan.PriceKobo > 0 {
		t := time.Now().AddDate(0, 1, 0)
		periodEnd = &t
	}

	db := s.store.DB()

	// Upsert subscription (one subscription per vendor).
	var subID uuid.UUID
	err = db.QueryRowContext(ctx, `
		INSERT INTO vendor_subscriptions (vendor_profile_id, plan_id, status, payment_reference, current_period_start, current_period_end)
		VALUES ($1, $2, 'active', $3, NOW(), $4)
		ON CONFLICT (vendor_profile_id) DO UPDATE
		  SET plan_id              = EXCLUDED.plan_id,
		      status               = 'active',
		      payment_reference    = EXCLUDED.payment_reference,
		      current_period_start = NOW(),
		      current_period_end   = EXCLUDED.current_period_end,
		      updated_at           = NOW()
		RETURNING id`,
		vendor.ID, planID, req.PaymentReference, periodEnd,
	).Scan(&subID)
	if err != nil {
		return dto.SubscriptionResp{}, fmt.Errorf("upsert subscription: %w", err)
	}

	// Advance onboarding step to plan_selected if still at account_created.
	_, _ = db.ExecContext(ctx, `
		UPDATE vendor_profiles SET onboarding_step = 'plan_selected', updated_at = NOW()
		WHERE id = $1 AND onboarding_step = 'account_created'`, vendor.ID)

	// Dedicated Virtual Account provisioning does NOT happen here anymore —
	// it's triggered by storefront's CreateStore instead, once the vendor's
	// store (and therefore its name) exists. See ProvisionVendorDVA. A plan
	// can be selected long before a store exists, and the DVA's account_name
	// is meant to read as the store's name, not the vendor's personal name.

	return s.GetSubscription(ctx, userID)
}

// ProvisionVendorDVA creates a Paystack Customer + Dedicated Virtual Account
// for a vendor, named after their store (storeName) rather than their
// personal name — called by storefront's CreateStore right after the store
// row exists (via the internal /vendor/provision-dva route), not at plan
// selection, since a store might not exist yet at that point. Idempotent:
// a vendor who already has a DVA is left untouched.
func (s *IdentityService) ProvisionVendorDVA(ctx context.Context, userID uuid.UUID, storeName string) error {
	vendor, err := s.store.Queries().GetVendorProfileByUserID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return apperrors.NotFound("vendor profile not found")
	}
	if err != nil {
		return fmt.Errorf("get vendor: %w", err)
	}
	if vendor.PaystackDVAAccountNumber.Valid {
		return nil
	}

	user, err := s.store.Queries().GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if !user.Email.Valid {
		return apperrors.BadRequest("vendor has no email on file")
	}

	return s.provisionPaystackAccount(vendor.ID, user.Email.String, user.FullName.String, storeName, user.Phone.String)
}

// provisionPaystackAccount creates a Paystack Customer + Dedicated Virtual
// Account for a vendor and persists the result. displayName becomes the
// DVA's account_name (via Paystack's first_name/last_name split — pass the
// vendor's store name, not their personal name); vendorName is used only for
// the account-ready email greeting, kept separate so that email still reads
// as addressed to the person, not the store.
func (s *IdentityService) provisionPaystackAccount(vendorID uuid.UUID, email, vendorName, displayName, phone string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	first, last := paystack.SplitName(displayName)
	if last == "" {
		// Paystack requires a non-empty last_name on the underlying customer
		// for dedicated_account creation — a single-word store name (e.g.
		// "Zara") would otherwise fail with "last_name is required".
		last = "Store"
	}
	customerCode, err := s.paystackClient.CreateCustomer(ctx, email, first, last, phone)
	if err != nil {
		s.log.Warn().Err(err).Str("vendor_id", vendorID.String()).Msg("paystack customer creation failed")
		middleware.RecordBackgroundError(s.store.DB(), s.log, "identity", "paystack customer creation failed: "+err.Error(),
			map[string]any{"vendor_id": vendorID.String()})
		return fmt.Errorf("paystack customer creation failed: %w", err)
	}

	accNum, bankName, accName, err := s.paystackClient.CreateDedicatedAccount(ctx, customerCode)
	if err != nil {
		s.log.Warn().Err(err).Str("vendor_id", vendorID.String()).Msg("paystack DVA creation failed")
		middleware.RecordBackgroundError(s.store.DB(), s.log, "identity", "paystack DVA creation failed: "+err.Error(),
			map[string]any{"vendor_id": vendorID.String(), "customer_code": customerCode})
		return fmt.Errorf("paystack DVA creation failed: %w", err)
	}

	params := db.UpdateVendorPaystackAccountParams{
		ID:            vendorID,
		CustomerCode:  customerCode,
		AccountNumber: accNum,
		BankName:      bankName,
		AccountName:   accName,
	}
	// The pooled Neon connection occasionally rejects a bind on this
	// goroutine's write when it races other concurrent DB traffic
	// (PgBouncer transaction-pooling + unnamed prepared statements) — a
	// couple of quick retries absorb that without adding real latency to
	// the (already async, off the request path) common case.
	var persistErr error
	for attempt := 0; attempt < 3; attempt++ {
		if _, persistErr = s.store.Queries().UpdateVendorPaystackAccount(ctx, params); persistErr == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if persistErr != nil {
		s.log.Warn().Err(persistErr).Str("vendor_id", vendorID.String()).Msg("persist paystack DVA failed")
		middleware.RecordBackgroundError(s.store.DB(), s.log, "identity", "persist paystack DVA failed: "+persistErr.Error(),
			map[string]any{"vendor_id": vendorID.String(), "customer_code": customerCode, "account_number": accNum})
		return fmt.Errorf("persist paystack DVA failed: %w", persistErr)
	}

	if err := s.mailer.SendAccountReady(ctx, email, vendorName, bankName, accNum, accName); err != nil {
		s.log.Warn().Err(err).Str("vendor_id", vendorID.String()).Msg("account ready email failed")
		middleware.RecordBackgroundError(s.store.DB(), s.log, "identity", "account ready email failed: "+err.Error(),
			map[string]any{"vendor_id": vendorID.String()})
	}
	return nil
}

func (s *IdentityService) GetSubscription(ctx context.Context, userID uuid.UUID) (dto.SubscriptionResp, error) {
	vendor, err := s.store.Queries().GetVendorProfileByUserID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.SubscriptionResp{}, apperrors.NotFound("no vendor profile")
	}
	if err != nil {
		return dto.SubscriptionResp{}, fmt.Errorf("get vendor: %w", err)
	}

	var r subRow
	err = s.store.DB().QueryRowxContext(ctx, `
		SELECT vs.id AS sub_id, vs.plan_id, vs.status, vs.payment_reference,
		       vs.current_period_start, vs.current_period_end,
		       p.id AS plan_db_id, p.slug, p.display_name, p.description, p.price_kobo, p.billing_cycle,
		       p.product_limit, p.store_limit, p.team_limit, p.features, p.sort_order
		FROM vendor_subscriptions vs
		JOIN plans p ON p.id = vs.plan_id
		WHERE vs.vendor_profile_id = $1`, vendor.ID).StructScan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.SubscriptionResp{}, apperrors.NotFound("no subscription — plan not selected yet")
	}
	if err != nil {
		return dto.SubscriptionResp{}, fmt.Errorf("get subscription: %w", err)
	}
	resp := subRowToResp(r)
	if vendor.PaystackDVAAccountNumber.Valid {
		resp.PaystackAccountNumber = &vendor.PaystackDVAAccountNumber.String
	}
	if vendor.PaystackDVABankName.Valid {
		resp.PaystackBankName = &vendor.PaystackDVABankName.String
	}
	if vendor.PaystackDVAAccountName.Valid {
		resp.PaystackAccountName = &vendor.PaystackDVAAccountName.String
	}
	return resp, nil
}

// ── Row types ──────────────────────────────────────────────────────────────────

type planRow struct {
	ID           uuid.UUID `db:"id"`
	Slug         string    `db:"slug"`
	DisplayName  string    `db:"display_name"`
	Description  string    `db:"description"`
	PriceKobo    int64     `db:"price_kobo"`
	BillingCycle string    `db:"billing_cycle"`
	ProductLimit int       `db:"product_limit"`
	StoreLimit   int       `db:"store_limit"`
	TeamLimit    int       `db:"team_limit"`
	Features     []byte    `db:"features"`
	SortOrder    int       `db:"sort_order"`
}

type subRow struct {
	SubID              uuid.UUID  `db:"sub_id"`
	PlanIDCol          uuid.UUID  `db:"plan_id"`
	Status             string     `db:"status"`
	PaymentReference   *string    `db:"payment_reference"`
	CurrentPeriodStart time.Time  `db:"current_period_start"`
	CurrentPeriodEnd   *time.Time `db:"current_period_end"`
	// plan fields (prefixed to avoid column name collision)
	PlanDBID     uuid.UUID `db:"plan_db_id"`
	Slug         string    `db:"slug"`
	DisplayName  string    `db:"display_name"`
	Description  string    `db:"description"`
	PriceKobo    int64     `db:"price_kobo"`
	BillingCycle string    `db:"billing_cycle"`
	ProductLimit int       `db:"product_limit"`
	StoreLimit   int       `db:"store_limit"`
	TeamLimit    int       `db:"team_limit"`
	Features     []byte    `db:"features"`
	SortOrder    int       `db:"sort_order"`
}

func planRowToResp(r planRow) dto.PlanResp {
	var features []string
	_ = json.Unmarshal(r.Features, &features)
	if features == nil {
		features = []string{}
	}
	return dto.PlanResp{
		ID:           r.ID.String(),
		Slug:         r.Slug,
		DisplayName:  r.DisplayName,
		Description:  r.Description,
		PriceKobo:    r.PriceKobo,
		BillingCycle: r.BillingCycle,
		ProductLimit: r.ProductLimit,
		StoreLimit:   r.StoreLimit,
		TeamLimit:    r.TeamLimit,
		Features:     features,
		SortOrder:    r.SortOrder,
	}
}

func subRowToResp(r subRow) dto.SubscriptionResp {
	plan := planRowToResp(planRow{
		ID:           r.PlanDBID,
		Slug:         r.Slug,
		DisplayName:  r.DisplayName,
		Description:  r.Description,
		PriceKobo:    r.PriceKobo,
		BillingCycle: r.BillingCycle,
		ProductLimit: r.ProductLimit,
		StoreLimit:   r.StoreLimit,
		TeamLimit:    r.TeamLimit,
		Features:     r.Features,
		SortOrder:    r.SortOrder,
	})
	plan.ID = r.PlanIDCol.String()

	resp := dto.SubscriptionResp{
		ID:                 r.SubID.String(),
		PlanID:             r.PlanIDCol.String(),
		Plan:               plan,
		Status:             r.Status,
		PaymentReference:   r.PaymentReference,
		CurrentPeriodStart: r.CurrentPeriodStart.UTC().Format(time.RFC3339),
	}
	if r.CurrentPeriodEnd != nil {
		s := r.CurrentPeriodEnd.UTC().Format(time.RFC3339)
		resp.CurrentPeriodEnd = &s
	}
	return resp
}
