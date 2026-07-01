package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/dto"
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

	// Paid plans require a payment reference.
	if plan.PriceKobo > 0 && (req.PaymentReference == nil || *req.PaymentReference == "") {
		return dto.SubscriptionResp{}, apperrors.BadRequest("payment_reference is required for paid plans")
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

	return s.GetSubscription(ctx, userID)
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
	return subRowToResp(r), nil
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
