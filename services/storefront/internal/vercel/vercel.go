// Package vercel registers store subdomains as domains on the storefront
// Vercel project. The *.gomarketi.com wildcard already handles DNS routing
// for any subdomain, but Vercel only issues a working SSL certificate for
// domains that have been explicitly registered on the project — without
// this, a freshly created store's subdomain loads over unencrypted HTTP
// ("Not Secure") until someone adds it by hand.
package vercel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Registrar is the interface CreateStore depends on, so it can be swapped
// for NoopRegistrar when Vercel isn't configured (e.g. local dev) without
// any conditional logic at the call site.
type Registrar interface {
	AddDomain(ctx context.Context, domain string) error
}

// NoopRegistrar discards domain registration — used when no Vercel API
// token is configured.
type NoopRegistrar struct{}

func (NoopRegistrar) AddDomain(context.Context, string) error { return nil }

type Client struct {
	apiToken   string
	projectID  string
	teamID     string
	httpClient *http.Client
}

type Config struct {
	APIToken  string // Vercel API access token (Account/Team Settings → Tokens)
	ProjectID string // target project's ID (Project Settings → General)
	TeamID    string // optional — required if the project belongs to a team
}

func New(cfg Config) (*Client, error) {
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("vercel: API token is required")
	}
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("vercel: project ID is required")
	}
	return &Client{
		apiToken:   cfg.APIToken,
		projectID:  cfg.ProjectID,
		teamID:     cfg.TeamID,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// AddDomain registers domain (e.g. "cobi.gomarketi.com") on the configured
// project. Idempotent — a domain already registered on this project is
// treated as success, not an error.
func (c *Client) AddDomain(ctx context.Context, domain string) error {
	body, err := json.Marshal(map[string]string{"name": domain})
	if err != nil {
		return fmt.Errorf("vercel: encode request: %w", err)
	}

	url := fmt.Sprintf("https://api.vercel.com/v10/projects/%s/domains", c.projectID)
	if c.teamID != "" {
		url += "?teamId=" + c.teamID
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("vercel: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("vercel: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errBody struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		if strings.Contains(strings.ToLower(errBody.Error.Code+errBody.Error.Message), "already") {
			return nil // already registered — not an error
		}
		return fmt.Errorf("vercel: add domain %q: %s (%s)", domain, errBody.Error.Message, errBody.Error.Code)
	}
	return nil
}
