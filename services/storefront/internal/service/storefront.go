// Package service implements storefront use-cases.
// All methods are stubs pending database layer wiring.
// Run `make tidy` then add sqlc queries to complete each method.
package service

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
)

// StorefrontService implements all storefront use-cases.
type StorefrontService struct {
	log zerolog.Logger
}

// New creates a StorefrontService.
func New(log zerolog.Logger) *StorefrontService {
	return &StorefrontService{log: log}
}

var errNotImplemented = apperrors.Wrap(http.StatusNotImplemented, "not yet implemented", nil)

// ── Store ─────────────────────────────────────────────────────────────────────

// CreateStore creates the initial store for a vendor from the StoreSetupForm.
// Idempotent — returns the existing store if the vendor already has one.
func (s *StorefrontService) CreateStore(_ context.Context, userID uuid.UUID, req dto.CreateStoreReq) (dto.StoreResp, error) {
	return dto.StoreResp{}, errNotImplemented
}

// GetMyStore returns the store owned by the authenticated vendor.
func (s *StorefrontService) GetMyStore(_ context.Context, userID uuid.UUID) (dto.StoreResp, error) {
	return dto.StoreResp{}, errNotImplemented
}

// UpdateStore applies a partial update to a store the vendor owns.
func (s *StorefrontService) UpdateStore(_ context.Context, userID uuid.UUID, storeID uuid.UUID, req dto.UpdateStoreReq) (dto.StoreResp, error) {
	return dto.StoreResp{}, errNotImplemented
}

// CheckSlugAvailable reports whether a given slug is free to use.
func (s *StorefrontService) CheckSlugAvailable(_ context.Context, slug string) (dto.SlugCheckResp, error) {
	return dto.SlugCheckResp{}, errNotImplemented
}

// ── Staff ─────────────────────────────────────────────────────────────────────

// ListStaff returns all staff members for a store the vendor owns.
func (s *StorefrontService) ListStaff(_ context.Context, userID uuid.UUID, storeID uuid.UUID) ([]dto.StaffMemberResp, error) {
	return nil, errNotImplemented
}

// InviteStaff sends an invitation email and creates a pending staff record.
func (s *StorefrontService) InviteStaff(_ context.Context, userID uuid.UUID, storeID uuid.UUID, req dto.InviteStaffReq) (dto.StaffMemberResp, error) {
	return dto.StaffMemberResp{}, errNotImplemented
}

// RemoveStaff revokes a staff member's access to the store.
func (s *StorefrontService) RemoveStaff(_ context.Context, userID uuid.UUID, storeID uuid.UUID, staffID uuid.UUID) error {
	return errNotImplemented
}
