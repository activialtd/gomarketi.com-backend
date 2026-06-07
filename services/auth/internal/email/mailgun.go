package email

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MailgunClient sends transactional email via the Mailgun v3 API using only
// stdlib net/http — no Mailgun SDK needed for a single endpoint.
type MailgunClient struct {
	apiKey     string
	domain     string
	from       string
	httpClient *http.Client
}

// MailgunConfig holds the credentials and sender identity for Mailgun.
type MailgunConfig struct {
	APIKey string // Mailgun private API key
	Domain string // e.g. "mg.gomarketi.com"
	From   string // e.g. "GoMarket <noreply@gomarketi.com>"
}

// NewMailgunClient creates a MailgunClient. All fields in cfg are required.
func NewMailgunClient(cfg MailgunConfig) (*MailgunClient, error) {
	if cfg.APIKey == "" || cfg.Domain == "" || cfg.From == "" {
		return nil, fmt.Errorf("mailgun: APIKey, Domain, and From are required")
	}
	return &MailgunClient{
		apiKey: cfg.APIKey,
		domain: cfg.Domain,
		from:   cfg.From,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

// SendOTP sends a verification code email to the given address.
func (m *MailgunClient) SendOTP(ctx context.Context, to, otp string) error {
	endpoint := fmt.Sprintf("https://api.mailgun.net/v3/%s/messages", m.domain)

	body := url.Values{}
	body.Set("from", m.from)
	body.Set("to", to)
	body.Set("subject", "Your GoMarket verification code")
	body.Set("text", otpEmailText(otp))
	body.Set("html", otpEmailHTML(otp))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(body.Encode()))
	if err != nil {
		return fmt.Errorf("building mailgun request: %w", err)
	}
	req.SetBasicAuth("api", m.apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending email via mailgun: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("mailgun responded %d: %s", resp.StatusCode, string(b))
	}

	return nil
}

func otpEmailText(otp string) string {
	return fmt.Sprintf(
		"Your GoMarket verification code is: %s\n\nThis code expires in 10 minutes.\nDo not share it with anyone.",
		otp,
	)
}

func otpEmailHTML(otp string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family:sans-serif;max-width:480px;margin:0 auto;padding:32px 16px;">
  <h2 style="color:#1a1a1a;">Your GoMarket code</h2>
  <p style="font-size:14px;color:#555;">Use the code below to verify your email address.</p>
  <div style="background:#f5f5f5;border-radius:8px;padding:24px;text-align:center;margin:24px 0;">
    <span style="font-size:36px;font-weight:700;letter-spacing:8px;color:#1a1a1a;">%s</span>
  </div>
  <p style="font-size:12px;color:#999;">This code expires in 10 minutes. Do not share it with anyone.</p>
</body>
</html>`, otp)
}
