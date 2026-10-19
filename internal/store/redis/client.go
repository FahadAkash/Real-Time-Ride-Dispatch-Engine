package redis

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	Addrs    []string
	Username string
	Password string
	UseTLS   bool
}

type Client struct {
	rdb redis.UniversalClient
}

// NewClient initializes a pooled Redis client supporting standalone or cluster topologies.
func NewClient(cfg Config) (*Client, error) {
	var tlsConfig *tls.Config
	if cfg.UseTLS {
		tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	opts := &redis.UniversalOptions{
		Addrs:        cfg.Addrs,
		Username:     cfg.Username,
		Password:     cfg.Password,
		TLSConfig:    tlsConfig,
		PoolSize:     100,
		MinIdleConns: 20,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
	}

	rdb := redis.NewUniversalClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &Client{rdb: rdb}, nil
}

// Close closes the Redis connection pool.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Raw returns the underlying UniversalClient for advanced usage if needed.
func (c *Client) Raw() redis.UniversalClient {
	return c.rdb
}
