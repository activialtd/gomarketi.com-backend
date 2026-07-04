// Package main runs all pending SQL migrations from shared/migrations/ against
// the PostgreSQL database specified in DATABASE_URL.
//
// Usage (from repo root):
//
//	go run ./scripts/migrate
//	make migrate
//
// DATABASE_URL must be exported in the shell or present in a .env file at the
// repository root. Only DATABASE_URL is read from .env — other variables are
// intentionally ignored so they cannot silently override the caller's environment.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	migrationsDir   = "./shared/migrations"
	maxConnAttempts = 5
	retryDelay      = 2 * time.Second
)

// createMigrationsTable is idempotent — safe to run on every startup.
const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT        PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);`

func main() {
	loadDotEnv()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set — export it or add it to .env")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("opening connection: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(3)

	if err = pingWithRetry(db); err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	log.Println("connected to database")

	if _, err = db.Exec(createMigrationsTable); err != nil {
		log.Fatalf("creating schema_migrations table: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil {
		log.Fatalf("scanning migrations directory: %v", err)
	}
	if len(files) == 0 {
		log.Printf("no migration files found in %s", migrationsDir)
		return
	}
	sort.Strings(files)

	applied := 0
	for _, f := range files {
		version := strings.TrimSuffix(filepath.Base(f), ".sql")

		var exists bool
		if err = db.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`,
			version,
		).Scan(&exists); err != nil {
			log.Fatalf("checking %s: %v", version, err)
		}
		if exists {
			log.Printf("  skip  %s", version)
			continue
		}

		if err = applyMigration(db, version, f); err != nil {
			log.Fatalf("applying %s: %v", version, err)
		}
		log.Printf("  apply %s ✓", version)
		applied++
	}

	log.Printf("done — %d migration(s) applied", applied)
}

func applyMigration(db *sql.DB, version, file string) error {
	content, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err = tx.Exec(string(content)); err != nil {
		return fmt.Errorf("executing SQL: %w", err)
	}

	if _, err = tx.Exec(
		`INSERT INTO schema_migrations (version) VALUES ($1)`, version,
	); err != nil {
		return fmt.Errorf("recording migration: %w", err)
	}

	return tx.Commit()
}

func pingWithRetry(db *sql.DB) error {
	var err error
	for i := 1; i <= maxConnAttempts; i++ {
		if err = db.Ping(); err == nil {
			return nil
		}
		log.Printf("  ping %d/%d failed: %v", i, maxConnAttempts, err)
		if i < maxConnAttempts {
			time.Sleep(retryDelay)
		}
	}
	return fmt.Errorf("after %d attempts: %w", maxConnAttempts, err)
}

// loadDotEnv reads DATABASE_URL from a .env file at the repo root if the
// variable is not already present in the process environment.
func loadDotEnv() {
	if os.Getenv("DATABASE_URL") != "" {
		return
	}
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && k == "DATABASE_URL" {
			os.Setenv("DATABASE_URL", v)
			return
		}
	}
}
