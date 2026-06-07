package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTPConfig holds credentials for an SMTP server (e.g. Gmail).
type SMTPConfig struct {
	Host     string // e.g. smtp.gmail.com
	Port     string // e.g. 587
	Username string // full email address
	Password string // app password (not your account password)
	From     string // display name + address, e.g. "GoMarket <noreply@gmail.com>"
}

// SMTPClient sends email over STARTTLS SMTP.
// For Gmail: use an App Password (not your Google account password).
// Enable 2FA on the Google account first, then generate an App Password at
// myaccount.google.com/apppasswords.
type SMTPClient struct {
	cfg SMTPConfig
}

// NewSMTPClient creates an SMTPClient. All fields in cfg are required.
func NewSMTPClient(cfg SMTPConfig) (*SMTPClient, error) {
	if cfg.Host == "" || cfg.Port == "" || cfg.Username == "" || cfg.Password == "" || cfg.From == "" {
		return nil, fmt.Errorf("smtp: Host, Port, Username, Password, and From are all required")
	}
	return &SMTPClient{cfg: cfg}, nil
}

// SendOTP sends a verification code email via STARTTLS SMTP.
// ctx deadline is respected for the dial phase; SMTP itself is synchronous.
func (c *SMTPClient) SendOTP(ctx context.Context, to, otp string) error {
	addr := net.JoinHostPort(c.cfg.Host, c.cfg.Port)

	// Dial with context deadline awareness.
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if deadline, ok := ctx.Deadline(); ok {
		dialer.Deadline = deadline
	}

	tlsCfg := &tls.Config{ServerName: c.cfg.Host, MinVersion: tls.VersionTLS12}

	var client *smtp.Client
	var err error

	if c.cfg.Port == "465" {
		// Port 465: implicit TLS (SMTPS) — wrap the dialer in tls.Dialer.
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: tlsCfg}
		conn, dialErr := tlsDialer.DialContext(ctx, "tcp", addr)
		if dialErr != nil {
			return fmt.Errorf("smtp tls dial %s: %w", addr, dialErr)
		}
		client, err = smtp.NewClient(conn, c.cfg.Host)
		if err != nil {
			return fmt.Errorf("smtp new client: %w", err)
		}
	} else {
		// Port 587/25: plain dial then upgrade via STARTTLS.
		conn, dialErr := dialer.DialContext(ctx, "tcp", addr)
		if dialErr != nil {
			return fmt.Errorf("smtp dial %s: %w", addr, dialErr)
		}
		client, err = smtp.NewClient(conn, c.cfg.Host)
		if err != nil {
			return fmt.Errorf("smtp new client: %w", err)
		}
		if err = client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	defer client.Close()

	// Authenticate with PLAIN auth (Gmail app passwords use this mechanism).
	auth := smtp.PlainAuth("", c.cfg.Username, c.cfg.Password, c.cfg.Host)
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}

	if err = client.Mail(c.cfg.Username); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err = client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	defer wc.Close()

	msg := buildOTPMessage(c.cfg.From, to, otp)
	if _, err = wc.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp write message: %w", err)
	}

	return nil
}

func buildOTPMessage(from, to, otp string) string {
	subject := "Your GoMarket verification code"
	body := otpHTMLBody(otp)

	// RFC 2822 message with MIME multipart for plain+HTML.
	boundary := "gomarket-boundary-001"
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n")
	sb.WriteString("\r\n")

	// Plain text part.
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	sb.WriteString("Your GoMarket verification code is: " + otp + "\r\n")
	sb.WriteString("This code expires in 10 minutes. Do not share it with anyone.\r\n\r\n")

	// HTML part.
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
	sb.WriteString(body + "\r\n")

	sb.WriteString("--" + boundary + "--\r\n")
	return sb.String()
}

func otpHTMLBody(otp string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family:sans-serif;max-width:480px;margin:0 auto;padding:32px 16px;background:#fff;">
  <h2 style="color:#1a1a1a;margin-bottom:8px;">Your GoMarket code</h2>
  <p style="font-size:14px;color:#555;margin-bottom:24px;">
    Use the code below to verify your email address. It expires in 10 minutes.
  </p>
  <div style="background:#f5f5f5;border-radius:8px;padding:32px;text-align:center;">
    <span style="font-size:40px;font-weight:700;letter-spacing:10px;color:#1a1a1a;">%s</span>
  </div>
  <p style="font-size:12px;color:#999;margin-top:24px;">
    If you did not request this code, ignore this email.
  </p>
</body>
</html>`, otp)
}
