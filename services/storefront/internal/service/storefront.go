package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog"

	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/email"
	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/phone"
	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
)

type StorefrontService struct {
	db          *sqlx.DB
	log         zerolog.Logger
	emailer     email.WelcomeMailer
	storeDomain string
	// phones may be nil — number format is still checked, the Termii lookup
	// is simply skipped.
	phones *phone.Verifier
}

func New(db *sqlx.DB, emailer email.WelcomeMailer, storeDomain string, phones *phone.Verifier, log zerolog.Logger) *StorefrontService {
	return &StorefrontService{db: db, emailer: emailer, storeDomain: storeDomain, phones: phones, log: log}
}

func (s *StorefrontService) DB() *sqlx.DB { return s.db }

// ── Store ─────────────────────────────────────────────────────────────────────

func (s *StorefrontService) CreateStore(ctx context.Context, userID uuid.UUID, req dto.CreateStoreReq) (dto.StoreResp, error) {
	// Idempotent: return existing store if vendor already has one.
	existing, err := s.getStoreByVendor(ctx, userID)
	if err == nil {
		return existing, nil
	}
	if !isNotFound(err) {
		return dto.StoreResp{}, fmt.Errorf("check existing store: %w", err)
	}

	// Verify the support phone before creating anything, so a bad number is
	// a clean validation error rather than a store the vendor must edit.
	if req.SupportPhone != nil && *req.SupportPhone != "" {
		normalised, err := s.verifyPhone(ctx, *req.SupportPhone)
		if err != nil {
			return dto.StoreResp{}, err
		}
		req.SupportPhone = &normalised
	}

	// Check slug is free.
	var taken bool
	_ = s.db.QueryRowContext(ctx, `SELECT TRUE FROM stores WHERE slug=$1`, req.Slug).Scan(&taken)
	if taken {
		return dto.StoreResp{}, apperrors.Wrap(http.StatusConflict, "slug already taken", nil)
	}

	var row storeRow
	err = s.db.QueryRowxContext(ctx, `
		INSERT INTO stores (vendor_id, name, slug, category, currency, team_size, support_phone, market_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING `+storeCols,
		userID, req.Name, req.Slug, req.Category, req.Currency,
		req.TeamSize, req.SupportPhone, nullUUID(req.MarketID),
	).StructScan(&row)
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("insert store: %w", err)
	}

	resp := rowToResp(row)

	// Send welcome email asynchronously — never block store creation on email delivery.
	var vendorEmail, vendorName string
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(email,''), COALESCE(full_name,'') FROM users WHERE id=$1`, userID,
	).Scan(&vendorEmail, &vendorName)

	if vendorEmail != "" {
		storeName := resp.Name
		storeSlug := resp.Slug
		storeDomain := s.storeDomain
		go func() {
			ctx2, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if emailErr := s.emailer.SendWelcome(ctx2,
				vendorEmail, vendorName, storeName, storeSlug, storeDomain,
			); emailErr != nil {
				s.log.Warn().Err(emailErr).Str("slug", storeSlug).Msg("welcome email failed")
			}
		}()
	}

	return resp, nil
}

func (s *StorefrontService) GetMyStore(ctx context.Context, userID uuid.UUID) (dto.StoreResp, error) {
	return s.getStoreByVendor(ctx, userID)
}

func (s *StorefrontService) UpdateStore(ctx context.Context, userID uuid.UUID, storeID uuid.UUID, req dto.UpdateStoreReq) (dto.StoreResp, error) {
	if req.SupportPhone != nil && *req.SupportPhone != "" {
		normalised, err := s.verifyPhone(ctx, *req.SupportPhone)
		if err != nil {
			return dto.StoreResp{}, err
		}
		req.SupportPhone = &normalised
	}

	var row storeRow
	err := s.db.QueryRowxContext(ctx, `
		UPDATE stores SET
			name             = COALESCE($1, name),
			tagline          = COALESCE($2, tagline),
			logo_url         = COALESCE($3, logo_url),
			hero_image_url   = COALESCE($4, hero_image_url),
			site_description = COALESCE($5, site_description),
			social_links     = CASE WHEN $6::jsonb IS NOT NULL THEN $6::jsonb ELSE COALESCE(social_links, '{}') END,
			support_phone    = COALESCE($7, support_phone),
			address          = COALESCE($8, address),
			city             = COALESCE($9, city),
			state            = COALESCE($10, state),
			theme_config     = CASE WHEN $11::jsonb IS NOT NULL THEN $11::jsonb ELSE COALESCE(theme_config, '{}') END,
			market_id        = COALESCE($12::uuid, market_id),
			updated_at       = NOW()
		WHERE id=$13 AND vendor_id=$14
		RETURNING `+storeCols,
		req.Name, req.Tagline, req.LogoURL, req.HeroImageURL, req.SiteDescription,
		nullJSON(req.SocialLinks), req.SupportPhone,
		req.Address, req.City, req.State, nullJSON(req.ThemeConfig),
		nullUUID(req.MarketID),
		storeID, userID,
	).StructScan(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.StoreResp{}, apperrors.NotFound("store not found")
	}
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("update store: %w", err)
	}
	return rowToResp(row), nil
}

func (s *StorefrontService) CheckSlugAvailable(ctx context.Context, slug string) (dto.SlugCheckResp, error) {
	var taken bool
	_ = s.db.QueryRowContext(ctx, `SELECT TRUE FROM stores WHERE slug=$1`, slug).Scan(&taken)
	return dto.SlugCheckResp{Slug: slug, Available: !taken}, nil
}

func (s *StorefrontService) GetStoreBySlug(ctx context.Context, slug string) (dto.StoreResp, error) {
	var row storeRow
	err := s.db.QueryRowxContext(ctx, `
		SELECT `+storeCols+`
		FROM stores WHERE slug=$1 AND is_active=TRUE`, slug).StructScan(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.StoreResp{}, apperrors.NotFound("store not found")
	}
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("get store by slug: %w", err)
	}
	return s.withDeliveryOptions(ctx, rowToResp(row)), nil
}

func (s *StorefrontService) GetStoreByDomain(ctx context.Context, domain string) (dto.StoreResp, error) {
	var row storeRow
	err := s.db.QueryRowxContext(ctx, `
		SELECT `+storeCols+`
		FROM stores WHERE custom_domain=$1 AND custom_domain_status='active' AND is_active=TRUE`, domain).StructScan(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.StoreResp{}, apperrors.NotFound("store not found")
	}
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("get store by domain: %w", err)
	}
	return s.withDeliveryOptions(ctx, rowToResp(row)), nil
}

// withDeliveryOptions attaches the store's active delivery options to a public
// store payload so checkout can render them in one round trip. A failure here
// is logged and swallowed — a storefront that loads without delivery choices
// beats one that does not load at all.
func (s *StorefrontService) withDeliveryOptions(ctx context.Context, resp dto.StoreResp) dto.StoreResp {
	storeID, err := uuid.Parse(resp.ID)
	if err != nil {
		return resp
	}
	opts, err := s.ListDeliveryOptions(ctx, storeID, true)
	if err != nil {
		s.log.Warn().Err(err).Str("store_id", resp.ID).Msg("delivery options lookup failed")
		return resp
	}
	resp.DeliveryOptions = opts
	return resp
}

// ── Staff ─────────────────────────────────────────────────────────────────────

func (s *StorefrontService) ListStaff(ctx context.Context, userID uuid.UUID, storeID uuid.UUID) ([]dto.StaffMemberResp, error) {
	if err := s.assertOwner(ctx, userID, storeID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryxContext(ctx,
		`SELECT id, user_id, full_name, email, role, invited_at FROM store_staff WHERE store_id=$1 ORDER BY invited_at`,
		storeID)
	if err != nil {
		return nil, fmt.Errorf("list staff: %w", err)
	}
	defer rows.Close()

	var out []dto.StaffMemberResp
	for rows.Next() {
		var r staffRow
		if err := rows.StructScan(&r); err != nil {
			return nil, err
		}
		out = append(out, dto.StaffMemberResp{
			ID:        r.ID.String(),
			UserID:    r.UserID.String(),
			FullName:  r.FullName,
			Email:     r.Email,
			Role:      r.Role,
			InvitedAt: r.InvitedAt.UTC().Format(time.RFC3339),
		})
	}
	if out == nil {
		out = []dto.StaffMemberResp{}
	}
	return out, nil
}

func (s *StorefrontService) InviteStaff(ctx context.Context, userID uuid.UUID, storeID uuid.UUID, req dto.InviteStaffReq) (dto.StaffMemberResp, error) {
	if err := s.assertOwner(ctx, userID, storeID); err != nil {
		return dto.StaffMemberResp{}, err
	}
	var r staffRow
	err := s.db.QueryRowxContext(ctx, `
		INSERT INTO store_staff (store_id, user_id, email, role)
		VALUES ($1, gen_random_uuid(), $2, $3)
		ON CONFLICT (store_id, user_id) DO UPDATE SET role=EXCLUDED.role
		RETURNING id, user_id, full_name, email, role, invited_at`,
		storeID, req.Email, req.Role,
	).StructScan(&r)
	if err != nil {
		return dto.StaffMemberResp{}, fmt.Errorf("invite staff: %w", err)
	}
	return dto.StaffMemberResp{
		ID:        r.ID.String(),
		UserID:    r.UserID.String(),
		FullName:  r.FullName,
		Email:     r.Email,
		Role:      r.Role,
		InvitedAt: r.InvitedAt.UTC().Format(time.RFC3339),
	}, nil
}

func (s *StorefrontService) RemoveStaff(ctx context.Context, userID uuid.UUID, storeID uuid.UUID, staffID uuid.UUID) error {
	if err := s.assertOwner(ctx, userID, storeID); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM store_staff WHERE id=$1 AND store_id=$2`, staffID, storeID)
	if err != nil {
		return fmt.Errorf("remove staff: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperrors.NotFound("staff member not found")
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// storeCols is the column list every store query returns — kept in one place
// so the markets join stays consistent across SELECT and RETURNING. The
// correlated subquery works in RETURNING too, where a JOIN would not.
const storeCols = `id, vendor_id, name, slug, category, currency,
          team_size, staff_range, tagline, logo_url, hero_image_url, site_description,
          COALESCE(social_links, '{}') AS social_links,
          support_phone, address, city, state,
          market_id,
          (SELECT m.name FROM markets m WHERE m.id = stores.market_id) AS market_name,
          custom_domain, custom_domain_status,
          COALESCE(theme_config, '{}') AS theme_config, is_active, created_at`

type storeRow struct {
	ID                 uuid.UUID      `db:"id"`
	VendorID           uuid.UUID      `db:"vendor_id"`
	Name               string         `db:"name"`
	Slug               string         `db:"slug"`
	Category           string         `db:"category"`
	Currency           string         `db:"currency"`
	TeamSize           sql.NullString `db:"team_size"`
	StaffRange         sql.NullString `db:"staff_range"`
	Tagline            sql.NullString `db:"tagline"`
	LogoURL            sql.NullString `db:"logo_url"`
	HeroImageURL       sql.NullString `db:"hero_image_url"`
	SiteDescription    sql.NullString `db:"site_description"`
	SocialLinks        []byte         `db:"social_links"`
	SupportPhone       sql.NullString `db:"support_phone"`
	Address            sql.NullString `db:"address"`
	City               sql.NullString `db:"city"`
	State              sql.NullString `db:"state"`
	MarketID           uuid.NullUUID  `db:"market_id"`
	MarketName         sql.NullString `db:"market_name"`
	CustomDomain       sql.NullString `db:"custom_domain"`
	CustomDomainStatus string         `db:"custom_domain_status"`
	ThemeConfig        []byte         `db:"theme_config"`
	IsActive           bool           `db:"is_active"`
	CreatedAt          time.Time      `db:"created_at"`
}

type staffRow struct {
	ID        uuid.UUID `db:"id"`
	UserID    uuid.UUID `db:"user_id"`
	FullName  string    `db:"full_name"`
	Email     string    `db:"email"`
	Role      string    `db:"role"`
	InvitedAt time.Time `db:"invited_at"`
}

func (s *StorefrontService) getStoreByVendor(ctx context.Context, userID uuid.UUID) (dto.StoreResp, error) {
	var row storeRow
	err := s.db.QueryRowxContext(ctx, `
		SELECT `+storeCols+`
		FROM stores WHERE vendor_id=$1`, userID).StructScan(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.StoreResp{}, apperrors.NotFound("store not found")
	}
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("get store: %w", err)
	}
	return rowToResp(row), nil
}

func (s *StorefrontService) assertOwner(ctx context.Context, userID, storeID uuid.UUID) error {
	var exists bool
	_ = s.db.QueryRowContext(ctx, `SELECT TRUE FROM stores WHERE id=$1 AND vendor_id=$2`, storeID, userID).Scan(&exists)
	if !exists {
		return apperrors.Wrap(http.StatusForbidden, "store not found or access denied", nil)
	}
	return nil
}

func rowToResp(r storeRow) dto.StoreResp {
	resp := dto.StoreResp{
		ID:                 r.ID.String(),
		VendorID:           r.VendorID.String(),
		Name:               r.Name,
		Slug:               r.Slug,
		Category:           r.Category,
		Currency:           r.Currency,
		TeamSize:           nullToPtr(r.TeamSize),
		StaffRange:         nullToPtr(r.StaffRange),
		Tagline:            nullToPtr(r.Tagline),
		LogoURL:            nullToPtr(r.LogoURL),
		HeroImageURL:       nullToPtr(r.HeroImageURL),
		SiteDescription:    nullToPtr(r.SiteDescription),
		SupportPhone:       nullToPtr(r.SupportPhone),
		Address:            nullToPtr(r.Address),
		City:               nullToPtr(r.City),
		State:              nullToPtr(r.State),
		MarketName:         nullToPtr(r.MarketName),
		CustomDomain:       nullToPtr(r.CustomDomain),
		CustomDomainStatus: r.CustomDomainStatus,
		IsActive:           r.IsActive,
		CreatedAt:          r.CreatedAt.UTC().Format(time.RFC3339),
	}
	if r.MarketID.Valid {
		id := r.MarketID.UUID.String()
		resp.MarketID = &id
	}
	// social_links and theme_config are JSONB — only set when non-empty.
	if len(r.SocialLinks) > 0 && string(r.SocialLinks) != "{}" && string(r.SocialLinks) != "null" {
		resp.SocialLinks = r.SocialLinks
	}
	if len(r.ThemeConfig) > 0 && string(r.ThemeConfig) != "{}" && string(r.ThemeConfig) != "null" {
		resp.ThemeConfig = r.ThemeConfig
	}
	return resp
}

func nullToPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

// nullUUID converts an optional UUID string from a request into a SQL NULL
// when absent, so COALESCE leaves the stored value untouched on PATCH.
func nullUUID(v *string) any {
	if v == nil || *v == "" {
		return nil
	}
	return *v
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// nullJSON returns nil when the raw JSON is empty or a bare null/empty-object,
// so the SQL CASE expression leaves the existing DB value untouched.
func nullJSON(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	if s == "null" || s == "{}" || s == "[]" {
		return nil
	}
	return s
}

// LogView records a storefront page view. It silently ignores unknown slugs.
func (s *StorefrontService) LogView(ctx context.Context, req dto.LogViewReq, ipHash string) {
	var storeID uuid.UUID
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM stores WHERE slug=$1 AND is_active=TRUE`, req.StoreSlug).Scan(&storeID); err != nil {
		return // unknown slug — ignore
	}
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO storefront_views (store_id, slug, path, referrer, ip_hash)
		VALUES ($1,$2,$3,$4,$5)`,
		storeID, req.StoreSlug, req.Path,
		nullStr(req.Referrer), nullStr(ipHash),
	)
}

// GetStoreViews returns view counts for a store.
func (s *StorefrontService) GetStoreViews(ctx context.Context, storeID uuid.UUID) (dto.StoreViewsResp, error) {
	var r dto.StoreViewsResp
	r.StoreID = storeID.String()
	rows := []struct {
		label string
		field *int64
		where string
	}{
		{"30d", &r.Views30d, "NOW() - INTERVAL '30 days'"},
		{"7d", &r.Views7d, "NOW() - INTERVAL '7 days'"},
		{"all", &r.ViewsAll, "'epoch'"},
	}
	for _, row := range rows {
		q := fmt.Sprintf(`SELECT COUNT(*) FROM storefront_views WHERE store_id=$1 AND viewed_at > %s`, row.where)
		if err := s.db.QueryRowContext(ctx, q, storeID).Scan(row.field); err != nil {
			return r, err
		}
	}
	return r, nil
}

func isNotFound(err error) bool {
	return apperrors.IsNotFound(err) || errors.Is(err, sql.ErrNoRows)
}

// verifyPhone normalises a support number to E.164 and checks with Termii
// that the line actually exists. The stored value is always the normalised
// form, so every store's number looks the same downstream.
//
// A number Termii does not recognise is rejected; a Termii outage is not the
// vendor's problem, so the lookup fails open (see phone.Verifier.Verify).
func (s *StorefrontService) verifyPhone(ctx context.Context, raw string) (string, error) {
	normalised, err := phone.Normalise(raw)
	if err != nil {
		return "", apperrors.BadRequest("enter a valid Nigerian phone number, e.g. 08031234567")
	}

	ok, err := s.phones.Verify(ctx, normalised)
	if err != nil {
		return "", fmt.Errorf("verify phone: %w", err)
	}
	if !ok {
		return "", apperrors.BadRequest("that phone number could not be verified — check it and try again")
	}
	return normalised, nil
}
