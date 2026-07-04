package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BrevoClient sends transactional email via the Brevo (formerly Sendinblue)
// v3 API. Free tier gives 300 emails/day with no daily sending limit on paid plans.
// Docs: https://developers.brevo.com/reference/sendtransacemail
type BrevoClient struct {
	apiKey     string
	from       string
	fromName   string
	httpClient *http.Client
}

type BrevoConfig struct {
	APIKey   string // from Brevo dashboard → SMTP & API → API Keys
	From     string // verified sender email e.g. noreply@gomarketi.com
	FromName string // display name e.g. GoMarketi
}

func NewBrevoClient(cfg BrevoConfig) (*BrevoClient, error) {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("brevo: BREVO_API_KEY is required")
	}
	if cfg.From == "" {
		return nil, fmt.Errorf("brevo: BREVO_FROM is required (a verified sender address)")
	}
	name := cfg.FromName
	if name == "" {
		name = "GoMarketi"
	}
	return &BrevoClient{
		apiKey:     cfg.APIKey,
		from:       cfg.From,
		fromName:   name,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (c *BrevoClient) SendOTP(ctx context.Context, to, otp string) error {
	payload := map[string]any{
		"sender":  map[string]string{"email": c.from, "name": c.fromName},
		"to":      []map[string]string{{"email": to}},
		"subject": "Your GoMarketi verification code",
		"htmlContent": fmt.Sprintf(`
<div style="font-family:sans-serif;max-width:480px;margin:0 auto;padding:32px">
  <h2 style="color:#1a7a42;margin-bottom:8px">Verify your email</h2>
  <p style="color:#555;margin-bottom:24px">Use the code below to complete your sign-in. It expires in 10 minutes.</p>
  <div style="background:#f0faf3;border:1px solid #22c55e33;border-radius:12px;padding:24px;text-align:center">
    <span style="font-size:36px;font-weight:800;letter-spacing:8px;color:#1a7a42">%s</span>
  </div>
  <p style="color:#999;font-size:12px;margin-top:24px">If you didn't request this, you can safely ignore this email.</p>
</div>`, otp),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("brevo: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.brevo.com/v3/smtp/email",
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("brevo: new request: %w", err)
	}
	req.Header.Set("api-key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("brevo: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		var errBody struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		}
		json.Unmarshal(b, &errBody) //nolint:errcheck
		if errBody.Message != "" {
			return fmt.Errorf("brevo: api error %d (%s): %s", resp.StatusCode, errBody.Code, errBody.Message)
		}
		return fmt.Errorf("brevo: api error %d: %s", resp.StatusCode, string(b))
	}

	return nil
}
