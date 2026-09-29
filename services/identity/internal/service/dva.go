package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// dvaBackfillInterval is how often the sweep below re-checks for vendors
// still missing a Dedicated Virtual Account.
const dvaBackfillInterval = 1 * time.Hour

// VendorMissingDVA is one vendor who has a store but no Dedicated Virtual
// Account yet — i.e. finished onboarding but has nowhere for buyers' money
// to land.
type VendorMissingDVA struct {
	UserID    uuid.UUID `db:"user_id"`
	StoreName string    `db:"store_name"`
	StoreSlug string    `db:"store_slug"`
	Email     string    `db:"email"`
}

// vendorsMissingDVAQuery is shared verbatim with scripts/dva-audit, which
// reports the same set without needing the identity service running. A
// vendor with no store is deliberately excluded: the DVA is named after the
// store, so there is nothing to name the account yet.
//
// DISTINCT ON guards the (currently impossible, but unenforced by any
// constraint) case of one vendor owning several stores — the oldest store
// wins, matching the name the vendor onboarded with.
const vendorsMissingDVAQuery = `
	SELECT DISTINCT ON (vp.user_id)
	       vp.user_id,
	       s.name              AS store_name,
	       s.slug              AS store_slug,
	       COALESCE(u.email,'') AS email
	FROM vendor_profiles vp
	JOIN stores s ON s.vendor_id = vp.user_id
	JOIN users  u ON u.id        = vp.user_id
	WHERE vp.paystack_dva_account_number IS NULL
	ORDER BY vp.user_id, s.created_at`

// VendorsMissingDVA lists every vendor with a store but no virtual account.
func (s *IdentityService) VendorsMissingDVA(ctx context.Context) ([]VendorMissingDVA, error) {
	out := []VendorMissingDVA{}
	if err := s.store.DB().SelectContext(ctx, &out, vendorsMissingDVAQuery); err != nil {
		return nil, fmt.Errorf("list vendors missing dva: %w", err)
	}
	return out, nil
}

// StartDVABackfillLoop runs until ctx is cancelled, provisioning a virtual
// account for any vendor that has a store but no DVA.
//
// Store creation is the trigger for provisioning (see storefront's
// CreateStore), and that call is best-effort and asynchronous — so a
// Paystack outage, an identity restart mid-flight, or a wrong/missing
// INTERNAL_API_KEY used to leave a vendor permanently without an account
// number, with no path to recovery but a manual fix. This is that path:
// whatever the one-shot trigger misses, the next sweep picks up.
//
// Same shape as orders' StartAutoReleaseLoop — a plain goroutine started at
// boot, since no cron infrastructure exists in this backend. Safe to run on
// several instances at once: ProvisionVendorDVA no-ops on a vendor that
// already has an account number.
func (s *IdentityService) StartDVABackfillLoop(ctx context.Context) {
	ticker := time.NewTicker(dvaBackfillInterval)
	defer ticker.Stop()
	// Run once at startup too, so a deploy is itself a retry.
	s.backfillMissingDVAs(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.backfillMissingDVAs(ctx)
		}
	}
}

func (s *IdentityService) backfillMissingDVAs(ctx context.Context) {
	vendors, err := s.VendorsMissingDVA(ctx)
	if err != nil {
		s.log.Warn().Err(err).Msg("dva backfill: query failed")
		middleware.RecordBackgroundError(s.store.DB(), s.log, "identity", "dva backfill: query failed: "+err.Error(), nil)
		return
	}
	if len(vendors) == 0 {
		return
	}

	s.log.Info().Int("count", len(vendors)).Msg("dva backfill: provisioning vendors without a virtual account")

	var provisioned int
	for _, v := range vendors {
		// Each vendor is a separate Paystack round trip; one failure must
		// not abandon the rest, and it is already recorded to the admin
		// error queue inside provisionPaystackAccount.
		if err := s.ProvisionVendorDVA(ctx, v.UserID, v.StoreName); err != nil {
			s.log.Warn().Err(err).Str("store", v.StoreName).Str("user_id", v.UserID.String()).
				Msg("dva backfill: provisioning failed, will retry next sweep")
			continue
		}
		provisioned++
	}
	s.log.Info().Int("provisioned", provisioned).Int("remaining", len(vendors)-provisioned).Msg("dva backfill: done")
}
