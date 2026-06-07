// Package logger provides a zerolog-based structured JSON logger.
// All GoMarket services log to stdout in production (JSON) and to stderr in
// development (human-readable console format with coloured levels).
package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// New creates a zerolog.Logger configured for the given environment string.
//
//   - "development"  → human-readable console output to stderr
//   - anything else  → structured JSON to stdout (production default)
//
// Services should call this once at startup and propagate the logger via
// dependency injection, not through a global variable.
func New(env string) zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano

	var out io.Writer
	if env == "development" {
		out = zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: "15:04:05",
		}
	} else {
		out = os.Stdout
	}

	return zerolog.New(out).With().Timestamp().Logger()
}

// CtxKey is the gin context key under which a request-scoped logger is stored
// by the RequestLogger middleware. Service handlers retrieve it with:
//
//	log := c.MustGet(logger.CtxKey).(zerolog.Logger)
const CtxKey = "logger"
