package repository

import (
	"context"
	"database/sql"
	"fmt"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/repository/db"
	"github.com/jmoiron/sqlx"
)

// Store wraps *sqlx.DB and exposes query and transaction helpers.
type Store struct {
	db *sqlx.DB
}

func NewStore(database *sqlx.DB) *Store {
	return &Store{db: database}
}

func (s *Store) Queries() *db.Queries {
	return db.New(s.db)
}

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

func NormaliseErr(err error, resourceName string) error {
	if err == nil {
		return nil
	}
	if apperrors.Is(err, sql.ErrNoRows) {
		return apperrors.NotFound(resourceName + " not found")
	}
	return err
}
