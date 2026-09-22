// Package phone verifies that a phone number a vendor gives is real, using
// Termii's number-lookup API.
//
// This is a lookup, not an OTP round trip: the vendor types their number once
// during store setup and carries on. Nothing is sent to them, and there is no
// code to enter.
package phone

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Verifier checks numbers against Termii. A nil *Verifier is usable and
// simply accepts every well-formed number, which is what local development
// without a Termii key does.
type Verifier struct {
	apiKey  string
	baseURL string
	http    *http.Client
	log     zerolog.Logger
}

// New returns a Verifier. With an empty apiKey it returns nil, so format
// checking still runs but the network lookup is skipped.
func New(apiKey string, log zerolog.Logger) *Verifier {
	if apiKey == "" {
		return nil
	}
	return &Verifier{
		apiKey:  apiKey,
		baseURL: "https://api.ng.termii.com",
		http:    &http.Client{Timeout: 8 * time.Second},
		log:     log,
	}
}

// nigerianLocal matches the formats Nigerians actually type: 08031234567,
// +2348031234567, 2348031234567, 8031234567.
var nigerianLocal = regexp.MustCompile(`^(?:\+?234|0)?([789][01]\d{8})$`)

// Normalise converts a Nigerian number to the E.164 digits Termii expects
// (234XXXXXXXXXX, no plus). It returns an error when the number cannot be a
// Nigerian mobile number — the cheap check that catches most typos before any
// network call.
func Normalise(raw string) (string, error) {
	trimmed := strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '(' || r == ')' {
			return -1
		}
		return r
	}, strings.TrimSpace(raw))

	m := nigerianLocal.FindStringSubmatch(trimmed)
	if m == nil {
		return "", fmt.Errorf("not a valid Nigerian phone number")
	}
	return "234" + m[1], nil
}

type termiiLookupResp struct {
	Number      string `json:"number"`
	Network     string `json:"network"`
	NetworkCode string `json:"network_code"`
	DNDActive   bool   `json:"dnd_active"`
}

// Verify reports whether Termii recognises the number as a real, routable
// line.
//
// It fails open on transport errors, timeouts and unexpected payloads: a
// vendor should not be blocked from creating their store because Termii is
// having a bad day. Only an explicit negative from Termii rejects the number.
func (v *Verifier) Verify(ctx context.Context, e164 string) (bool, error) {
	if v == nil {
		return true, nil
	}

	endpoint := fmt.Sprintf("%s/api/check/dnd?api_key=%s&phone_number=%s",
		v.baseURL, url.QueryEscape(v.apiKey), url.QueryEscape(e164))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		v.log.Warn().Err(err).Msg("termii: building request failed — accepting number")
		return true, nil
	}

	resp, err := v.http.Do(req)
	if err != nil {
		v.log.Warn().Err(err).Msg("termii: lookup unreachable — accepting number")
		return true, nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	// 400/404 is Termii saying it does not know this number.
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode >= 300 {
		v.log.Warn().Int("status", resp.StatusCode).Str("body", string(body)).
			Msg("termii: unexpected status — accepting number")
		return true, nil
	}

	var parsed termiiLookupResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		v.log.Warn().Err(err).Msg("termii: unreadable response — accepting number")
		return true, nil
	}

	// A recognised number comes back carrying its network. Nothing else in
	// the payload is a reliable yes/no, so that is the signal we trust.
	if parsed.Network == "" && parsed.NetworkCode == "" {
		return false, nil
	}
	return true, nil
}
