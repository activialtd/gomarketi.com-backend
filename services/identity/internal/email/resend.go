package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ResendMailer sends account-ready emails via the Resend HTTP API. Resend is
// the provider this project actually uses — storefront's welcome email has
// been going out through it all along (see services/storefront/internal/email
// /resend.go), while this service only knew how to talk to Brevo and so fell
// back to the noop mailer, silently dropping every "your payment account is
// ready" email.
type ResendMailer struct {
	apiKey string
	from   string
	http   *http.Client
}

// NewResend returns a ResendMailer, or nil when apiKey is empty so the caller
// can fall through to the next provider.
func NewResend(apiKey, from string) *ResendMailer {
	if apiKey == "" {
		return nil
	}
	if from == "" {
		from = "GoMarketi <noreply@gomarketi.com>"
	}
	return &ResendMailer{
		apiKey: apiKey,
		from:   from,
		http:   &http.Client{Timeout: 10 * time.Second},
	}
}

// SendAccountReady fires the branded email showing the vendor their newly
// provisioned Paystack Dedicated Virtual Account details. Same body as the
// Brevo path — only the transport differs.
func (m *ResendMailer) SendAccountReady(ctx context.Context, to, vendorName, bankName, accountNumber, accountName string) error {
	payload := map[string]any{
		"from":    m.from,
		"to":      []string{to},
		"subject": "Welcome to GoMarketi — your payment account is ready",
		"html":    accountReadyHTML(vendorName, bankName, accountNumber, accountName),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("resend: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("resend: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("resend: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("resend: status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
