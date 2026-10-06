package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	htmlpkg "html"
	"io"
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
func SendInvoice(ctx context.Context, to, customerName, orderID, storeSlug, storeName string, totalKobo, deliveryFeeKobo int64, deliveryTitle string, items []InvoiceItem) error {
	orderURL := buildOrderURL(storeSlug, orderID, to)
	subject := fmt.Sprintf("Order confirmed — %s (#%s)", storeName, shortID(orderID))
	html := customerInvoiceHTML(customerName, storeName, orderID, orderURL, totalKobo, deliveryFeeKobo, deliveryTitle, items)
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
	body := heading("You left something behind") +
		paragraph(fmt.Sprintf("Hi %s, your basket at %s is still saved. Nothing has been charged.",
			htmlpkg.EscapeString(customerName), htmlpkg.EscapeString(storeName))) +
		labelRow("In your basket") +
		itemTable(items) +
		fmt.Sprintf(`
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="margin-top:16px;">
<tr>
  <td style="font-size:15px;font-weight:700;color:%s;">Basket total</td>
  <td style="font-size:19px;font-weight:700;color:%s;text-align:right;letter-spacing:-0.3px;">%s</td>
</tr>
</table>`, inkColor, inkColor, fmtNaira(totalKobo)) +
		divider() +
		button("Finish your order", trackURL)

	return shell("Your basket", fmt.Sprintf("Your basket at %s — %s", storeName, fmtNaira(totalKobo)), body)
}

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
    <p style="margin:0;font-size:11px;color:#94a3b8;">&copy; 2026 GoMarket</p>
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

// humanStatus reads inside a sentence — a subject line, the plain-text part.
func humanStatus(s string) string {
	switch s {
	case "confirmed":
		return "confirmed"
	case "at_hub":
		return "received at our hub"
	case "shipped":
		return "sent out for delivery"
	case "ready_for_collection":
		return "ready for you to collect"
	case "delivered":
		return "delivered"
	case "cancelled":
		return "cancelled"
	default:
		return s
	}
}

// statusBadgeLabel reads as a badge beside the status dot, so it is title case
// and short enough not to wrap next to the order number.
func statusBadgeLabel(s string) string {
	switch s {
	case "confirmed":
		return "Confirmed"
	case "at_hub":
		return "At our hub"
	case "shipped":
		return "On its way"
	case "ready_for_collection":
		return "Ready to collect"
	case "delivered":
		return "Delivered"
	case "cancelled":
		return "Cancelled"
	default:
		return s
	}
}

// statusAccent is the one spot of colour in a status email: a dot beside the
// label. The old template coloured a full-width banner and led with a 32px
// emoji, which made "your order moved a step" read like an announcement.
func statusAccent(s string) string {
	switch s {
	case "confirmed", "delivered":
		return brandGreen
	case "at_hub":
		return "#7c5cd6"
	case "shipped", "ready_for_collection":
		return "#2f6fb5"
	case "cancelled":
		return "#c0392b"
	default:
		return mutedColor
	}
}

func statusUpdateHTML(customerName, storeName, orderID, orderURL, status string) string {
	sid := shortID(orderID)
	accent := statusAccent(status)
	label := statusBadgeLabel(status)

	// Statements of fact. Someone opening this wants to know where their order
	// is and whether they have to do anything — not to be told it is great
	// news. Each one says what happens next, because that is the question.
	var headline, message string
	switch status {
	case "confirmed":
		headline = "Your order is confirmed"
		message = fmt.Sprintf("%s has your order and is preparing it. We will email you again when it is on its way.", htmlpkg.EscapeString(storeName))
	case "at_hub":
		headline = "Your order is at our hub"
		message = "It is being packed with anything else you ordered, then it goes out for delivery."
	case "shipped":
		headline = "Your order is on its way"
		message = "Once it reaches you, confirm receipt on the tracking page — that is what releases payment to the seller."
	case "ready_for_collection":
		headline = "Your order is ready to collect"
		message = fmt.Sprintf("%s has it packed and waiting for you. Once you have picked it up, confirm receipt on the tracking page — that is what releases payment to them.", htmlpkg.EscapeString(storeName))
	case "delivered":
		headline = "Your order has been delivered"
		message = fmt.Sprintf("Thanks for shopping with %s. If anything is wrong with it, reply to this email and we will sort it out.", htmlpkg.EscapeString(storeName))
	case "cancelled":
		headline = "Your order was cancelled"
		message = "Any payment you made is being refunded. That usually lands within a few working days, depending on your bank."
	default:
		headline = "Your order was updated"
		message = fmt.Sprintf("The status is now <strong>%s</strong>.", htmlpkg.EscapeString(label))
	}

	body := heading(headline) +
		paragraph(fmt.Sprintf("Hi %s,", htmlpkg.EscapeString(customerName))) +
		paragraph(message) +
		fmt.Sprintf(`<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:22px 0 26px;">
<tr>
  <td style="padding-right:9px;"><span style="display:inline-block;width:8px;height:8px;border-radius:50%%;background:%s;"></span></td>
  <td style="font-size:13px;font-weight:700;color:%s;">%s</td>
  <td style="padding-left:14px;font-size:13px;color:%s;">Order #%s</td>
</tr>
</table>`, accent, inkColor, htmlpkg.EscapeString(label), mutedColor, sid) +
		button("View your order", orderURL)

	return shell("Order update", fmt.Sprintf("%s — order #%s", headline, sid), body)
}

func SendVendorAlert(ctx context.Context, vendorEmail, storeName, orderID, customerName, customerEmail, customerPhone, deliveryAddress string, totalKobo, deliveryFeeKobo int64, deliveryTitle string, items []InvoiceItem) error {
	dashboardBase := getenv("VENDOR_BASE_URL", "http://localhost:3000")
	orderURL := fmt.Sprintf("%s/merchant/orders", dashboardBase)
	subject := fmt.Sprintf("New order #%s — %s from %s", shortID(orderID), fmtNaira(totalKobo), customerName)
	html := vendorAlertHTML(storeName, orderID, orderURL, customerName, customerEmail, customerPhone, deliveryAddress, totalKobo, deliveryFeeKobo, deliveryTitle, items)
	plain := fmt.Sprintf("New order #%s placed.\nCustomer: %s (%s)\nTotal: %s\nView orders: %s", shortID(orderID), customerName, customerEmail, fmtNaira(totalKobo), orderURL)
	return sendMail(ctx, vendorEmail, subject, html, plain)
}

// ── Mail transport ─────────────────────────────────────────────────────────────

// sendMail dispatches an email using whichever provider is configured.
// Priority: Gmail API (same creds as auth service) → SMTP.
func sendMail(ctx context.Context, to, subject, html, plainText string) error {
	// Resend first: an HTTPS API that works on any host, unlike SMTP which
	// Railway blocks outbound.
	if key := getenv("RESEND_API_KEY", ""); key != "" {
		return sendMailResend(ctx, key, to, subject, html, plainText)
	}
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

// itemRows renders the line items shared by the invoice, the vendor alert and
// the cart summary. A product with no picture gets a quiet initial rather than
// a parcel emoji, which at 18px looked like a broken image.
func itemRows(items []InvoiceItem) string {
	var rows strings.Builder
	for _, item := range items {
		cell := fmt.Sprintf(
			`<div style="width:42px;height:42px;border-radius:7px;background:#f1f4f3;text-align:center;line-height:42px;font-size:15px;font-weight:700;color:#9aa5a0;">%s</div>`,
			htmlpkg.EscapeString(initial(item.Name)))
		if item.ImageURL != "" {
			cell = fmt.Sprintf(
				`<img src="%s" width="42" height="42" style="border-radius:7px;object-fit:cover;display:block;" alt="">`,
				item.ImageURL)
		}
		rows.WriteString(fmt.Sprintf(`
<tr>
  <td style="padding:13px 0;border-bottom:1px solid %s;vertical-align:top;">
    <table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr>
      <td style="padding-right:12px;">%s</td>
      <td style="vertical-align:top;">
        <p style="margin:0;font-size:14px;font-weight:600;color:%s;line-height:1.35;">%s</p>
        <p style="margin:3px 0 0;font-size:12.5px;color:%s;">%d &times; %s</p>
      </td>
    </tr></table>
  </td>
  <td style="padding:13px 0;border-bottom:1px solid %s;font-size:14px;font-weight:600;color:%s;text-align:right;vertical-align:top;white-space:nowrap;">%s</td>
</tr>`,
			hairlineGrey, cell, inkColor, htmlpkg.EscapeString(item.Name),
			mutedColor, item.Quantity, fmtNaira(item.PriceKobo),
			hairlineGrey, inkColor, fmtNaira(item.PriceKobo*int64(item.Quantity))))
	}
	return rows.String()
}

func initial(name string) string {
	for _, r := range strings.TrimSpace(name) {
		return strings.ToUpper(string(r))
	}
	return "?"
}

// totalsBlock closes an itemised table: subtotal, delivery, then the total on
// its own weight. Delivery is always shown, including when it is free, because
// "where did the extra 4,500 come from" is the question these answer.
func totalsBlock(totalKobo, deliveryFeeKobo int64, deliveryTitle string) string {
	deliveryLabel := "Delivery"
	if deliveryTitle != "" {
		deliveryLabel = "Delivery &middot; " + htmlpkg.EscapeString(deliveryTitle)
	}
	deliveryValue := "Free"
	if deliveryFeeKobo > 0 {
		deliveryValue = fmtNaira(deliveryFeeKobo)
	}
	return fmt.Sprintf(`
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="margin-top:16px;">
<tr>
  <td style="font-size:13.5px;color:%s;padding:3px 0;">Items</td>
  <td style="font-size:13.5px;color:%s;text-align:right;padding:3px 0;">%s</td>
</tr>
<tr>
  <td style="font-size:13.5px;color:%s;padding:3px 0;">%s</td>
  <td style="font-size:13.5px;color:%s;text-align:right;padding:3px 0;">%s</td>
</tr>
<tr><td colspan="2" style="padding:12px 0 0;"><div style="height:1px;background:%s;"></div></td></tr>
<tr>
  <td style="font-size:15px;font-weight:700;color:%s;padding:12px 0 0;">Total</td>
  <td style="font-size:19px;font-weight:700;color:%s;text-align:right;padding:12px 0 0;letter-spacing:-0.3px;">%s</td>
</tr>
</table>`,
		mutedColor, inkColor, fmtNaira(totalKobo-deliveryFeeKobo),
		mutedColor, deliveryLabel, inkColor, deliveryValue,
		hairlineGrey, inkColor, inkColor, fmtNaira(totalKobo))
}

func itemTable(items []InvoiceItem) string {
	return fmt.Sprintf(
		`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0">%s</table>`,
		itemRows(items))
}

func customerInvoiceHTML(customerName, storeName, orderID, orderURL string, totalKobo, deliveryFeeKobo int64, deliveryTitle string, items []InvoiceItem) string {
	sid := shortID(orderID)

	body := heading("Thanks for your order") +
		paragraph(fmt.Sprintf("Hi %s, %s has your order and will start preparing it. Here is what you bought.",
			htmlpkg.EscapeString(customerName), htmlpkg.EscapeString(storeName))) +
		labelRow("Order #"+sid) +
		itemTable(items) +
		totalsBlock(totalKobo, deliveryFeeKobo, deliveryTitle) +
		divider() +
		paragraph("You can follow this order at any time, and confirm receipt once it reaches you.") +
		button("Track this order", orderURL)

	return shell("Your order", fmt.Sprintf("Order #%s from %s — %s", sid, storeName, fmtNaira(totalKobo)), body)
}

func deliveryRowHTML(deliveryFeeKobo int64, title string, colspan int) string {
	if deliveryFeeKobo <= 0 {
		return ""
	}
	label := "Delivery"
	if title != "" {
		label = "Delivery — " + title
	}
	span := ""
	if colspan > 1 {
		span = fmt.Sprintf(` colspan="%d"`, colspan)
	}
	return fmt.Sprintf(`
      <tr>
        <td%s style="padding:12px 0 0;font-size:13px;color:#6b7280;">%s</td>
        <td style="padding:12px 0 0;font-size:13px;color:#1C1C1C;text-align:right;">%s</td>
      </tr>`, span, label, fmtNaira(deliveryFeeKobo))
}

// ── Vendor alert HTML ──────────────────────────────────────────────────────────

func vendorAlertHTML(storeName, orderID, dashboardURL, customerName, customerEmail, customerPhone, deliveryAddress string, totalKobo, deliveryFeeKobo int64, deliveryTitle string, items []InvoiceItem) string {
	sid := shortID(orderID)

	detail := func(label, value string) string {
		if strings.TrimSpace(value) == "" {
			return ""
		}
		return fmt.Sprintf(`
<tr>
  <td style="padding:5px 0;font-size:13px;color:%s;width:112px;vertical-align:top;">%s</td>
  <td style="padding:5px 0;font-size:13.5px;color:%s;vertical-align:top;">%s</td>
</tr>`, mutedColor, label, inkColor, htmlpkg.EscapeString(value))
	}

	body := heading("You have a new order") +
		paragraph(fmt.Sprintf("Order #%s came in for %s. Confirm it in your dashboard so the customer knows you have it.",
			sid, htmlpkg.EscapeString(storeName))) +
		labelRow("Customer") +
		fmt.Sprintf(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0">%s%s%s%s</table>`,
			detail("Name", customerName),
			detail("Email", customerEmail),
			detail("Phone", customerPhone),
			detail("Deliver to", deliveryAddress)) +
		labelRow("Items") +
		itemTable(items) +
		totalsBlock(totalKobo, deliveryFeeKobo, deliveryTitle) +
		divider() +
		button("Open your dashboard", dashboardURL)

	return shell("New order", fmt.Sprintf("Order #%s — %s", sid, fmtNaira(totalKobo)), body)
}

func sendMailResend(ctx context.Context, apiKey, to, subject, html, plainText string) error {
	// EMAIL_FROM is accepted as an alias for RESEND_FROM.
	from := getenv("RESEND_FROM", getenv("EMAIL_FROM", "GoMarketi <onboarding@resend.dev>"))

	payload := map[string]any{
		"from":    from,
		"to":      []string{to},
		"subject": subject,
		"html":    html,
	}
	if plainText != "" {
		payload["text"] = plainText
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
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
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
