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

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
)

type StorefrontService struct {
	db  *sqlx.DB
	log zerolog.Logger
}

func New(db *sqlx.DB, log zerolog.Logger) *StorefrontService {
	return &StorefrontService{db: db, log: log}
}

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

	// Check slug is free.
	var taken bool
	_ = s.db.QueryRowContext(ctx, `SELECT TRUE FROM stores WHERE slug=$1`, req.Slug).Scan(&taken)
	if taken {
		return dto.StoreResp{}, apperrors.Wrap(http.StatusConflict, "slug already taken", nil)
	}

	var row storeRow
	err = s.db.QueryRowxContext(ctx, `
		INSERT INTO stores (vendor_id, name, slug, category, currency, team_size, support_phone)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, vendor_id, name, slug, category, currency,
		          team_size, staff_range, tagline, logo_url, support_phone,
		          address, city, state, custom_domain, custom_domain_status, theme_config, is_active, created_at`,
		userID, req.Name, req.Slug, req.Category, req.Currency,
		req.TeamSize, req.SupportPhone,
	).StructScan(&row)
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("insert store: %w", err)
	}
	return rowToResp(row), nil
}

func (s *StorefrontService) GetMyStore(ctx context.Context, userID uuid.UUID) (dto.StoreResp, error) {
	return s.getStoreByVendor(ctx, userID)
}

func (s *StorefrontService) UpdateStore(ctx context.Context, userID uuid.UUID, storeID uuid.UUID, req dto.UpdateStoreReq) (dto.StoreResp, error) {
	var row storeRow
	err := s.db.QueryRowxContext(ctx, `
		UPDATE stores SET
			name          = COALESCE($1, name),
			tagline       = COALESCE($2, tagline),
			logo_url      = COALESCE($3, logo_url),
			support_phone = COALESCE($4, support_phone),
			address       = COALESCE($5, address),
			city          = COALESCE($6, city),
			state         = COALESCE($7, state),
			theme_config  = COALESCE($8::jsonb, theme_config),
			updated_at    = NOW()
		WHERE id=$9 AND vendor_id=$10
		RETURNING id, vendor_id, name, slug, category, currency,
		          team_size, staff_range, tagline, logo_url, support_phone,
		          address, city, state, custom_domain, custom_domain_status, theme_config, is_active, created_at`,
		req.Name, req.Tagline, req.LogoURL, req.SupportPhone,
		req.Address, req.City, req.State, req.ThemeConfig,
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
		SELECT id, vendor_id, name, slug, category, currency,
		       team_size, staff_range, tagline, logo_url, support_phone,
		       address, city, state, custom_domain, custom_domain_status, is_active, created_at
		FROM stores WHERE slug=$1 AND is_active=TRUE`, slug).StructScan(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.StoreResp{}, apperrors.NotFound("store not found")
	}
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("get store by slug: %w", err)
	}
	return rowToResp(row), nil
}

func (s *StorefrontService) GetStoreByDomain(ctx context.Context, domain string) (dto.StoreResp, error) {
	var row storeRow
	err := s.db.QueryRowxContext(ctx, `
		SELECT id, vendor_id, name, slug, category, currency,
		       team_size, staff_range, tagline, logo_url, support_phone,
		       address, city, state, custom_domain, custom_domain_status, is_active, created_at
		FROM stores WHERE custom_domain=$1 AND custom_domain_status='active' AND is_active=TRUE`, domain).StructScan(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.StoreResp{}, apperrors.NotFound("store not found")
	}
	if err != nil {
		return dto.StoreResp{}, fmt.Errorf("get store by domain: %w", err)
	}
	return rowToResp(row), nil
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
	SupportPhone       sql.NullString `db:"support_phone"`
	Address            sql.NullString `db:"address"`
	City               sql.NullString `db:"city"`
	State              sql.NullString `db:"state"`
	CustomDomain       sql.NullString `db:"custom_domain"`
	CustomDomainStatus string         `db:"custom_domain_status"`
	ThemeConfig        sql.NullString `db:"theme_config"`
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
		SELECT id, vendor_id, name, slug, category, currency,
		       team_size, staff_range, tagline, logo_url, support_phone,
		       address, city, state, custom_domain, custom_domain_status, is_active, created_at
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
	return dto.StoreResp{
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
		SupportPhone:       nullToPtr(r.SupportPhone),
		Address:            nullToPtr(r.Address),
		City:               nullToPtr(r.City),
		State:              nullToPtr(r.State),
		CustomDomain:       nullToPtr(r.CustomDomain),
		CustomDomainStatus: r.CustomDomainStatus,
		ThemeConfig:        nullToPtr(r.ThemeConfig),
		IsActive:           r.IsActive,
		CreatedAt:          r.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func nullToPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func isNotFound(err error) bool {
	return apperrors.IsNotFound(err) || errors.Is(err, sql.ErrNoRows)
}
