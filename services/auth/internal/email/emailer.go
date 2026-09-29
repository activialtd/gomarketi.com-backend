// Package email defines the Emailer interface and provides implementations
// used by the auth service to send OTP verification codes.
package email

import "context"

// Emailer is the interface the auth service uses to send emails.
// The concrete implementation is swappable: Mailgun (primary) or SES (fallback).
type Emailer interface {
	// SendOTP sends a 6-digit OTP code to the given email address.
	SendOTP(ctx context.Context, to, otp string) error

	// SendPasswordReset sends a 6-digit password reset code to the given
	// email address. Same delivery mechanism as SendOTP, distinct copy so
	// a user asking to reset their password isn't told to "sign in".
	SendPasswordReset(ctx context.Context, to, otp string) error
}
