package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// InvoiceItem is a single line item in the invoice email.
type InvoiceItem struct {
	Name      string
	Quantity  int
	PriceKobo int64
}

// SendInvoice sends an order confirmation email to the customer.
// It is designed to be called asynchronously — errors should be logged, not returned to the caller.
func SendInvoice(ctx context.Context, to, customerName, orderID, storeSlug, storeName string, totalKobo int64, items []InvoiceItem) error {
	host := getenv("SMTP_HOST", "")
	port := getenv("SMTP_PORT", "465")
	user := getenv("SMTP_USERNAME", "")
	pass := getenv("SMTP_PASSWORD", "")
	from := getenv("SMTP_FROM", user)

	if host == "" || user == "" || pass == "" {
		return fmt.Errorf("SMTP not configured")
	}

	orderURL := fmt.Sprintf("https://%s.gomarketi.com/orders/%s", storeSlug, orderID)
	subject := fmt.Sprintf("Order confirmed — %s", storeName)
	html := invoiceHTML(customerName, storeName, orderID, orderURL, totalKobo, items)
	msg := buildMsg(from, to, subject, html)

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
	if err = c.Mail(user); err != nil {
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

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func buildMsg(from, to, subject, html string) string {
	b := "invoice-boundary-001"
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString(`Content-Type: multipart/alternative; boundary="` + b + `"` + "\r\n\r\n")
	sb.WriteString("--" + b + "\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	sb.WriteString("Your order has been placed.\r\n\r\n")
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

func invoiceHTML(customerName, storeName, orderID, orderURL string, totalKobo int64, items []InvoiceItem) string {
	var rows strings.Builder
	for _, item := range items {
		rows.WriteString(fmt.Sprintf(`
<tr>
  <td style="padding:10px 0;border-bottom:1px solid #f1f5f9;font-size:14px;color:#1C1C1C;">%s</td>
  <td style="padding:10px 0;border-bottom:1px solid #f1f5f9;font-size:14px;color:#6b7280;text-align:center;">%d</td>
  <td style="padding:10px 0;border-bottom:1px solid #f1f5f9;font-size:14px;color:#1C1C1C;text-align:right;">%s</td>
</tr>`, item.Name, item.Quantity, fmtNaira(item.PriceKobo*int64(item.Quantity))))
	}

	shortID := orderID
	if len(orderID) >= 8 {
		shortID = orderID[:8]
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<body style="margin:0;padding:0;background:#f4f7f6;font-family:'Segoe UI',Arial,sans-serif;">
<table width="100%%" cellpadding="0" cellspacing="0" style="background:#f4f7f6;padding:40px 16px;">
<tr><td align="center">
<table width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%%;">

<tr><td style="background:#0A2E1A;border-radius:20px 20px 0 0;padding:32px 40px;text-align:center;">
  <div style="font-size:24px;font-weight:800;color:#fff;letter-spacing:-0.5px;">%s</div>
  <p style="color:rgba(255,255,255,0.6);font-size:14px;margin:8px 0 0;">Order Confirmation</p>
</td></tr>

<tr><td style="background:#fff;border-radius:0 0 20px 20px;padding:32px 40px;">
  <h2 style="color:#1C1C1C;font-size:18px;margin:0 0 8px;">Hi %s!</h2>
  <p style="color:#6b7280;font-size:14px;line-height:1.6;margin:0 0 24px;">
    Thank you for your order. Here's a summary of what you ordered.
  </p>

  <div style="background:#F0FAF3;border-radius:10px;padding:12px 16px;margin-bottom:24px;">
    <p style="margin:0;font-size:12px;color:#3D6B4F;">Order ID: <strong>#%s</strong></p>
  </div>

  <table width="100%%" cellpadding="0" cellspacing="0">
    <tr>
      <th style="text-align:left;font-size:11px;color:#94a3b8;font-weight:600;text-transform:uppercase;padding-bottom:8px;">Item</th>
      <th style="text-align:center;font-size:11px;color:#94a3b8;font-weight:600;text-transform:uppercase;padding-bottom:8px;">Qty</th>
      <th style="text-align:right;font-size:11px;color:#94a3b8;font-weight:600;text-transform:uppercase;padding-bottom:8px;">Price</th>
    </tr>
    %s
    <tr>
      <td colspan="2" style="padding:16px 0 0;font-size:15px;font-weight:700;color:#1C1C1C;">Total</td>
      <td style="padding:16px 0 0;font-size:18px;font-weight:800;color:#1A7A42;text-align:right;">%s</td>
    </tr>
  </table>

  <div style="text-align:center;margin-top:32px;">
    <a href="%s" style="display:inline-block;background:#1A7A42;color:#fff;text-decoration:none;font-size:14px;font-weight:700;padding:14px 32px;border-radius:10px;">
      View Order Status &rarr;
    </a>
  </div>

  <p style="margin-top:24px;font-size:12px;color:#94a3b8;text-align:center;">
    Questions? Reply to this email or visit <a href="https://gomarketi.com" style="color:#1A7A42;">gomarketi.com</a>
  </p>
</td></tr>

<tr><td style="padding:20px 0;text-align:center;">
  <p style="margin:0;color:#b0bec5;font-size:12px;">&copy; 2026 GoMarketi &middot; Powered by GoMarket</p>
</td></tr>

</table>
</td></tr>
</table>
</body>
</html>`, storeName, customerName, shortID, rows.String(), fmtNaira(totalKobo), orderURL)
}
