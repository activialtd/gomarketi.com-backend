package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type GmailConfig struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
	From         string // your Gmail address e.g. josephclinton.obi@gmail.com
}

type GmailClient struct {
	cfg    GmailConfig
	client *http.Client
}

func NewGmailClient(cfg GmailConfig) (*GmailClient, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RefreshToken == "" {
		return nil, fmt.Errorf("GMAIL_CLIENT_ID, GMAIL_CLIENT_SECRET and GMAIL_REFRESH_TOKEN are all required")
	}
	if cfg.From == "" {
		return nil, fmt.Errorf("GMAIL_FROM is required (your Gmail address)")
	}
	return &GmailClient{
		cfg:    cfg,
		client: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// getAccessToken exchanges the stored refresh token for a short-lived access token.
func (c *GmailClient) getAccessToken(ctx context.Context) (string, error) {
	body := url.Values{
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"refresh_token": {c.cfg.RefreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://oauth2.googleapis.com/token",
		strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gmail: token request: %w", err)
	}
	defer resp.Body.Close()

	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", fmt.Errorf("gmail: decode token response: %w", err)
	}
	if tok.Error != "" {
		return "", fmt.Errorf("gmail: token error: %s — %s", tok.Error, tok.ErrorDesc)
	}
	return tok.AccessToken, nil
}

func (c *GmailClient) SendOTP(ctx context.Context, to, otp string) error {
	accessToken, err := c.getAccessToken(ctx)
	if err != nil {
		return err
	}

	// Build RFC 2822 message
	raw := strings.Join([]string{
		"From: GoMarketi <" + c.cfg.From + ">",
		"To: " + to,
		"Subject: Your GoMarketi verification code",
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		fmt.Sprintf(`
<div style="font-family:sans-serif;max-width:480px;margin:0 auto;padding:32px">
  <h2 style="color:#1a7a42;margin-bottom:8px">Verify your email</h2>
  <p style="color:#555;margin-bottom:24px">Use the code below to complete your sign-in. It expires in 10 minutes.</p>
  <div style="background:#f0faf3;border:1px solid #22c55e33;border-radius:12px;padding:24px;text-align:center">
    <span style="font-size:36px;font-weight:800;letter-spacing:8px;color:#1a7a42">%s</span>
  </div>
  <p style="color:#999;font-size:12px;margin-top:24px">If you didn't request this, you can safely ignore this email.</p>
</div>`, otp),
	}, "\r\n")

	// Gmail API requires base64url encoding (no padding)
	encoded := base64.URLEncoding.EncodeToString([]byte(raw))

	payload, err := json.Marshal(map[string]string{"raw": encoded})
	if err != nil {
		return fmt.Errorf("gmail: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://gmail.googleapis.com/gmail/v1/users/me/messages/send",
		bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("gmail: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("gmail: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errBody struct {
			Error struct {
				Message string `json:"message"`
				Code    int    `json:"code"`
			} `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return fmt.Errorf("gmail: api error %d: %s", errBody.Error.Code, errBody.Error.Message)
	}

	return nil
}
