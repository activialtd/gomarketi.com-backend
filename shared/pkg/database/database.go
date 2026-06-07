// Package database provides a configured *sqlx.DB connection to PostgreSQL.
// It handles all Neon-specific requirements: mandatory TLS and cold-start retries.
//
// Usage in a service cmd/server/main.go:
//
//	db, err := database.Connect(database.Config{
//	    URL: os.Getenv("DATABASE_URL"),
//	})
package database

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // PostgreSQL driver — blank import registers it.
)

// Config holds the parameters for opening a database connection.
// Zero values fall back to the defaults listed below.
type Config struct {
	// URL is the full PostgreSQL connection string.
	// For Neon, sslmode=require is mandatory: postgresql://user:pass@host/db?sslmode=require
	URL string

	// MaxOpenConns is the maximum number of open connections to the database.
	// Default: 10 (development). Use 25 in production.
	// Neon's connection pooler has a hard limit — stay well under it.
	MaxOpenConns int

	// MaxIdleConns is the maximum number of idle connections in the pool.
	// Default: 5.
	MaxIdleConns int

	// ConnMaxLifetime is the maximum amount of time a connection may be reused.
	// Default: 30 minutes.
	ConnMaxLifetime time.Duration

	// MaxPingAttempts is how many times to retry Ping before giving up.
	// Neon serverless instances spin down on inactivity and need a moment to
	// warm up on first connection. Default: 5.
	MaxPingAttempts int

	// PingRetryDelay is the wait between ping attempts. Default: 2s.
	PingRetryDelay time.Duration
}

const (
	defaultMaxOpenConns    = 10
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 30 * time.Minute
	defaultMaxPingAttempts = 5
	defaultPingRetryDelay  = 2 * time.Second
)

// Connect opens a *sqlx.DB, applies pool limits, and verifies connectivity
// with retried pings before returning. It closes the underlying connection on
// ping failure so the caller never receives a broken DB.
func Connect(cfg Config) (*sqlx.DB, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("database URL is required")
	}
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = defaultMaxOpenConns
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = defaultMaxIdleConns
	}
	if cfg.ConnMaxLifetime == 0 {
		cfg.ConnMaxLifetime = defaultConnMaxLifetime
	}
	if cfg.MaxPingAttempts == 0 {
		cfg.MaxPingAttempts = defaultMaxPingAttempts
	}
	if cfg.PingRetryDelay == 0 {
		cfg.PingRetryDelay = defaultPingRetryDelay
	}

	db, err := sqlx.Open("postgres", cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("opening postgres connection: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if err = pingWithRetry(db, cfg.MaxPingAttempts, cfg.PingRetryDelay); err != nil {
		db.Close()
		return nil, fmt.Errorf("verifying postgres connection: %w", err)
	}

	return db, nil
}

func pingWithRetry(db *sqlx.DB, maxAttempts int, delay time.Duration) error {
	var err error
	for i := 1; i <= maxAttempts; i++ {
		if err = db.Ping(); err == nil {
			return nil
		}
		if i < maxAttempts {
			time.Sleep(delay)
		}
	}
	return fmt.Errorf("after %d ping attempts: %w", maxAttempts, err)
}
