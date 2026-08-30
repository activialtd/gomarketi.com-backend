// Package paystack provides a minimal wrapper around the community
// github.com/gray-adeyi/paystack SDK for creating a Paystack Customer and a
// Dedicated Virtual Account (DVA) for a vendor. It does not handle payments,
// subaccounts, or settlement — only account provisioning. (Paystack does not
// publish an official Go SDK; this is the most complete community one,
// confirmed to cover both the Customer and Dedicated Virtual Account
// endpoints this package needs.)
//
// When PAYSTACK_SECRET_KEY is not set the client runs in simulation mode,
// synthesizing a fake account so onboarding can be developed and tested
// without live credentials (same shape as services/identity/internal/smileid).
package paystack

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/gray-adeyi/paystack"
	"github.com/gray-adeyi/paystack/models"
	"github.com/rs/zerolog"
)

const preferredBank = "wema-bank" // Paystack's standard DVA provider

// Client wraps the Paystack SDK for customer + DVA creation.
// Construct via New(); use a single instance per service.
type Client struct {
	sdk     *sdk.PaystackClient
	log     zerolog.Logger
	simMode bool
}

// New creates a Client. When secretKey is empty the client enters simulation
// mode, synthesizing a fake customer/account instead of calling Paystack.
func New(secretKey string, log zerolog.Logger) *Client {
	if secretKey == "" {
		return &Client{log: log, simMode: true}
	}
	return &Client{sdk: sdk.NewClient(sdk.WithSecretKey(secretKey)), log: log}
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

// CreateDedicatedAccount creates a Dedicated Virtual Account (a unique NUBAN)
// for an existing Paystack customer.
func (c *Client) CreateDedicatedAccount(ctx context.Context, customerCode string) (accountNumber, bankName, accountName string, err error) {
	if c.simMode {
		c.log.Warn().Str("customer_code", customerCode).Msg("paystack: SIMULATION MODE — synthesizing a fake dedicated account")
		n := time.Now().UnixMilli() % 10000000000
		return fmt.Sprintf("%010d", n), "Wema Bank (simulated)", "GoMarketi Vendor (simulated)", nil
	}

	var resp models.Response[models.DedicatedAccount]
	if sdkErr := c.sdk.DedicatedVirtualAccounts.Create(ctx, customerCode, &resp,
		sdk.WithOptionalPayload("preferred_bank", preferredBank)); sdkErr != nil {
		return "", "", "", fmt.Errorf("paystack: create dedicated account: %w", sdkErr)
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
