// Package email sends the "your GoMarketi payment account is ready" email
// fired after a vendor's Paystack Dedicated Virtual Account is provisioned.
// Mirrors services/storefront/internal/email's Brevo-backed pattern.
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// AccountMailer is the interface every email backend must satisfy.
type AccountMailer interface {
	SendAccountReady(ctx context.Context, to, vendorName, bankName, accountNumber, accountName string) error
}

// NoopMailer discards emails — used when no provider is configured.
type NoopMailer struct{}

func (NoopMailer) SendAccountReady(context.Context, string, string, string, string, string) error { return nil }

// ── Brevo ─────────────────────────────────────────────────────────────────────

// BrevoMailer sends account-ready emails via the Brevo v3 HTTP API.
type BrevoMailer struct {
	apiKey   string
	from     string
	fromName string
	http     *http.Client
}

// NewBrevo returns a BrevoMailer. Returns nil when apiKey is empty.
func NewBrevo(apiKey, from, fromName string) *BrevoMailer {
	if apiKey == "" {
		return nil
	}
	if fromName == "" {
		fromName = "GoMarketi"
	}
	return &BrevoMailer{
		apiKey:   apiKey,
		from:     from,
		fromName: fromName,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

// SendAccountReady fires a branded email showing the vendor their newly
// provisioned Paystack Dedicated Virtual Account details.
func (c *BrevoMailer) SendAccountReady(ctx context.Context, to, vendorName, bankName, accountNumber, accountName string) error {
	payload := map[string]any{
		"sender":      map[string]string{"email": c.from, "name": c.fromName},
		"to":          []map[string]string{{"email": to, "name": vendorName}},
		"subject":     "Welcome to GoMarketi — your payment account is ready",
		"htmlContent": accountReadyHTML(vendorName, bankName, accountNumber, accountName),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("account ready email marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.brevo.com/v3/smtp/email", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("account ready email request: %w", err)
	}
	req.Header.Set("api-key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("account ready email send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("account ready email brevo %d", resp.StatusCode)
	}
	return nil
}

func accountReadyHTML(vendorName, bankName, accountNumber, accountName string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1.0"/>
  <title>Welcome to GoMarketi</title>
</head>
<body style="margin:0;padding:0;background:#f4f7f6;font-family:'Segoe UI',Arial,sans-serif;">
  <table width="100%%" cellpadding="0" cellspacing="0" style="background:#f4f7f6;padding:40px 16px;">
    <tr><td align="center">
      <table width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%%;">

        <tr>
          <td align="center" style="padding:0 0 24px 0;">
            <div style="display:inline-flex;align-items:center;gap:10px;">
              <div style="width:40px;height:40px;background:#1A7A42;border-radius:10px;display:flex;align-items:center;justify-content:center;">
                <span style="color:#fff;font-size:20px;font-weight:900;line-height:1;">G</span>
              </div>
              <span style="font-size:22px;font-weight:800;color:#0A2E1A;letter-spacing:-0.5px;">GoMarketi</span>
            </div>
          </td>
        </tr>

        <tr>
          <td>
            <div style="background:#0A2E1A;border-radius:20px 20px 0 0;padding:40px 40px 32px;text-align:center;">
              <div style="font-size:48px;margin-bottom:16px;">🎉</div>
              <h1 style="margin:0 0 10px;color:#ffffff;font-size:26px;font-weight:800;letter-spacing:-0.5px;line-height:1.2;">
                Welcome to GoMarketi, %s!
              </h1>
              <p style="margin:0;color:rgba(255,255,255,0.65);font-size:15px;line-height:1.5;">
                Your dedicated payment account is ready to receive funds.
              </p>
            </div>
          </td>
        </tr>

        <tr>
          <td style="background:#ffffff;border-radius:0 0 20px 20px;padding:32px 40px 40px;">

            <div style="background:#F0FAF3;border:1px solid rgba(26,122,66,0.2);border-radius:14px;padding:20px 24px;margin-bottom:28px;">
              <p style="margin:0 0 14px;font-size:12px;font-weight:700;text-transform:uppercase;letter-spacing:0.06em;color:#1A7A42;">Your GoMarketi account</p>
              <table cellpadding="0" cellspacing="0" width="100%%">
                <tr>
                  <td style="padding:6px 0;color:#6b7280;font-size:13px;">Bank</td>
                  <td style="padding:6px 0;text-align:right;color:#1C1C1C;font-size:13px;font-weight:700;">%s</td>
                </tr>
                <tr>
                  <td style="padding:6px 0;color:#6b7280;font-size:13px;">Account number</td>
                  <td style="padding:6px 0;text-align:right;color:#1C1C1C;font-size:15px;font-weight:800;letter-spacing:0.5px;">%s</td>
                </tr>
                <tr>
                  <td style="padding:6px 0;color:#6b7280;font-size:13px;">Account name</td>
                  <td style="padding:6px 0;text-align:right;color:#1C1C1C;font-size:13px;font-weight:700;">%s</td>
                </tr>
              </table>
            </div>

            <div style="text-align:center;margin-bottom:32px;">
              <a href="https://vendor.gomarketi.com" style="display:inline-block;background:#1A7A42;color:#ffffff;text-decoration:none;font-size:15px;font-weight:700;padding:14px 32px;border-radius:12px;letter-spacing:0.2px;">
                Go to your dashboard →
              </a>
            </div>

            <div style="height:1px;background:#f1f5f9;margin:0 0 28px;"></div>

            <p style="margin:0;color:#94a3b8;font-size:13px;line-height:1.6;text-align:center;">
              Questions? Reply to this email or chat with us at
              <a href="https://gomarketi.com" style="color:#1A7A42;text-decoration:none;">gomarketi.com</a>.
              <br/>We're here to help you succeed. 🚀
            </p>
          </td>
        </tr>

        <tr>
          <td style="padding:24px 0 0;text-align:center;">
            <p style="margin:0;color:#b0bec5;font-size:12px;">
              © 2026 GoMarketi · Helping Nigerian merchants sell online<br/>
              <a href="https://vendor.gomarketi.com" style="color:#94a3b8;text-decoration:none;">Dashboard</a> ·
              <a href="https://gomarketi.com/privacy" style="color:#94a3b8;text-decoration:none;">Privacy</a>
            </p>
          </td>
        </tr>

      </table>
    </td></tr>
  </table>
</body>
</html>`, vendorName, bankName, accountNumber, accountName)
}
