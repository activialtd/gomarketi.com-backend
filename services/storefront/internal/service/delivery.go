package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
)

// Delivery options are the vendor's own delivery choices — a title, an
// optional note and a price, e.g. "Ogba Busstop / dispatch will call to
// balance up for bulky items / ₦4,500". Checkout renders the active ones and
// the orders service validates the chosen fee against this table.

const deliveryOptionCols = `id, store_id, title, description, price_kobo, position, is_active, created_at`

type deliveryOptionRow struct {
	ID          uuid.UUID `db:"id"`
	StoreID     uuid.UUID `db:"store_id"`
	Title       string    `db:"title"`
	Description string    `db:"description"`
	PriceKobo   int64     `db:"price_kobo"`
	Position    int       `db:"position"`
	IsActive    bool      `db:"is_active"`
	CreatedAt   time.Time `db:"created_at"`
}

func deliveryRowToResp(r deliveryOptionRow) dto.DeliveryOptionResp {
	return dto.DeliveryOptionResp{
		ID:          r.ID.String(),
		StoreID:     r.StoreID.String(),
		Title:       r.Title,
		Description: r.Description,
		PriceKobo:   r.PriceKobo,
		Position:    r.Position,
		IsActive:    r.IsActive,
		CreatedAt:   r.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// ListDeliveryOptions returns a store's options. activeOnly is what public
// callers (checkout) get; the vendor dashboard sees disabled ones too.
func (s *StorefrontService) ListDeliveryOptions(ctx context.Context, storeID uuid.UUID, activeOnly bool) ([]dto.DeliveryOptionResp, error) {
	rows, err := s.db.QueryxContext(ctx, `
		SELECT `+deliveryOptionCols+`
		FROM store_delivery_options
		WHERE store_id = $1 AND ($2 = FALSE OR is_active = TRUE)
		ORDER BY position, created_at`, storeID, activeOnly)
	if err != nil {
		return nil, fmt.Errorf("list delivery options: %w", err)
	}
	defer rows.Close()

	out := []dto.DeliveryOptionResp{}
	for rows.Next() {
		var r deliveryOptionRow
		if err := rows.StructScan(&r); err != nil {
			return nil, fmt.Errorf("scan delivery option: %w", err)
		}
		out = append(out, deliveryRowToResp(r))
	}
	return out, rows.Err()
}

// ListDeliveryOptionsForVendor is the dashboard read — ownership enforced.
func (s *StorefrontService) ListDeliveryOptionsForVendor(ctx context.Context, userID, storeID uuid.UUID) ([]dto.DeliveryOptionResp, error) {
	if err := s.assertOwner(ctx, userID, storeID); err != nil {
		return nil, err
	}
	return s.ListDeliveryOptions(ctx, storeID, false)
}

// CreateDeliveryOption adds an option to the vendor's own store.
func (s *StorefrontService) CreateDeliveryOption(ctx context.Context, userID, storeID uuid.UUID, req dto.CreateDeliveryOptionReq) (dto.DeliveryOptionResp, error) {
	if err := s.assertOwner(ctx, userID, storeID); err != nil {
		return dto.DeliveryOptionResp{}, err
	}

	// Default the position to the end of the current list so options keep the
	// order the vendor added them in.
	position := 0
	if req.Position != nil {
		position = *req.Position
	} else {
		_ = s.db.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(position)+1, 0) FROM store_delivery_options WHERE store_id=$1`,
			storeID,
		).Scan(&position)
	}

	var row deliveryOptionRow
	err := s.db.QueryRowxContext(ctx, `
		INSERT INTO store_delivery_options (store_id, title, description, price_kobo, position)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING `+deliveryOptionCols,
		storeID, req.Title, req.Description, req.PriceKobo, position,
	).StructScan(&row)
	if err != nil {
		return dto.DeliveryOptionResp{}, fmt.Errorf("insert delivery option: %w", err)
	}
	return deliveryRowToResp(row), nil
}

// UpdateDeliveryOption patches one option. Omitted fields keep their value.
func (s *StorefrontService) UpdateDeliveryOption(ctx context.Context, userID, storeID, optionID uuid.UUID, req dto.UpdateDeliveryOptionReq) (dto.DeliveryOptionResp, error) {
	if err := s.assertOwner(ctx, userID, storeID); err != nil {
		return dto.DeliveryOptionResp{}, err
	}

	var row deliveryOptionRow
	err := s.db.QueryRowxContext(ctx, `
		UPDATE store_delivery_options SET
			title       = COALESCE($1, title),
			description = COALESCE($2, description),
			price_kobo  = COALESCE($3, price_kobo),
			position    = COALESCE($4, position),
			is_active   = COALESCE($5, is_active),
			updated_at  = NOW()
		WHERE id = $6 AND store_id = $7
		RETURNING `+deliveryOptionCols,
		req.Title, req.Description, req.PriceKobo, req.Position, req.IsActive,
		optionID, storeID,
	).StructScan(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.DeliveryOptionResp{}, apperrors.NotFound("delivery option not found")
	}
	if err != nil {
		return dto.DeliveryOptionResp{}, fmt.Errorf("update delivery option: %w", err)
	}
	return deliveryRowToResp(row), nil
}

// DeleteDeliveryOption removes an option. Orders already placed keep the fee
// and title copied onto them, so deleting never rewrites order history.
func (s *StorefrontService) DeleteDeliveryOption(ctx context.Context, userID, storeID, optionID uuid.UUID) error {
	if err := s.assertOwner(ctx, userID, storeID); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM store_delivery_options WHERE id=$1 AND store_id=$2`, optionID, storeID)
	if err != nil {
		return fmt.Errorf("delete delivery option: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperrors.NotFound("delivery option not found")
	}
	return nil
}
