// Package email provides a minimal Brevo transactional email client used
// by the storefront service to send vendor welcome emails after store creation.
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client sends transactional email via the Brevo v3 API.
type Client struct {
	apiKey   string
	from     string
	fromName string
	http     *http.Client
}

// New returns nil (disabled) when apiKey is empty — callers must nil-check.
func New(apiKey, from, fromName string) *Client {
	if apiKey == "" {
		return nil
	}
	if fromName == "" {
		fromName = "GoMarketi"
	}
	return &Client{
		apiKey:   apiKey,
		from:     from,
		fromName: fromName,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

// SendWelcome fires a branded welcome email to a newly onboarded vendor.
// It is non-blocking: failures are logged by the caller, never returned.
func (c *Client) SendWelcome(ctx context.Context, to, vendorName, storeName, storeSlug, storeDomain string) error {
	if c == nil {
		return nil // emailer disabled
	}

	storeURL := fmt.Sprintf("https://%s.%s", storeSlug, storeDomain)
	if storeDomain == "" {
		storeDomain = "gomarketi.com"
		storeURL = fmt.Sprintf("https://%s.gomarketi.com", storeSlug)
	}

	payload := map[string]any{
		"sender":  map[string]string{"email": c.from, "name": c.fromName},
		"to":      []map[string]string{{"email": to, "name": vendorName}},
		"subject": fmt.Sprintf("Your GoMarketi store %s is live!", storeName),
		"htmlContent": welcomeHTML(vendorName, storeName, storeURL, storeSlug),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("welcome email marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.brevo.com/v3/smtp/email", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("welcome email request: %w", err)
	}
	req.Header.Set("api-key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("welcome email send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("welcome email brevo %d", resp.StatusCode)
	}
	return nil
}

func welcomeHTML(vendorName, storeName, storeURL, storeSlug string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1.0"/>
  <title>Welcome to GoMarketi</title>
</head>
<body style="margin:0;padding:0;background:#f4f7f6;font-family:'Segoe UI',Arial,sans-serif;">

  <!-- Wrapper -->
  <table width="100%%" cellpadding="0" cellspacing="0" style="background:#f4f7f6;padding:40px 16px;">
    <tr><td align="center">
      <table width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%%;">

        <!-- Logo header -->
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

        <!-- Hero card -->
        <tr>
          <td>
            <div style="background:#0A2E1A;border-radius:20px 20px 0 0;padding:40px 40px 32px;text-align:center;">
              <div style="font-size:48px;margin-bottom:16px;">🎉</div>
              <h1 style="margin:0 0 10px;color:#ffffff;font-size:28px;font-weight:800;letter-spacing:-0.5px;line-height:1.2;">
                Your store is live!
              </h1>
              <p style="margin:0;color:rgba(255,255,255,0.65);font-size:16px;line-height:1.5;">
                Welcome to GoMarketi, %s. Your storefront is ready and waiting for its first customer.
              </p>
            </div>
          </td>
        </tr>

        <!-- White body -->
        <tr>
          <td style="background:#ffffff;border-radius:0 0 20px 20px;padding:32px 40px 40px;">

            <!-- Store pill -->
            <div style="text-align:center;margin-bottom:28px;">
              <div style="display:inline-block;background:#F0FAF3;border:1px solid rgba(26,122,66,0.2);border-radius:100px;padding:10px 20px;">
                <span style="color:#1A7A42;font-size:14px;font-weight:700;">🏪 %s</span>
              </div>
            </div>

            <!-- CTA button -->
            <div style="text-align:center;margin-bottom:32px;">
              <a href="%s" style="display:inline-block;background:#1A7A42;color:#ffffff;text-decoration:none;font-size:15px;font-weight:700;padding:14px 32px;border-radius:12px;letter-spacing:0.2px;">
                Visit your storefront →
              </a>
              <p style="margin:10px 0 0;color:#94a3b8;font-size:12px;">%s</p>
            </div>

            <!-- Divider -->
            <div style="height:1px;background:#f1f5f9;margin:0 0 28px;"></div>

            <!-- Next steps -->
            <h2 style="margin:0 0 16px;color:#1C1C1C;font-size:16px;font-weight:700;">What to do next</h2>
            <table cellpadding="0" cellspacing="0" width="100%%">
              %s
            </table>

            <!-- Divider -->
            <div style="height:1px;background:#f1f5f9;margin:28px 0;"></div>

            <!-- Footer note -->
            <p style="margin:0;color:#94a3b8;font-size:13px;line-height:1.6;text-align:center;">
              Questions? Reply to this email or chat with us at
              <a href="https://gomarketi.com" style="color:#1A7A42;text-decoration:none;">gomarketi.com</a>.
              <br/>We're here to help you succeed. 🚀
            </p>
          </td>
        </tr>

        <!-- Footer -->
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
</html>`,
		vendorName, storeName, storeURL, storeURL,
		nextStepsRows(),
	)
}

func nextStepsRows() string {
	steps := []struct{ icon, title, desc string }{
		{"📦", "Add your first product", "Upload photos, set your price, and publish in under 2 minutes."},
		{"🎨", "Customise your storefront", "Choose colours, fonts, and your store layout — no code needed."},
		{"✅", "Complete KYC verification", "Verify your identity to unlock automated payouts and the GoMarketi badge."},
		{"📣", "Share your store link", "Send your store URL to customers on WhatsApp, Instagram, or anywhere."},
	}
	out := ""
	for _, s := range steps {
		out += fmt.Sprintf(`
<tr>
  <td style="padding:0 0 14px 0;vertical-align:top;">
    <table cellpadding="0" cellspacing="0">
      <tr>
        <td style="width:36px;height:36px;background:#F0FAF3;border-radius:9px;text-align:center;vertical-align:middle;font-size:18px;">%s</td>
        <td style="padding-left:12px;vertical-align:middle;">
          <p style="margin:0;font-size:14px;font-weight:600;color:#1C1C1C;">%s</p>
          <p style="margin:2px 0 0;font-size:13px;color:#6b7280;">%s</p>
        </td>
      </tr>
    </table>
  </td>
</tr>`, s.icon, s.title, s.desc)
	}
	return out
}
