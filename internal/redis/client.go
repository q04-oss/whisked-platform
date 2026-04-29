package redisclient

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Connect creates a Redis client and verifies the connection.
func Connect(ctx context.Context, url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parsing Redis URL: %w", err)
	}

	client := redis.NewClient(opts)

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("pinging Redis: %w", err)
	}

	return client, nil
}
