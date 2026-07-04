package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
)

// ValidRoles lists the granular RBAC roles for store staff.
var ValidRoles = map[string]bool{
	"manager":       true,
	"fulfillment":   true,
	"support":       true,
	"analytics_only": true,
}

// StaffResp is returned by all staff management endpoints.
type StaffResp struct {
	ID        string    `json:"id"`
	StoreID   string    `json:"store_id"`
	FullName  string    `json:"full_name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateStaffReq holds the fields required to create a staff member.
type CreateStaffReq struct {
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// UpdateStaffReq allows updating a staff member's name, role, and active status.
type UpdateStaffReq struct {
	FullName *string `json:"full_name,omitempty"`
	Role     *string `json:"role,omitempty"`
	IsActive *bool   `json:"is_active,omitempty"`
	Password *string `json:"password,omitempty"`
}

// ListStaff returns all staff members for the caller's store.
func (s *IdentityService) ListStaff(ctx context.Context, storeID uuid.UUID) ([]StaffResp, error) {
	rows, err := s.store.DB().QueryxContext(ctx, `
		SELECT id::text, store_id::text, COALESCE(full_name,'') AS full_name,
			email, role, is_active, COALESCE(created_at, NOW()) AS created_at
		FROM store_staff
		WHERE store_id = $1
		ORDER BY created_at ASC`, storeID)
	if err != nil {
		return nil, fmt.Errorf("list staff: %w", err)
	}
	defer rows.Close()

	type row struct {
		ID        string    `db:"id"`
		StoreID   string    `db:"store_id"`
		FullName  string    `db:"full_name"`
		Email     string    `db:"email"`
		Role      string    `db:"role"`
		IsActive  bool      `db:"is_active"`
		CreatedAt time.Time `db:"created_at"`
	}

	var out []StaffResp
	for rows.Next() {
		var r row
		if err := rows.StructScan(&r); err != nil {
			continue
		}
		out = append(out, StaffResp{
			ID: r.ID, StoreID: r.StoreID, FullName: r.FullName,
			Email: r.Email, Role: r.Role, IsActive: r.IsActive, CreatedAt: r.CreatedAt,
		})
	}
	if out == nil {
		out = []StaffResp{}
	}
	return out, nil
}

// CreateStaff adds a new staff member to the store.
func (s *IdentityService) CreateStaff(ctx context.Context, storeID uuid.UUID, req CreateStaffReq) (StaffResp, error) {
	if !ValidRoles[req.Role] {
		return StaffResp{}, apperrors.BadRequest("invalid role: must be manager, fulfillment, support, or analytics_only")
	}
	if req.Email == "" || req.Password == "" || req.FullName == "" {
		return StaffResp{}, apperrors.BadRequest("full_name, email, and password are required")
	}
	if len(req.Password) < 8 {
		return StaffResp{}, apperrors.BadRequest("password must be at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return StaffResp{}, apperrors.Internal(fmt.Errorf("hash password: %w", err))
	}

	type row struct {
		ID        string    `db:"id"`
		StoreID   string    `db:"store_id"`
		FullName  string    `db:"full_name"`
		Email     string    `db:"email"`
		Role      string    `db:"role"`
		IsActive  bool      `db:"is_active"`
		CreatedAt time.Time `db:"created_at"`
	}
	var r row
	err = s.store.DB().QueryRowxContext(ctx, `
		INSERT INTO store_staff (store_id, user_id, full_name, email, role, password_hash, is_active)
		VALUES ($1, gen_random_uuid(), $2, $3, $4, $5, TRUE)
		RETURNING id::text, store_id::text, COALESCE(full_name,'') AS full_name,
			email, role, is_active, COALESCE(created_at, NOW()) AS created_at`,
		storeID, req.FullName, req.Email, req.Role, string(hash)).StructScan(&r)
	if err != nil {
		return StaffResp{}, apperrors.Conflict("a staff member with this email already exists in this store")
	}
	return StaffResp{
		ID: r.ID, StoreID: r.StoreID, FullName: r.FullName,
		Email: r.Email, Role: r.Role, IsActive: r.IsActive, CreatedAt: r.CreatedAt,
	}, nil
}

// UpdateStaff updates a staff member's role, name, active status, or password.
func (s *IdentityService) UpdateStaff(ctx context.Context, storeID uuid.UUID, staffID uuid.UUID, req UpdateStaffReq) (StaffResp, error) {
	if req.Role != nil && !ValidRoles[*req.Role] {
		return StaffResp{}, apperrors.BadRequest("invalid role")
	}

	var passwordHash *string
	if req.Password != nil {
		if len(*req.Password) < 8 {
			return StaffResp{}, apperrors.BadRequest("password must be at least 8 characters")
		}
		h, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			return StaffResp{}, apperrors.Internal(err)
		}
		hs := string(h)
		passwordHash = &hs
	}

	type row struct {
		ID        string    `db:"id"`
		StoreID   string    `db:"store_id"`
		FullName  string    `db:"full_name"`
		Email     string    `db:"email"`
		Role      string    `db:"role"`
		IsActive  bool      `db:"is_active"`
		CreatedAt time.Time `db:"created_at"`
	}
	var r row
	err := s.store.DB().QueryRowxContext(ctx, `
		UPDATE store_staff
		SET
			full_name     = COALESCE($3, full_name),
			role          = COALESCE($4, role),
			is_active     = COALESCE($5, is_active),
			password_hash = COALESCE($6, password_hash)
		WHERE id = $1 AND store_id = $2
		RETURNING id::text, store_id::text, COALESCE(full_name,'') AS full_name,
			email, role, is_active, COALESCE(created_at, NOW()) AS created_at`,
		staffID, storeID, req.FullName, req.Role, req.IsActive, passwordHash).StructScan(&r)
	if err != nil {
		return StaffResp{}, apperrors.NotFound("staff member not found")
	}
	return StaffResp{
		ID: r.ID, StoreID: r.StoreID, FullName: r.FullName,
		Email: r.Email, Role: r.Role, IsActive: r.IsActive, CreatedAt: r.CreatedAt,
	}, nil
}

// DeleteStaff removes a staff member from the store.
func (s *IdentityService) DeleteStaff(ctx context.Context, storeID, staffID uuid.UUID) error {
	res, err := s.store.DB().ExecContext(ctx,
		`DELETE FROM store_staff WHERE id = $1 AND store_id = $2`, staffID, storeID)
	if err != nil {
		return fmt.Errorf("delete staff: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return apperrors.NotFound("staff member not found")
	}
	return nil
}
