// Package paystack provides a minimal wrapper for creating a Paystack
// Customer and a Dedicated Virtual Account (DVA) for a vendor. It does not
// handle payments, subaccounts, or settlement — only account provisioning.
//
// Customer creation uses the community github.com/gray-adeyi/paystack SDK
// (Paystack does not publish an official Go SDK). Dedicated Virtual Account
// creation deliberately does NOT use that SDK: its DedicatedAccountBank.Id
// field is typed string, but Paystack's live API returns a JSON number for
// bank.id, so the SDK fails to decode every real (non-403, non-simulated)
// DVA response — confirmed against a live key, latest SDK version (v0.2.1)
// at the time. CreateDedicatedAccount calls Paystack directly instead, same
// raw-HTTP pattern as services/orders/internal/service/paystack.go, with a
// local response struct that simply omits the field the SDK gets wrong.
//
// When PAYSTACK_SECRET_KEY is not set the client runs in simulation mode,
// synthesizing a fake account so onboarding can be developed and tested
// without live credentials (same shape as services/identity/internal/smileid).
package paystack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdk "github.com/gray-adeyi/paystack"
	"github.com/gray-adeyi/paystack/models"
	"github.com/rs/zerolog"
)

const preferredBank = "wema-bank" // Paystack's standard DVA provider

// Client wraps the Paystack SDK (customer creation) and a plain HTTP client
// (DVA creation — see package doc). Construct via New(); use a single
// instance per service.
type Client struct {
	sdk       *sdk.PaystackClient
	secretKey string
	log       zerolog.Logger
	simMode   bool
}

// New creates a Client. When secretKey is empty the client enters simulation
// mode, synthesizing a fake customer/account instead of calling Paystack.
func New(secretKey string, log zerolog.Logger) *Client {
	if secretKey == "" {
		return &Client{log: log, simMode: true}
	}
	return &Client{sdk: sdk.NewClient(sdk.WithSecretKey(secretKey)), secretKey: secretKey, log: log}
}

// CreateCustomer registers a Paystack Customer for a vendor and returns the
// customer_code needed to create a Dedicated Virtual Account for them.
func (c *Client) CreateCustomer(ctx context.Context, email, firstName, lastName, phone string) (string, error) {
	if c.simMode {
		c.log.Warn().Str("email", email).Msg("paystack: SIMULATION MODE — set PAYSTACK_SECRET_KEY for live account creation")
		return fmt.Sprintf("SIM_CUS_%d", time.Now().UnixMilli()), nil
	}

	var resp models.Response[models.Customer]
	opts := []sdk.OptionalPayload{}
	if phone != "" {
		opts = append(opts, sdk.WithOptionalPayload("phone", phone))
	}
	if err := c.sdk.Customers.Create(ctx, email, firstName, lastName, &resp, opts...); err != nil {
		return "", fmt.Errorf("paystack: create customer: %w", err)
	}
	if !resp.Status || resp.Data.CustomerCode == "" {
		return "", fmt.Errorf("paystack: create customer: %s", resp.Message)
	}
	return resp.Data.CustomerCode, nil
}

// dedicatedAccountResp deliberately omits bank.id (and every other field
// this package doesn't need) — see the package doc for why the SDK's own
// typed model can't be used here.
type dedicatedAccountResp struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		AccountNumber string `json:"account_number"`
		AccountName   string `json:"account_name"`
		Bank          struct {
			Name string `json:"name"`
		} `json:"bank"`
	} `json:"data"`
}

// CreateDedicatedAccount creates a Dedicated Virtual Account (a unique NUBAN)
// for an existing Paystack customer.
func (c *Client) CreateDedicatedAccount(ctx context.Context, customerCode string) (accountNumber, bankName, accountName string, err error) {
	if c.simMode {
		c.log.Warn().Str("customer_code", customerCode).Msg("paystack: SIMULATION MODE — synthesizing a fake dedicated account")
		n := time.Now().UnixMilli() % 10000000000
		return fmt.Sprintf("%010d", n), "Wema Bank (simulated)", "GoMarketi Vendor (simulated)", nil
	}

	payload, err := json.Marshal(map[string]string{
		"customer":       customerCode,
		"preferred_bank": preferredBank,
	})
	if err != nil {
		return "", "", "", fmt.Errorf("paystack: create dedicated account: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.paystack.co/dedicated_account", bytes.NewReader(payload))
	if err != nil {
		return "", "", "", fmt.Errorf("paystack: create dedicated account: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.secretKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	httpResp, err := client.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("paystack: create dedicated account: request failed: %w", err)
	}
	defer httpResp.Body.Close()

	var resp dedicatedAccountResp
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return "", "", "", fmt.Errorf("paystack: create dedicated account: decode response: %w", err)
	}
	if !resp.Status || resp.Data.AccountNumber == "" {
		return "", "", "", fmt.Errorf("paystack: create dedicated account: %s", resp.Message)
	}
	return resp.Data.AccountNumber, resp.Data.Bank.Name, resp.Data.AccountName, nil
}

// SplitName splits a full name into (first, last) for Paystack's Customer API.
// A single-token name yields an empty last name.
func SplitName(fullName string) (first, last string) {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return "", ""
	}
	parts := strings.SplitN(fullName, " ", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.TrimSpace(parts[1])
}
