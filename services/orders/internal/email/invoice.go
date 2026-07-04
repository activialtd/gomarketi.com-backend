package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"time"
)

// InvoiceItem is a single line item in an order email.
type InvoiceItem struct {
	Name      string
	ImageURL  string
	Quantity  int
	PriceKobo int64
}

// ── Customer invoice ───────────────────────────────────────────────────────────

// SendInvoice sends the order-confirmed invoice to the customer.
// Call asynchronously — errors should be logged, not returned to the caller.
func SendInvoice(ctx context.Context, to, customerName, orderID, storeSlug, storeName string, totalKobo int64, items []InvoiceItem) error {
	orderURL := buildOrderURL(storeSlug, orderID, to)
	subject := fmt.Sprintf("Order confirmed — %s (#%s)", storeName, shortID(orderID))
	html := customerInvoiceHTML(customerName, storeName, orderID, orderURL, totalKobo, items)
	return sendMail(ctx, to, subject, html, "Your order has been confirmed. View it at: "+orderURL)
}

// ── Pre-payment cart summary ──────────────────────────────────────────────────

// SendCartSummary fires when the customer clicks Pay — before Paystack opens.
// It gives them a record of what they ordered even if the browser closes mid-flow.
func SendCartSummary(ctx context.Context, to, customerName, storeSlug, storeName string, totalKobo int64, items []InvoiceItem) error {
	trackURL := buildOrderURL(storeSlug, "", to) // no order ID yet — links to /track page
	// strip /orders/ segment from the URL — point to /track instead
	trackURL = buildTrackURL(storeSlug, to)
	subject := fmt.Sprintf("Your order from %s — we're processing your payment", storeName)
	html := cartSummaryHTML(customerName, storeName, trackURL, totalKobo, items)
	plain := fmt.Sprintf("Hi %s,\n\nYou're in the process of placing an order with %s.\nTotal: %s\n\nTo track your order after payment: %s",
		customerName, storeName, fmtNaira(totalKobo), trackURL)
	return sendMail(ctx, to, subject, html, plain)
}

func buildTrackURL(storeSlug, customerEmail string) string {
	rootDomain := getenv("STOREFRONT_ROOT_DOMAIN", "")
	if rootDomain != "" {
		return fmt.Sprintf("https://%s.%s/track", storeSlug, rootDomain)
	}
	localBase := getenv("STOREFRONT_LOCAL_BASE", "localhost:3001")
	return fmt.Sprintf("http://%s.%s/track", storeSlug, localBase)
}

func cartSummaryHTML(customerName, storeName, trackURL string, totalKobo int64, items []InvoiceItem) string {
	var rows strings.Builder
	for _, item := range items {
		lineTotal := fmtNaira(item.PriceKobo * int64(item.Quantity))
		unitPrice := fmtNaira(item.PriceKobo)
		imgCell := `<div style="width:44px;height:44px;border-radius:6px;background:#f1f5f9;display:inline-flex;align-items:center;justify-content:center;font-size:18px;">📦</div>`
		if item.ImageURL != "" {
			imgCell = fmt.Sprintf(`<img src="%s" width="44" height="44" style="border-radius:6px;object-fit:cover;display:block;" alt="">`, item.ImageURL)
		}
		rows.WriteString(fmt.Sprintf(`
<tr>
  <td style="padding:12px 0;border-bottom:1px solid #f1f5f9;vertical-align:middle;">
    <table cellpadding="0" cellspacing="0"><tr>
      <td style="padding-right:12px;">%s</td>
      <td>
        <p style="margin:0;font-size:14px;font-weight:600;color:#1C1C1C;line-height:1.3;">%s</p>
        <p style="margin:4px 0 0;font-size:12px;color:#94a3b8;">Qty %d · %s each</p>
      </td>
    </tr></table>
  </td>
  <td style="padding:12px 0;border-bottom:1px solid #f1f5f9;font-size:14px;font-weight:700;color:#1C1C1C;text-align:right;vertical-align:middle;">%s</td>
</tr>`, imgCell, item.Name, item.Quantity, unitPrice, lineTotal))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Order Processing</title></head>
<body style="margin:0;padding:0;background:#f0f4f8;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;">
<table width="100%%" cellpadding="0" cellspacing="0" style="background:#f0f4f8;padding:32px 16px 48px;">
<tr><td align="center">
<table width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%%;">

  <tr><td style="background:#0E1F13;border-radius:16px 16px 0 0;padding:36px 40px;text-align:center;">
    <p style="margin:0 0 6px;font-size:11px;font-weight:700;letter-spacing:0.18em;text-transform:uppercase;color:rgba(255,255,255,0.45);">Payment in progress</p>
    <h1 style="margin:0;font-size:24px;font-weight:800;color:#fff;letter-spacing:-0.4px;">🛒 &nbsp;%s</h1>
    <p style="margin:8px 0 0;font-size:14px;color:rgba(255,255,255,0.5);">Hi %s — here's a summary of your order.</p>
  </td></tr>

  <tr><td style="background:#1A7A42;padding:12px 40px;text-align:center;">
    <p style="margin:0;font-size:12px;font-weight:700;color:rgba(255,255,255,0.8);">
      Complete your payment to confirm this order &nbsp;·&nbsp; Total: <span style="color:#fff;">%s</span>
    </p>
  </td></tr>

  <tr><td style="background:#fff;border-radius:0 0 16px 16px;padding:36px 40px;">
    <p style="margin:0 0 20px;font-size:15px;color:#374151;line-height:1.6;">
      You're in the process of placing an order with <strong>%s</strong>. Once your payment goes through, we'll send a full receipt with your order ID and tracking link.
    </p>

    <p style="margin:0 0 12px;font-size:11px;font-weight:700;letter-spacing:0.12em;text-transform:uppercase;color:#94a3b8;">Items in your cart</p>
    <table width="100%%" cellpadding="0" cellspacing="0">
      <tr>
        <th style="text-align:left;font-size:11px;color:#94a3b8;font-weight:600;padding-bottom:8px;border-bottom:2px solid #f1f5f9;">Product</th>
        <th style="text-align:right;font-size:11px;color:#94a3b8;font-weight:600;padding-bottom:8px;border-bottom:2px solid #f1f5f9;">Total</th>
      </tr>
      %s
      <tr>
        <td style="padding:16px 0 0;font-size:13px;font-weight:700;color:#6b7280;">Order total</td>
        <td style="padding:16px 0 0;font-size:22px;font-weight:900;color:#1A7A42;text-align:right;">%s</td>
      </tr>
    </table>

    <div style="height:1px;background:#f1f5f9;margin:28px 0;"></div>

    <p style="margin:0 0 16px;font-size:14px;color:#374151;line-height:1.5;">
      Already paid? You can track your order using the button below.
    </p>
    <div style="text-align:center;margin:20px 0 8px;">
      <a href="%s" style="display:inline-block;background:#1A7A42;color:#fff;text-decoration:none;font-size:15px;font-weight:700;padding:16px 40px;border-radius:12px;">
        Track my order &rarr;
      </a>
    </div>

    <div style="height:1px;background:#f1f5f9;margin:28px 0;"></div>
    <p style="margin:0;font-size:12px;color:#94a3b8;text-align:center;line-height:1.7;">
      Questions? Contact %s directly.<br>
      Powered by <a href="https://gomarketi.com" style="color:#1A7A42;text-decoration:none;">GoMarketi</a>
    </p>
  </td></tr>

  <tr><td style="padding:24px 0;text-align:center;">
    <p style="margin:0;font-size:11px;color:#94a3b8;">&copy; 2026 GoMarketi &middot; Made in Nigeria 🇳🇬</p>
  </td></tr>
</table>
</td></tr>
</table>
</body>
</html>`,
		storeName, customerName,
		fmtNaira(totalKobo),
		storeName,
		rows.String(), fmtNaira(totalKobo),
		trackURL,
		storeName,
	)
}

// ── Campaign / newsletter mail ────────────────────────────────────────────────

// SendCampaignMail delivers a merchant-composed newsletter to one subscriber.
// The body_html is merchant-authored; we wrap it in a minimal branded shell.
func SendCampaignMail(ctx context.Context, to, recipientName, storeName, subject, bodyHTML, plainText string) error {
	wrapped := campaignWrapHTML(recipientName, storeName, bodyHTML)
	return sendMail(ctx, to, subject, wrapped, plainText)
}

func campaignWrapHTML(recipientName, storeName, body string) string {
	greeting := recipientName
	if greeting == "" {
		greeting = "there"
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;padding:0;background:#f0f4f8;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;">
<table width="100%%" cellpadding="0" cellspacing="0" style="background:#f0f4f8;padding:32px 16px 48px;">
<tr><td align="center">
<table width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%%;">
  <tr><td style="background:#0E1F13;border-radius:16px 16px 0 0;padding:24px 40px;">
    <p style="margin:0;font-size:15px;font-weight:800;color:#fff;">%s</p>
  </td></tr>
  <tr><td style="background:#fff;border-radius:0 0 16px 16px;padding:32px 40px;">
    <p style="margin:0 0 20px;font-size:14px;color:#6b7280;">Hi %s,</p>
    <div style="font-size:15px;color:#1C1C1C;line-height:1.7;">%s</div>
    <div style="height:1px;background:#f1f5f9;margin:28px 0;"></div>
    <p style="margin:0;font-size:11px;color:#94a3b8;text-align:center;">
      You're receiving this because you subscribed to updates from %s.<br>
      Powered by <a href="https://gomarketi.com" style="color:#1A7A42;text-decoration:none;">GoMarketi</a>
    </p>
  </td></tr>
  <tr><td style="padding:20px 0;text-align:center;">
    <p style="margin:0;font-size:11px;color:#94a3b8;">&copy; 2026 GoMarketi &middot; Made in Nigeria 🇳🇬</p>
  </td></tr>
</table>
</td></tr>
</table>
</body>
</html>`, storeName, greeting, body, storeName)
}

// ── Order status update notification ─────────────────────────────────────────

// SendStatusUpdate notifies the customer that their order status has changed.
// Call asynchronously — errors should be logged, not returned to the caller.
func SendStatusUpdate(ctx context.Context, to, customerName, orderID, storeSlug, storeName string, newStatus string) error {
	orderURL := buildOrderURL(storeSlug, orderID, to)
	subject := fmt.Sprintf("Your order has been %s — %s", humanStatus(newStatus), storeName)
	html := statusUpdateHTML(customerName, storeName, orderID, orderURL, newStatus)
	plain := fmt.Sprintf("Your order #%s from %s is now %s.\nTrack it: %s", shortID(orderID), storeName, humanStatus(newStatus), orderURL)
	return sendMail(ctx, to, subject, html, plain)
}

func humanStatus(s string) string {
	switch s {
	case "confirmed":
		return "confirmed"
	case "shipped":
		return "shipped"
	case "delivered":
		return "delivered"
	case "cancelled":
		return "cancelled"
	default:
		return s
	}
}

func statusIcon(s string) string {
	switch s {
	case "confirmed":
		return "✅"
	case "shipped":
		return "🚚"
	case "delivered":
		return "🎉"
	case "cancelled":
		return "❌"
	default:
		return "📦"
	}
}

func statusColor(s string) string {
	switch s {
	case "confirmed":
		return "#1A7A42"
	case "shipped":
		return "#3b82f6"
	case "delivered":
		return "#1A7A42"
	case "cancelled":
		return "#ef4444"
	default:
		return "#6b7280"
	}
}

func statusUpdateHTML(customerName, storeName, orderID, orderURL, status string) string {
	sid := shortID(orderID)
	icon := statusIcon(status)
	color := statusColor(status)
	label := humanStatus(status)

	var message string
	switch status {
	case "confirmed":
		message = "Your order has been confirmed and is being prepared. We'll let you know as soon as it's on the way."
	case "shipped":
		message = "Great news — your order is on its way! The seller will share delivery details with you directly."
	case "delivered":
		message = "Your order has been delivered. We hope you love what you received! Leave the seller a review."
	case "cancelled":
		message = "Your order has been cancelled. If you have any questions, please contact the store directly."
	default:
		message = fmt.Sprintf("Your order status has been updated to <strong>%s</strong>.", label)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Order Update</title></head>
<body style="margin:0;padding:0;background:#f0f4f8;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;">

<table width="100%%" cellpadding="0" cellspacing="0" style="background:#f0f4f8;padding:32px 16px 48px;">
<tr><td align="center">
<table width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%%;">

  <tr><td style="background:#0E1F13;border-radius:16px 16px 0 0;padding:36px 40px;text-align:center;">
    <p style="margin:0 0 6px;font-size:32px;">%s</p>
    <h1 style="margin:0;font-size:22px;font-weight:800;color:#fff;letter-spacing:-0.4px;">Order update</h1>
    <p style="margin:6px 0 0;font-size:14px;color:rgba(255,255,255,0.5);">Hi %s — here's your latest update.</p>
  </td></tr>

  <tr><td style="background:%s;padding:14px 40px;text-align:center;">
    <p style="margin:0;font-size:13px;font-weight:800;color:#fff;text-transform:uppercase;letter-spacing:0.1em;">
      Status: %s &nbsp;·&nbsp; Order #%s
    </p>
  </td></tr>

  <tr><td style="background:#fff;border-radius:0 0 16px 16px;padding:36px 40px;">
    <p style="margin:0 0 24px;font-size:15px;color:#374151;line-height:1.7;">%s</p>

    <div style="text-align:center;margin:28px 0;">
      <a href="%s" style="display:inline-block;background:%s;color:#fff;text-decoration:none;font-size:15px;font-weight:700;padding:16px 40px;border-radius:12px;">
        Track your order &rarr;
      </a>
    </div>

    <div style="height:1px;background:#f1f5f9;margin:28px 0;"></div>
    <p style="margin:0;font-size:12px;color:#94a3b8;text-align:center;line-height:1.7;">
      Questions? Contact %s directly.<br>
      Powered by <a href="https://gomarketi.com" style="color:#1A7A42;text-decoration:none;">GoMarketi</a>
    </p>
  </td></tr>

  <tr><td style="padding:24px 0;text-align:center;">
    <p style="margin:0;font-size:11px;color:#94a3b8;">&copy; 2026 GoMarketi &middot; Made in Nigeria 🇳🇬</p>
  </td></tr>

</table>
</td></tr>
</table>

</body>
</html>`,
		icon, customerName,
		color, label, sid,
		message,
		orderURL, color,
		storeName,
	)
}

// ── Vendor new-order notification ─────────────────────────────────────────────

// SendVendorAlert emails the store owner when a new order arrives.
// Call asynchronously — errors should be logged, not returned to the caller.
func SendVendorAlert(ctx context.Context, vendorEmail, storeName, orderID, customerName, customerEmail, customerPhone, deliveryAddress string, totalKobo int64, items []InvoiceItem) error {
	dashboardBase := getenv("VENDOR_BASE_URL", "http://localhost:3000")
	orderURL := fmt.Sprintf("%s/merchant/orders", dashboardBase)
	subject := fmt.Sprintf("New order #%s — %s from %s", shortID(orderID), fmtNaira(totalKobo), customerName)
	html := vendorAlertHTML(storeName, orderID, orderURL, customerName, customerEmail, customerPhone, deliveryAddress, totalKobo, items)
	plain := fmt.Sprintf("New order #%s placed.\nCustomer: %s (%s)\nTotal: %s\nView orders: %s", shortID(orderID), customerName, customerEmail, fmtNaira(totalKobo), orderURL)
	return sendMail(ctx, vendorEmail, subject, html, plain)
}

// ── Mail transport ─────────────────────────────────────────────────────────────

// sendMail dispatches an email using whichever provider is configured.
// Priority: Gmail API (same creds as auth service) → SMTP.
func sendMail(ctx context.Context, to, subject, html, plainText string) error {
	if rt := getenv("GMAIL_REFRESH_TOKEN", ""); rt != "" {
		return sendMailGmail(ctx, to, subject, html)
	}
	return sendMailSMTP(ctx, to, subject, html, plainText)
}

// sendMailGmail sends via the Gmail REST API using OAuth2 — the same approach
// as the auth service (same env vars: GMAIL_CLIENT_ID, GMAIL_CLIENT_SECRET,
// GMAIL_REFRESH_TOKEN, GMAIL_FROM).
func sendMailGmail(ctx context.Context, to, subject, html string) error {
	clientID := getenv("GMAIL_CLIENT_ID", "")
	clientSecret := getenv("GMAIL_CLIENT_SECRET", "")
	refreshToken := getenv("GMAIL_REFRESH_TOKEN", "")
	from := getenv("GMAIL_FROM", "")

	if clientID == "" || clientSecret == "" || refreshToken == "" || from == "" {
		return fmt.Errorf("gmail: GMAIL_CLIENT_ID, GMAIL_CLIENT_SECRET, GMAIL_REFRESH_TOKEN and GMAIL_FROM are all required")
	}

	// Exchange refresh token for a short-lived access token.
	tokenBody := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://oauth2.googleapis.com/token",
		strings.NewReader(tokenBody.Encode()))
	if err != nil {
		return fmt.Errorf("gmail: build token request: %w", err)
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	hc := &http.Client{Timeout: 15 * time.Second}
	tokenResp, err := hc.Do(tokenReq)
	if err != nil {
		return fmt.Errorf("gmail: token request: %w", err)
	}
	defer tokenResp.Body.Close()

	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err = json.NewDecoder(tokenResp.Body).Decode(&tok); err != nil {
		return fmt.Errorf("gmail: decode token: %w", err)
	}
	if tok.Error != "" {
		return fmt.Errorf("gmail: token error: %s — %s", tok.Error, tok.ErrorDesc)
	}

	// Build RFC 2822 message.
	raw := strings.Join([]string{
		"From: GoMarketi <" + from + ">",
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		html,
	}, "\r\n")
	encoded := base64.URLEncoding.EncodeToString([]byte(raw))

	payload, err := json.Marshal(map[string]string{"raw": encoded})
	if err != nil {
		return fmt.Errorf("gmail: marshal: %w", err)
	}

	sendReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://gmail.googleapis.com/gmail/v1/users/me/messages/send",
		bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("gmail: build send request: %w", err)
	}
	sendReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	sendReq.Header.Set("Content-Type", "application/json")

	sendResp, err := hc.Do(sendReq)
	if err != nil {
		return fmt.Errorf("gmail: send: %w", err)
	}
	defer sendResp.Body.Close()

	if sendResp.StatusCode >= 400 {
		var errBody struct {
			Error struct {
				Message string `json:"message"`
				Code    int    `json:"code"`
			} `json:"error"`
		}
		json.NewDecoder(sendResp.Body).Decode(&errBody) //nolint:errcheck
		return fmt.Errorf("gmail: api error %d: %s", errBody.Error.Code, errBody.Error.Message)
	}

	return nil
}

// sendMailSMTP is a plain SMTP fallback (production only — requires credentials).
func sendMailSMTP(ctx context.Context, to, subject, html, plainText string) error {
	host := getenv("SMTP_HOST", "")
	port := getenv("SMTP_PORT", "465")
	user := getenv("SMTP_USERNAME", "")
	pass := getenv("SMTP_PASSWORD", "")
	from := getenv("SMTP_FROM", user)

	if host == "" || user == "" || pass == "" {
		return fmt.Errorf("no email provider configured: set GMAIL_REFRESH_TOKEN or SMTP_* in .env")
	}

	msg := buildMsg(from, to, subject, html, plainText)
	addr := net.JoinHostPort(host, port)
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	var c *smtp.Client
	var err error
	if port == "465" {
		conn, e := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
		if e != nil {
			return fmt.Errorf("smtp tls dial: %w", e)
		}
		c, err = smtp.NewClient(conn, host)
	} else {
		conn, e := dialer.DialContext(ctx, "tcp", addr)
		if e != nil {
			return fmt.Errorf("smtp dial: %w", e)
		}
		c, err = smtp.NewClient(conn, host)
		if err == nil {
			err = c.StartTLS(tlsCfg)
		}
	}
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	if err = c.Auth(smtp.PlainAuth("", user, pass, host)); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err = c.Mail(from); err != nil {
		return err
	}
	if err = c.Rcpt(to); err != nil {
		return err
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}
	defer wc.Close()
	_, err = wc.Write([]byte(msg))
	return err
}

// ── Helpers ────────────────────────────────────────────────────────────────────

// buildOrderURL constructs the customer-facing tracking URL.
// Uses STOREFRONT_ROOT_DOMAIN to build a subdomain URL:
//
//	cobi.gomarketi.com/orders/{id}?email={email}
//
// Falls back to path-based if the env var is not set:
//
//	localhost:3001/storefront/cobi/orders/{id}?email={email}
func buildOrderURL(storeSlug, orderID, customerEmail string) string {
	rootDomain := getenv("STOREFRONT_ROOT_DOMAIN", "")
	if rootDomain != "" {
		// subdomain format: cobi.gomarketi.com/orders/{id}?email={email}
		return fmt.Sprintf("https://%s.%s/orders/%s?email=%s",
			storeSlug, rootDomain, orderID, urlEscape(customerEmail))
	}
	// local dev fallback: cobi.localhost:3001/orders/{id}?email={email}
	localBase := getenv("STOREFRONT_LOCAL_BASE", "localhost:3001")
	return fmt.Sprintf("http://%s.%s/orders/%s?email=%s",
		storeSlug, localBase, orderID, urlEscape(customerEmail))
}

func urlEscape(s string) string {
	var out strings.Builder
	for _, b := range []byte(s) {
		if b == '@' {
			out.WriteString("%40")
		} else if b == '+' {
			out.WriteString("%2B")
		} else if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-' || b == '_' || b == '.' || b == '~' {
			out.WriteByte(b)
		} else {
			out.WriteString(fmt.Sprintf("%%%02X", b))
		}
	}
	return out.String()
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func shortID(id string) string {
	if len(id) >= 8 {
		return strings.ToUpper(id[:8])
	}
	return strings.ToUpper(id)
}

func buildMsg(from, to, subject, html, plainText string) string {
	b := "gm-boundary-20260101"
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString(`Content-Type: multipart/alternative; boundary="` + b + `"` + "\r\n\r\n")
	sb.WriteString("--" + b + "\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	sb.WriteString(plainText + "\r\n")
	sb.WriteString("--" + b + "\r\n")
	sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
	sb.WriteString(html + "\r\n")
	sb.WriteString("--" + b + "--\r\n")
	return sb.String()
}

func fmtNaira(kobo int64) string {
	return fmt.Sprintf("₦%s", formatNumber(kobo/100))
}

func formatNumber(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}
	return result.String()
}

// ── Customer invoice HTML ──────────────────────────────────────────────────────

func customerInvoiceHTML(customerName, storeName, orderID, orderURL string, totalKobo int64, items []InvoiceItem) string {
	sid := shortID(orderID)

	// Build item rows
	var rows strings.Builder
	for _, item := range items {
		lineTotal := fmtNaira(item.PriceKobo * int64(item.Quantity))
		unitPrice := fmtNaira(item.PriceKobo)
		imgCell := ""
		if item.ImageURL != "" {
			imgCell = fmt.Sprintf(`<img src="%s" width="44" height="44" style="border-radius:6px;object-fit:cover;display:block;" alt="">`, item.ImageURL)
		} else {
			imgCell = `<div style="width:44px;height:44px;border-radius:6px;background:#f1f5f9;display:flex;align-items:center;justify-content:center;font-size:18px;">📦</div>`
		}
		rows.WriteString(fmt.Sprintf(`
<tr>
  <td style="padding:12px 0;border-bottom:1px solid #f1f5f9;vertical-align:middle;">
    <table cellpadding="0" cellspacing="0"><tr>
      <td style="padding-right:12px;">%s</td>
      <td>
        <p style="margin:0;font-size:14px;font-weight:600;color:#1C1C1C;line-height:1.3;">%s</p>
        <p style="margin:4px 0 0;font-size:12px;color:#94a3b8;">Qty %d · %s each</p>
      </td>
    </tr></table>
  </td>
  <td style="padding:12px 0;border-bottom:1px solid #f1f5f9;font-size:14px;font-weight:700;color:#1C1C1C;text-align:right;vertical-align:middle;">%s</td>
</tr>`, imgCell, item.Name, item.Quantity, unitPrice, lineTotal))
	}

	// Delivery estimate (simple static for now)
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Order Confirmed</title></head>
<body style="margin:0;padding:0;background:#f0f4f8;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;">

<table width="100%%" cellpadding="0" cellspacing="0" style="background:#f0f4f8;padding:32px 16px 48px;">
<tr><td align="center">
<table width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%%;">

  <!-- Header -->
  <tr><td style="background:#0E1F13;border-radius:16px 16px 0 0;padding:36px 40px;text-align:center;">
    <p style="margin:0 0 6px;font-size:11px;font-weight:700;letter-spacing:0.18em;text-transform:uppercase;color:rgba(255,255,255,0.45);">Order confirmed</p>
    <h1 style="margin:0;font-size:26px;font-weight:800;color:#fff;letter-spacing:-0.5px;">%s</h1>
    <p style="margin:8px 0 0;font-size:14px;color:rgba(255,255,255,0.5);">Thank you for your order, %s!</p>
  </td></tr>

  <!-- Order badge -->
  <tr><td style="background:#1A7A42;padding:14px 40px;text-align:center;">
    <p style="margin:0;font-size:12px;font-weight:700;color:rgba(255,255,255,0.7);">Order ID &nbsp;·&nbsp; <span style="color:#fff;letter-spacing:0.08em;">#%s</span></p>
  </td></tr>

  <!-- Body -->
  <tr><td style="background:#fff;border-radius:0 0 16px 16px;padding:36px 40px;">

    <!-- Greeting -->
    <p style="margin:0 0 24px;font-size:15px;color:#374151;line-height:1.6;">
      Your payment was received and your order is now confirmed. We'll notify the seller and they'll reach out with delivery details shortly.
    </p>

    <!-- Items -->
    <p style="margin:0 0 12px;font-size:11px;font-weight:700;letter-spacing:0.12em;text-transform:uppercase;color:#94a3b8;">Items ordered</p>
    <table width="100%%" cellpadding="0" cellspacing="0">
      <tr>
        <th style="text-align:left;font-size:11px;color:#94a3b8;font-weight:600;padding-bottom:8px;border-bottom:2px solid #f1f5f9;">Product</th>
        <th style="text-align:right;font-size:11px;color:#94a3b8;font-weight:600;padding-bottom:8px;border-bottom:2px solid #f1f5f9;">Total</th>
      </tr>
      %s
      <!-- Total row -->
      <tr>
        <td style="padding:16px 0 0;font-size:13px;font-weight:700;color:#6b7280;">Order total</td>
        <td style="padding:16px 0 0;font-size:22px;font-weight:900;color:#1A7A42;text-align:right;">%s</td>
      </tr>
    </table>

    <!-- Divider -->
    <div style="height:1px;background:#f1f5f9;margin:28px 0;"></div>

    <!-- Track CTA -->
    <div style="text-align:center;margin:28px 0 8px;">
      <a href="%s" style="display:inline-block;background:#1A7A42;color:#fff;text-decoration:none;font-size:15px;font-weight:700;padding:16px 40px;border-radius:12px;letter-spacing:-0.2px;">
        Track your order &rarr;
      </a>
      <p style="margin:14px 0 0;font-size:12px;color:#94a3b8;">Or copy this link: <a href="%s" style="color:#1A7A42;word-break:break-all;">%s</a></p>
    </div>

    <!-- Divider -->
    <div style="height:1px;background:#f1f5f9;margin:28px 0;"></div>

    <!-- Footer note -->
    <p style="margin:0;font-size:12px;color:#94a3b8;text-align:center;line-height:1.7;">
      Questions about your order? Reply to this email or contact %s directly.<br>
      Powered by <a href="https://gomarketi.com" style="color:#1A7A42;text-decoration:none;">GoMarketi</a>
    </p>

  </td></tr>

  <!-- Bottom space -->
  <tr><td style="padding:24px 0;text-align:center;">
    <p style="margin:0;font-size:11px;color:#94a3b8;">&copy; 2026 GoMarketi &middot; Made in Nigeria 🇳🇬</p>
  </td></tr>

</table>
</td></tr>
</table>

</body>
</html>`,
		storeName, customerName,
		sid,
		rows.String(), fmtNaira(totalKobo),
		orderURL, orderURL, orderURL,
		storeName,
	)
}

// ── Vendor alert HTML ──────────────────────────────────────────────────────────

func vendorAlertHTML(storeName, orderID, dashboardURL, customerName, customerEmail, customerPhone, deliveryAddress string, totalKobo int64, items []InvoiceItem) string {
	sid := shortID(orderID)

	var rows strings.Builder
	for _, item := range items {
		rows.WriteString(fmt.Sprintf(`
<tr>
  <td style="padding:10px 0;border-bottom:1px solid #f1f5f9;font-size:14px;color:#1C1C1C;font-weight:600;">%s</td>
  <td style="padding:10px 0;border-bottom:1px solid #f1f5f9;font-size:14px;color:#6b7280;text-align:center;">×%d</td>
  <td style="padding:10px 0;border-bottom:1px solid #f1f5f9;font-size:14px;font-weight:700;color:#1C1C1C;text-align:right;">%s</td>
</tr>`, item.Name, item.Quantity, fmtNaira(item.PriceKobo*int64(item.Quantity))))
	}

	phoneRow := ""
	if customerPhone != "" {
		phoneRow = fmt.Sprintf(`<tr><td style="padding:6px 0;font-size:13px;color:#6b7280;width:100px;">Phone</td><td style="padding:6px 0;font-size:13px;color:#1C1C1C;font-weight:600;">%s</td></tr>`, customerPhone)
	}
	addressRow := ""
	if deliveryAddress != "" {
		addressRow = fmt.Sprintf(`<tr><td style="padding:6px 0;font-size:13px;color:#6b7280;vertical-align:top;width:100px;">Deliver to</td><td style="padding:6px 0;font-size:13px;color:#1C1C1C;font-weight:600;line-height:1.5;">%s</td></tr>`, deliveryAddress)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>New Order</title></head>
<body style="margin:0;padding:0;background:#f0f4f8;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;">

<table width="100%%" cellpadding="0" cellspacing="0" style="background:#f0f4f8;padding:32px 16px 48px;">
<tr><td align="center">
<table width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%%;">

  <!-- Header -->
  <tr><td style="background:#0E1F13;border-radius:16px 16px 0 0;padding:32px 40px;">
    <p style="margin:0 0 4px;font-size:11px;font-weight:700;letter-spacing:0.18em;text-transform:uppercase;color:rgba(255,255,255,0.45);">%s</p>
    <h1 style="margin:0 0 6px;font-size:24px;font-weight:800;color:#fff;letter-spacing:-0.4px;">🛍&nbsp; New order received!</h1>
    <p style="margin:0;font-size:14px;color:rgba(255,255,255,0.5);">Order <strong style="color:#4ade80;">#%s</strong> &nbsp;·&nbsp; %s</p>
  </td></tr>

  <!-- Alert banner -->
  <tr><td style="background:#1A7A42;padding:12px 40px;">
    <p style="margin:0;font-size:13px;font-weight:700;color:rgba(255,255,255,0.8);">
      Action needed: Confirm and arrange delivery for this order.
    </p>
  </td></tr>

  <!-- Body -->
  <tr><td style="background:#fff;border-radius:0 0 16px 16px;padding:36px 40px;">

    <!-- Customer info -->
    <p style="margin:0 0 12px;font-size:11px;font-weight:700;letter-spacing:0.12em;text-transform:uppercase;color:#94a3b8;">Customer</p>
    <div style="background:#f8fafc;border-radius:10px;padding:16px 20px;margin-bottom:28px;">
      <table cellpadding="0" cellspacing="0" width="100%%">
        <tr><td style="padding:6px 0;font-size:13px;color:#6b7280;width:100px;">Name</td><td style="padding:6px 0;font-size:13px;color:#1C1C1C;font-weight:600;">%s</td></tr>
        <tr><td style="padding:6px 0;font-size:13px;color:#6b7280;">Email</td><td style="padding:6px 0;font-size:13px;color:#1C1C1C;font-weight:600;"><a href="mailto:%s" style="color:#1A7A42;">%s</a></td></tr>
        %s
        %s
      </table>
    </div>

    <!-- Items -->
    <p style="margin:0 0 12px;font-size:11px;font-weight:700;letter-spacing:0.12em;text-transform:uppercase;color:#94a3b8;">Items ordered</p>
    <table width="100%%" cellpadding="0" cellspacing="0">
      <tr>
        <th style="text-align:left;font-size:11px;color:#94a3b8;font-weight:600;padding-bottom:8px;border-bottom:2px solid #f1f5f9;">Product</th>
        <th style="text-align:center;font-size:11px;color:#94a3b8;font-weight:600;padding-bottom:8px;border-bottom:2px solid #f1f5f9;">Qty</th>
        <th style="text-align:right;font-size:11px;color:#94a3b8;font-weight:600;padding-bottom:8px;border-bottom:2px solid #f1f5f9;">Total</th>
      </tr>
      %s
      <tr>
        <td colspan="2" style="padding:16px 0 0;font-size:13px;font-weight:700;color:#6b7280;">Order total</td>
        <td style="padding:16px 0 0;font-size:22px;font-weight:900;color:#1A7A42;text-align:right;">%s</td>
      </tr>
    </table>

    <!-- Divider -->
    <div style="height:1px;background:#f1f5f9;margin:28px 0;"></div>

    <!-- Dashboard CTA -->
    <div style="text-align:center;">
      <a href="%s" style="display:inline-block;background:#1A7A42;color:#fff;text-decoration:none;font-size:15px;font-weight:700;padding:16px 40px;border-radius:12px;letter-spacing:-0.2px;">
        View in dashboard &rarr;
      </a>
    </div>

    <!-- Divider -->
    <div style="height:1px;background:#f1f5f9;margin:28px 0;"></div>

    <p style="margin:0;font-size:12px;color:#94a3b8;text-align:center;line-height:1.7;">
      This is an automated notification from GoMarketi.<br>
      You're receiving this because you own <strong>%s</strong>.
    </p>

  </td></tr>

  <tr><td style="padding:24px 0;text-align:center;">
    <p style="margin:0;font-size:11px;color:#94a3b8;">&copy; 2026 GoMarketi &middot; Made in Nigeria 🇳🇬</p>
  </td></tr>

</table>
</td></tr>
</table>

</body>
</html>`,
		storeName, sid, fmtNaira(totalKobo),
		customerName,
		customerEmail, customerEmail,
		phoneRow, addressRow,
		rows.String(), fmtNaira(totalKobo),
		dashboardURL,
		storeName,
	)
}
