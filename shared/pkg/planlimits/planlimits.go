// Package planlimits resolves a vendor's current subscription-plan limits
// via a direct cross-service query against the shared plans/vendor_subscriptions
// tables (owned by the identity service, but read here from storefront/catalogue
// since every service shares one physical Postgres database — the same
// convention storefront.CreateStore already uses to read the users table).
package planlimits

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// Limits describes the caps in effect for a vendor's active plan.
type Limits struct {
	PlanSlug     string
	ProductLimit int // -1 = unlimited
	TeamLimit    int
	StoreLimit   int
}

// defaultFreeLimits is returned when a vendor has no subscription row yet
// (e.g. they clicked "Skip for now" on the plans page) — the same caps as
// the seeded Free plan, so callers never need a special no-subscription case.
var defaultFreeLimits = Limits{PlanSlug: "free", ProductLimit: 30, TeamLimit: 1, StoreLimit: 1}

const limitsSelect = `
	SELECT p.slug, p.product_limit, p.team_limit, p.store_limit
	FROM vendor_subscriptions vs
	JOIN plans p ON p.id = vs.plan_id
	WHERE vs.vendor_profile_id = $1`

// ForVendorUserID resolves plan limits from a vendor's users.id (== vendor_profiles.user_id).
func ForVendorUserID(ctx context.Context, db *sqlx.DB, vendorUserID uuid.UUID) (Limits, error) {
	var vendorProfileID uuid.UUID
	err := db.QueryRowContext(ctx,
		`SELECT id FROM vendor_profiles WHERE user_id = $1`, vendorUserID,
	).Scan(&vendorProfileID)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultFreeLimits, nil
	}
	if err != nil {
		return Limits{}, fmt.Errorf("planlimits: resolve vendor profile: %w", err)
	}
	return forVendorProfileID(ctx, db, vendorProfileID)
}

// ForStoreID resolves plan limits by walking stores.vendor_id -> vendor_profiles -> vendor_subscriptions -> plans.
func ForStoreID(ctx context.Context, db *sqlx.DB, storeID uuid.UUID) (Limits, error) {
	var vendorProfileID uuid.UUID
	err := db.QueryRowContext(ctx, `
		SELECT vp.id FROM stores s
		JOIN vendor_profiles vp ON vp.user_id = s.vendor_id
		WHERE s.id = $1`, storeID,
	).Scan(&vendorProfileID)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultFreeLimits, nil
	}
	if err != nil {
		return Limits{}, fmt.Errorf("planlimits: resolve store vendor: %w", err)
	}
	return forVendorProfileID(ctx, db, vendorProfileID)
}

func forVendorProfileID(ctx context.Context, db *sqlx.DB, vendorProfileID uuid.UUID) (Limits, error) {
	var l Limits
	err := db.QueryRowContext(ctx, limitsSelect, vendorProfileID).
		Scan(&l.PlanSlug, &l.ProductLimit, &l.TeamLimit, &l.StoreLimit)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultFreeLimits, nil
	}
	if err != nil {
		return Limits{}, fmt.Errorf("planlimits: resolve subscription: %w", err)
	}
	return l, nil
}

// AllowsCurrency reports whether the plan permits the given store currency.
func (l Limits) AllowsCurrency(currency string) bool {
	if l.PlanSlug != "free" {
		return true
	}
	return currency == "NGN"
}

// AllowsTeamSize reports whether the plan permits the given declared team-size bucket.
func (l Limits) AllowsTeamSize(teamSize string) bool {
	if l.PlanSlug != "free" {
		return true
	}
	return teamSize == "solo" || teamSize == "2-10"
}

// MaxImagesPerProduct returns the per-product image cap for this plan.
func (l Limits) MaxImagesPerProduct() int {
	if l.PlanSlug == "free" {
		return 3
	}
	return 8
}
