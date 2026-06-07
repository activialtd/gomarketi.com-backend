// Package email defines the Emailer interface and provides implementations
// used by the auth service to send OTP verification codes.
package email

import "context"

// Emailer is the interface the auth service uses to send emails.
// The concrete implementation is swappable: Mailgun (primary) or SES (fallback).
type Emailer interface {
	// SendOTP sends a 6-digit OTP code to the given email address.
	SendOTP(ctx context.Context, to, otp string) error
}
