// Package domain contains pure business logic for the auth service.
// No database, no HTTP, no external libraries — only stdlib.
package domain

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"
)

const (
	OTPLength      = 6
	OTPExpiry      = 10 * time.Minute
	MaxOTPAttempts = 5
)

// Sentinel errors — the service layer maps these to AppErrors with HTTP codes.
var (
	ErrOTPAlreadyUsed = errors.New("OTP session has already been used")
	ErrOTPExpired     = errors.New("OTP session has expired")
	ErrOTPExhausted   = errors.New("too many failed OTP attempts — request a new code")
	ErrOTPInvalid     = errors.New("invalid OTP")
)

// GenerateOTP generates a cryptographically secure 6-digit numeric OTP.
// Uses crypto/rand. math/rand must NEVER be used for security-sensitive values.
func GenerateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generating OTP: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// OTPSession represents an active OTP verification session as a domain entity.
// It is mapped from the otp_sessions database row by the repository layer.
type OTPSession struct {
	ID           string
	Email        string
	SessionToken string
	OTPHash      string
	Attempts     int
	ExpiresAt    time.Time
	UsedAt       *time.Time
}

// ValidateForVerification checks all state invariants before a verify attempt.
// It is called BEFORE incrementing the attempts counter in the database.
// The caller must increment attempts FIRST in the DB, then call bcrypt.Compare,
// to prevent timing attacks where an attacker could infer attempt count.
func (s *OTPSession) ValidateForVerification() error {
	if s.IsUsed() {
		return ErrOTPAlreadyUsed
	}
	if s.IsExpired() {
		return ErrOTPExpired
	}
	if s.IsExhausted() {
		return ErrOTPExhausted
	}
	return nil
}

// IsExpired reports whether this session has passed its expiry time.
func (s *OTPSession) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// IsUsed reports whether this session has already been successfully verified.
func (s *OTPSession) IsUsed() bool {
	return s.UsedAt != nil
}

// IsExhausted reports whether the maximum number of failed attempts has been reached.
// The attempts count in the domain struct reflects what is already stored in the DB
// (i.e. the value AFTER the pre-increment was applied).
func (s *OTPSession) IsExhausted() bool {
	return s.Attempts >= MaxOTPAttempts
}
