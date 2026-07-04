package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/dto"
)

// KnownGateways lists every gateway the platform supports.
// The order here is the display order on the checkout page.
var KnownGateways = []string{"paystack", "flutterwave", "stripe", "pos", "manual"}

// ListPaymentGateways returns all gateway rows for a store, creating missing ones on the fly.
func (s *OrdersService) ListPaymentGateways(ctx context.Context, storeID uuid.UUID) ([]dto.PaymentGatewayResp, error) {
	// Ensure all known gateways exist for this store (idempotent seed)
	for _, gw := range KnownGateways {
		_, _ = s.db.ExecContext(ctx, `
			INSERT INTO store_payment_methods (store_id, gateway, enabled)
			VALUES ($1, $2, $3)
			ON CONFLICT (store_id, gateway) DO NOTHING`,
			storeID, gw, gw == "paystack",
		)
	}

	type row struct {
		Gateway   string    `db:"gateway"`
		Enabled   bool      `db:"enabled"`
		Config    []byte    `db:"config"`
		UpdatedAt time.Time `db:"updated_at"`
	}
	rows, err := s.db.QueryxContext(ctx, `
		SELECT gateway, enabled, config, updated_at
		FROM store_payment_methods WHERE store_id=$1
		ORDER BY updated_at`, storeID)
	if err != nil {
		return nil, fmt.Errorf("list payment gateways: %w", err)
	}
	defer rows.Close()

	out := make([]dto.PaymentGatewayResp, 0, len(KnownGateways))
	for rows.Next() {
		var r row
		if err := rows.StructScan(&r); err != nil {
			return nil, err
		}
		cfg := map[string]any{}
		if len(r.Config) > 0 {
			_ = json.Unmarshal(r.Config, &cfg)
		}
		out = append(out, dto.PaymentGatewayResp{
			Gateway:   r.Gateway,
			Enabled:   r.Enabled,
			Config:    cfg,
			UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

// UpsertPaymentGateway enables/disables a gateway and stores optional config (e.g. public keys).
func (s *OrdersService) UpsertPaymentGateway(ctx context.Context, storeID uuid.UUID, gateway string, req dto.UpsertPaymentGatewayReq) (dto.PaymentGatewayResp, error) {
	cfgJSON, err := json.Marshal(req.Config)
	if err != nil {
		return dto.PaymentGatewayResp{}, fmt.Errorf("marshal config: %w", err)
	}

	var updatedAt time.Time
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO store_payment_methods (store_id, gateway, enabled, config)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (store_id, gateway) DO UPDATE
		  SET enabled=$3, config=$4, updated_at=NOW()
		RETURNING updated_at`,
		storeID, gateway, req.Enabled, cfgJSON,
	).Scan(&updatedAt)
	if err != nil {
		return dto.PaymentGatewayResp{}, fmt.Errorf("upsert gateway: %w", err)
	}

	cfg := map[string]any{}
	if req.Config != nil {
		cfg = req.Config
	}
	return dto.PaymentGatewayResp{
		Gateway:   gateway,
		Enabled:   req.Enabled,
		Config:    cfg,
		UpdatedAt: updatedAt.UTC().Format(time.RFC3339),
	}, nil
}

// GetPublicGateways returns only enabled gateways for a store — called by the storefront checkout.
func (s *OrdersService) GetPublicGateways(ctx context.Context, storeID uuid.UUID) ([]dto.PaymentGatewayResp, error) {
	type row struct {
		Gateway string `db:"gateway"`
		Config  []byte `db:"config"`
	}
	rows, err := s.db.QueryxContext(ctx, `
		SELECT gateway, config
		FROM store_payment_methods WHERE store_id=$1 AND enabled=true
		ORDER BY updated_at`, storeID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("get public gateways: %w", err)
	}
	defer func() {
		if rows != nil {
			rows.Close()
		}
	}()

	out := make([]dto.PaymentGatewayResp, 0)
	if rows != nil {
		for rows.Next() {
			var r row
			if err := rows.StructScan(&r); err != nil {
				continue
			}
			cfg := map[string]any{}
			if len(r.Config) > 0 {
				_ = json.Unmarshal(r.Config, &cfg)
			}
			out = append(out, dto.PaymentGatewayResp{
				Gateway: r.Gateway,
				Enabled: true,
				Config:  cfg,
			})
		}
	}

	// Default to paystack if nothing is configured yet
	if len(out) == 0 {
		out = append(out, dto.PaymentGatewayResp{
			Gateway: "paystack",
			Enabled: true,
			Config:  map[string]any{},
		})
	}
	return out, nil
}
