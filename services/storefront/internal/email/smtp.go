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

// SMTPMailer sends welcome emails over SMTP.
// For Gmail use port 465 (implicit TLS) with an App Password.
type SMTPMailer struct {
	host     string
	port     string
	username string
	password string
	from     string
}

// NewSMTP returns an SMTPMailer. All fields are required.
func NewSMTP(host, port, username, password, from string) (*SMTPMailer, error) {
	if host == "" || port == "" || username == "" || password == "" || from == "" {
		return nil, fmt.Errorf("smtp welcome mailer: host, port, username, password and from are all required")
	}
	return &SMTPMailer{host: host, port: port, username: username, password: password, from: from}, nil
}

// SendWelcome delivers a branded welcome email via SMTP.
func (s *SMTPMailer) SendWelcome(ctx context.Context, to, vendorName, storeName, storeSlug, storeDomain string) error {
	if storeDomain == "" {
		storeDomain = "gomarketi.com"
	}
	storeURL := fmt.Sprintf("https://%s.%s", storeSlug, storeDomain)

	subject := fmt.Sprintf("Your GoMarket store %s is live!", storeName)
	html := welcomeHTML(vendorName, storeName, storeURL, storeSlug)
	msg := buildWelcomeMessage(s.from, to, subject, html)

	addr := net.JoinHostPort(s.host, s.port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if deadline, ok := ctx.Deadline(); ok {
		dialer.Deadline = deadline
	}
	tlsCfg := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}

	var client *smtp.Client
	var err error

	if s.port == "465" {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: tlsCfg}
		conn, dialErr := tlsDialer.DialContext(ctx, "tcp", addr)
		if dialErr != nil {
			return fmt.Errorf("smtp tls dial %s: %w", addr, dialErr)
		}
		client, err = smtp.NewClient(conn, s.host)
	} else {
		conn, dialErr := dialer.DialContext(ctx, "tcp", addr)
		if dialErr != nil {
			return fmt.Errorf("smtp dial %s: %w", addr, dialErr)
		}
		client, err = smtp.NewClient(conn, s.host)
		if err == nil {
			err = client.StartTLS(tlsCfg)
		}
	}
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err = client.Mail(s.username); err != nil {
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
	if _, err = wc.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	return nil
}

func buildWelcomeMessage(from, to, subject, html string) string {
	boundary := "gomarket-welcome-001"
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n")
	sb.WriteString("\r\n")
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	sb.WriteString("Welcome to GoMarket! Your store is live. Visit: " + "\r\n\r\n")
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
	sb.WriteString(html + "\r\n")
	sb.WriteString("--" + boundary + "--\r\n")
	return sb.String()
}

// NoopMailer silently discards welcome emails — used when no emailer is configured.
type NoopMailer struct{}

func (NoopMailer) SendWelcome(_ context.Context, _, _, _, _, _ string) error { return nil }
