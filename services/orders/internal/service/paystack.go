package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
)

type paystackVerifyResp struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Status    string `json:"status"` // "success" | "failed" | "abandoned"
		Amount    int64  `json:"amount"` // kobo
		Currency  string `json:"currency"`
		Reference string `json:"reference"`
	} `json:"data"`
}

// verifyPaystackTransaction confirms with Paystack's API that the transaction
// succeeded and that the amount matches expectedKobo.
//
// When PAYSTACK_SECRET_KEY is not set the check is skipped with a warning —
// this allows local development without a live Paystack account. In production
// the key must be set; without it credits will still be applied but the missing
// key will be logged on every order so the gap is visible.
func (s *OrdersService) verifyPaystackTransaction(ctx context.Context, reference string, expectedKobo int64) error {
	secretKey := os.Getenv("PAYSTACK_SECRET_KEY")
	if secretKey == "" {
		s.log.Warn().
			Str("reference", reference).
			Msg("PAYSTACK_SECRET_KEY not set — skipping payment verification (dev mode)")
		return nil
	}

	reqURL := "https://api.paystack.co/transaction/verify/" + reference
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("paystack verify: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+secretKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("paystack verify: request failed: %w", err)
	}
	defer resp.Body.Close()

	var body paystackVerifyResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("paystack verify: decode response: %w", err)
	}

	if !body.Status || body.Data.Status != "success" {
		s.log.Warn().
			Str("reference", reference).
			Str("paystack_status", body.Data.Status).
			Msg("Paystack verification failed")
		return apperrors.BadRequest("payment verification failed: transaction is " + body.Data.Status)
	}

	if body.Data.Amount != expectedKobo {
		s.log.Error().
			Str("reference", reference).
			Int64("expected_kobo", expectedKobo).
			Int64("paystack_kobo", body.Data.Amount).
			Msg("Paystack amount mismatch — possible tampering")
		return apperrors.BadRequest(fmt.Sprintf(
			"payment amount mismatch: expected %d kobo, Paystack recorded %d kobo",
			expectedKobo, body.Data.Amount,
		))
	}

	return nil
}
