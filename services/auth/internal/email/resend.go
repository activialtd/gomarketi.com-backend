package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type ResendConfig struct {
	APIKey string
	From   string // e.g. "GoMarketi <noreply@gomarketi.com>"
}

type ResendClient struct {
	cfg    ResendConfig
	client *http.Client
}

func NewResendClient(cfg ResendConfig) (*ResendClient, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("RESEND_API_KEY is required")
	}
	if cfg.From == "" {
		return nil, fmt.Errorf("RESEND_FROM is required")
	}
	return &ResendClient{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (c *ResendClient) SendOTP(ctx context.Context, to, otp string) error {
	payload := map[string]any{
		"from":    c.cfg.From,
		"to":      []string{to},
		"subject": "Your GoMarketi verification code",
		"html": fmt.Sprintf(`
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
		return fmt.Errorf("resend: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("resend: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errBody struct {
			Message string `json:"message"`
		}
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return fmt.Errorf("resend: api error %d: %s", resp.StatusCode, errBody.Message)
	}

	return nil
}
