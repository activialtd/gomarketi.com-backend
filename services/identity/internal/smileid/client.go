// Package smileid provides a minimal client for Smile ID's ID Verification API.
// It is used to verify Nigerian BVN and NIN against the NIMC / CBN databases.
//
// Security notes:
//   - ID numbers are NEVER logged — the log field is always redacted.
//   - All calls go over TLS (https). Go's default http.Client verifies the
//     server certificate; we additionally enforce a minimum TLS version.
//   - The request signature is HMAC-SHA256(timestamp+partnerID+"sid_request_signature_v1")
//     using the api_key as secret — protects against replay attacks (Smile ID
//     rejects signatures older than 5 minutes).
//   - A 12-second context deadline is injected by the caller so we never hold
//     a goroutine open indefinitely waiting on a slow upstream.
//   - When SMILE_ID_PARTNER_ID is not set, the client runs in simulation mode:
//     any 11-digit number passes after a 1.5-second delay. This lets you
//     develop and test the full flow before obtaining live credentials.
package smileid

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

const (
	prodURL    = "https://api.smileidentity.com/v1"
	sandboxURL = "https://testapi.smileidentity.com/v1"

	signatureVersion = "sid_request_signature_v1"
	requestTimeout   = 12 * time.Second // never block a goroutine longer than this
)

// IDType is the Smile ID id_type for Nigerian documents.
type IDType string

const (
	IDTypeNIN IDType = "NIN" // NIMC National Identification Number
	IDTypeBVN IDType = "BVN" // CBN Bank Verification Number
)

// VerifyRequest is the input to Client.VerifyID.
// IDNumber is treated as sensitive — callers must not log or store it raw.
type VerifyRequest struct {
	Country   string
	IDType    IDType
	IDNumber  string // ← SENSITIVE: never log, never persist unencrypted
	FirstName string
	LastName  string
	DOB       string // YYYY-MM-DD
}

// VerifyResult is returned after a successful Smile ID call.
type VerifyResult struct {
	Matched    bool   // true when the ID was found and name/DOB matched
	ResultCode string // Smile ID numeric result code (e.g. "1012")
	ResultText string
	SmileJobID string // audit reference — safe to store / log
	RecordName string // full name from the official record (safe to store)
	Simulated  bool   // true when running without live credentials
}

// Client wraps the Smile ID HTTP API.
// Construct via New(); use a single instance per service (http.Client reuses
// connections via keep-alive — no per-request allocation overhead).
type Client struct {
	partnerID string
	apiKey    string
	baseURL   string
	http      *http.Client
	log       zerolog.Logger
	simMode   bool
}

// New creates a Client. When partnerID is empty the client enters simulation
// mode, returning synthetic positive results for valid 11-digit numbers.
func New(partnerID, apiKey string, sandbox bool, log zerolog.Logger) *Client {
	base := prodURL
	if sandbox {
		base = sandboxURL
	}
	// Enforce TLS 1.2+ for all upstream calls (Smile ID already requires this,
	// but we make it explicit so a config change can't weaken it).
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        20,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
	}
	return &Client{
		partnerID: partnerID,
		apiKey:    apiKey,
		baseURL:   base,
		log:       log,
		simMode:   partnerID == "",
		http:      &http.Client{Transport: transport, Timeout: requestTimeout},
	}
}

// sign produces the HMAC-SHA256 Smile ID request signature.
// Format: base64(HMAC_SHA256(timestamp + partnerID + version, apiKey))
func (c *Client) sign(timestamp string) string {
	mac := hmac.New(sha256.New, []byte(c.apiKey))
	mac.Write([]byte(timestamp + c.partnerID + signatureVersion))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// ─── Smile ID wire types (internal) ──────────────────────────────────────────

type wireRequest struct {
	PartnerID       string `json:"partner_id"`
	Timestamp       string `json:"timestamp"`
	Signature       string `json:"signature"`
	Country         string `json:"country"`
	IDType          IDType `json:"id_type"`
	IDNumber        string `json:"id_number"` // goes to Smile ID only — never logged
	FirstName       string `json:"first_name,omitempty"`
	LastName        string `json:"last_name,omitempty"`
	DOB             string `json:"dob,omitempty"`
	ReturnJobStatus bool   `json:"return_job_status"`
}

type wireResponse struct {
	ResultCode string `json:"ResultCode"`
	ResultText string `json:"ResultText"`
	SmileJobID string `json:"SmileJobID"`
	Actions    struct {
		VerifyIDNumber     string `json:"Verify_ID_Number"`
		ReturnPersonalInfo string `json:"Return_Personal_Info"`
	} `json:"Actions"`
	FullData struct {
		FullName  string `json:"FullName"`
		FirstName string `json:"FirstName"`
		LastName  string `json:"LastName"`
		DOB       string `json:"DOB"`
	} `json:"FullData"`
	// Error field used when Smile ID returns a non-2xx or error body
	Error string `json:"error,omitempty"`
}

// ─── Public API ───────────────────────────────────────────────────────────────

// VerifyID calls the Smile ID ID Verification endpoint and returns whether the
// provided ID number matches the government record.
//
// The raw IDNumber in req is passed to Smile ID over TLS and then discarded.
// It is NEVER written to any log. Only the SmileJobID and match result are
// returned to the caller for storage.
func (c *Client) VerifyID(ctx context.Context, req VerifyRequest) (VerifyResult, error) {
	if c.simMode {
		return c.simulate(req)
	}
	return c.callSmileID(ctx, req)
}

// simulate returns a synthetic result for development / staging without live
// credentials. It mirrors the real Smile ID behaviour:
//   - 11-digit numeric string → Verified
//   - anything else → no match
func (c *Client) simulate(req VerifyRequest) (VerifyResult, error) {
	c.log.Warn().
		Str("id_type", string(req.IDType)).
		Str("country", req.Country).
		// NEVER log req.IDNumber — even in simulation
		Msg("smile id: SIMULATION MODE — set SMILE_ID_PARTNER_ID for live verification")

	time.Sleep(1500 * time.Millisecond) // mimic realistic latency

	matched := len(req.IDNumber) == 11
	for _, ch := range req.IDNumber {
		if ch < '0' || ch > '9' {
			matched = false
			break
		}
	}

	recordName := req.FirstName + " " + req.LastName
	return VerifyResult{
		Matched:    matched,
		ResultCode: "1012",
		ResultText: "Simulated verification",
		SmileJobID: fmt.Sprintf("SIM-%d", time.Now().UnixMilli()),
		RecordName: recordName,
		Simulated:  true,
	}, nil
}

func (c *Client) callSmileID(ctx context.Context, req VerifyRequest) (VerifyResult, error) {
	ts := time.Now().UTC().Format(time.RFC3339Nano)

	body := wireRequest{
		PartnerID:       c.partnerID,
		Timestamp:       ts,
		Signature:       c.sign(ts),
		Country:         req.Country,
		IDType:          req.IDType,
		IDNumber:        req.IDNumber, // sent to Smile ID over TLS, not stored
		FirstName:       req.FirstName,
		LastName:        req.LastName,
		DOB:             req.DOB,
		ReturnJobStatus: true,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("smileid: marshal: %w", err)
	}

	// Use a deadline-aware context — the caller should always inject one.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/id_verification", bytes.NewReader(payload))
	if err != nil {
		return VerifyResult{}, fmt.Errorf("smileid: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("smileid: http: %w", err)
	}
	defer resp.Body.Close()

	var wire wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return VerifyResult{}, fmt.Errorf("smileid: decode: %w", err)
	}

	if wire.Error != "" {
		// Log the error code but NOT the ID number
		c.log.Error().
			Str("id_type", string(req.IDType)).
			Str("result_code", wire.ResultCode).
			Str("error", wire.Error).
			Msg("smile id verification error")
		return VerifyResult{}, fmt.Errorf("smileid: %s", wire.Error)
	}

	matched := wire.Actions.VerifyIDNumber == "Verified"

	name := wire.FullData.FullName
	if name == "" {
		name = wire.FullData.FirstName + " " + wire.FullData.LastName
	}

	// Log result WITHOUT the ID number — only the audit job ID is safe to log
	c.log.Info().
		Str("id_type", string(req.IDType)).
		Bool("matched", matched).
		Str("result_code", wire.ResultCode).
		Str("smile_job_id", wire.SmileJobID).
		Msg("smile id verification complete")

	return VerifyResult{
		Matched:    matched,
		ResultCode: wire.ResultCode,
		ResultText: wire.ResultText,
		SmileJobID: wire.SmileJobID,
		RecordName: name,
		Simulated:  false,
	}, nil
}
