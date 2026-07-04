// Package repository provides database access for the auth service.
// It wraps the sqlc-generated Queries with transaction support and
// error normalisation (sql.ErrNoRows → apperrors.NotFound).
package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/repository/db"
	"github.com/jmoiron/sqlx"
)

// Store wraps *sqlx.DB and the sqlc Queries, providing transaction helpers.
type Store struct {
	db *sqlx.DB
}

// NewStore creates a Store from a connected *sqlx.DB.
func NewStore(database *sqlx.DB) *Store {
	return &Store{db: database}
}

// Queries returns a sqlc Queries bound to the underlying *sqlx.DB.
// Use this for single-operation reads and writes outside a transaction.
func (s *Store) Queries() *db.Queries {
	return db.New(s.db)
}

// ExecTx executes fn inside a serialisable transaction. If fn returns an error
// the transaction is rolled back; otherwise it is committed.
// Any error from Rollback is appended to the returned error to avoid silent data
// loss but is never the primary return value.
func (s *Store) ExecTx(ctx context.Context, fn func(*db.Queries) error) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	q := db.New(tx)
	if err = fn(q); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("tx error: %w; rollback failed: %v", err, rbErr)
		}
		return err
	}

	return tx.Commit()
}

// QueryStoreIDs returns all store UUID strings owned by userID.
// Queries the stores table directly (same Neon DB, cross-service read).
// Returns nil on any error — callers treat a missing store ID as "no store yet".
func (s *Store) QueryStoreIDs(ctx context.Context, userID string) []string {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id::text FROM stores WHERE vendor_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// StaffRow holds the fields needed for staff login.
type StaffRow struct {
	ID           string
	StoreID      string
	Email        string
	Role         string
	PasswordHash string
	IsActive     bool
}

// QueryStaffByEmail looks up a store_staff record by email for login.
// Returns ErrNoRows if not found (caller should treat as not found).
func (s *Store) QueryStaffByEmail(ctx context.Context, email string) (StaffRow, error) {
	var r StaffRow
	err := s.db.QueryRowContext(ctx,
		`SELECT id::text, store_id::text, email, role,
			COALESCE(password_hash, ''), is_active
		 FROM store_staff WHERE email = $1 LIMIT 1`, email).
		Scan(&r.ID, &r.StoreID, &r.Email, &r.Role, &r.PasswordHash, &r.IsActive)
	return r, err
}

// NormaliseErr converts sql.ErrNoRows to an apperrors.NotFound so the service
// layer does not need to import database/sql to distinguish not-found cases.
func NormaliseErr(err error, resourceName string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return errors.NotFound(resourceName + " not found")
	}
	return err
}
