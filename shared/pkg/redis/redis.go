// Package redis provides a configured go-redis/v9 client for GoMarket services.
// All services use the same connect pattern: parse the REDIS_URL env var,
// open the client, verify with PING, return the client for injection.
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Config holds the parameters for creating a Redis client.
type Config struct {
	// URL is a Redis connection URL.
	// Examples:
	//   redis://localhost:6379
	//   redis://:password@host:6379/0
	//   rediss://host:6380  (TLS)
	URL string
}

// Connect parses cfg.URL, creates a client, and verifies connectivity with
// a 5-second-timeout PING. Returns a ready-to-use *redis.Client.
func Connect(cfg Config) (*redis.Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("redis URL is required")
	}

	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parsing redis URL: %w", err)
	}

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err = client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("pinging redis: %w", err)
	}

	return client, nil
}
