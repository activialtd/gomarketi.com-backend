package email

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
)

// ConsoleEmailer is used in local development when no email provider is
// configured. It prints the OTP to stdout instead of sending an email,
// so the developer can copy it from the terminal.
type ConsoleEmailer struct {
	log zerolog.Logger
}

func NewConsoleEmailer(log zerolog.Logger) *ConsoleEmailer {
	return &ConsoleEmailer{log: log}
}

func (c *ConsoleEmailer) SendOTP(_ context.Context, to, otp string) error {
	c.log.Warn().Str("to", to).Str("otp", otp).Msg("DEV MODE — OTP not emailed, check terminal")
	fmt.Printf("\n╔══════════════════════════════════╗\n")
	fmt.Printf("║   DEV OTP for %-18s║\n", to)
	fmt.Printf("║   Code: %-25s║\n", otp)
	fmt.Printf("╚══════════════════════════════════╝\n\n")
	return nil
}
