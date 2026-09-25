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

// ResendMailer sends welcome emails via the Resend HTTP API. Resend is the
// default provider: it works from anywhere over HTTPS, unlike SMTP which
// Railway and many hosts block outbound.
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

// SendWelcome fires the branded welcome email to a newly onboarded vendor.
func (m *ResendMailer) SendWelcome(ctx context.Context, to, vendorName, storeName, storeSlug, storeDomain string) error {
	if storeDomain == "" {
		storeDomain = "gomarketi.com"
	}
	storeURL := fmt.Sprintf("https://%s.%s", storeSlug, storeDomain)

	payload := map[string]any{
		"from":    m.from,
		"to":      []string{to},
		"subject": fmt.Sprintf("Your GoMarketi store %s is live!", storeName),
		"html":    welcomeHTML(vendorName, storeName, storeURL, storeSlug),
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
