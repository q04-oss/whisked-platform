package loyalty

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/q04-oss/whisked-platform/internal/platform"
)

const (
	// stampTokenTTL is how long a QR token is valid. Long enough for the customer
	// to open the app, show it at the counter, and have staff scan it.
	// Short enough that a screenshot cannot be replayed hours later.
	stampTokenTTL = 5 * time.Minute

	// stampTokenPrefix is the Redis key prefix for QR stamp tokens.
	stampTokenPrefix = "whisked:stamp-token:"
)

// StampToken is the response returned when a customer requests a QR token.
type StampToken struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"` // seconds
}

// issueStampToken generates a UUID, stores the customer ID in Redis with a
// short TTL, and returns the token. The token is the QR code payload.
func issueStampToken(ctx context.Context, rdb *redis.Client, customerID platform.CustomerID) (*StampToken, error) {
	token := uuid.NewString()
	key := stampTokenPrefix + token

	if err := rdb.Set(ctx, key, customerID.Int64(), stampTokenTTL).Err(); err != nil {
		return nil, fmt.Errorf("issuing stamp token: %w", err)
	}

	return &StampToken{
		Token:     token,
		ExpiresIn: int(stampTokenTTL.Seconds()),
	}, nil
}

// redeemStampToken validates a QR token, returns the customer ID, and
// deletes the token from Redis — single-use.
// Returns platform.ErrNotFound if the token is invalid or expired.
func redeemStampToken(ctx context.Context, rdb *redis.Client, token string) (platform.CustomerID, error) {
	if _, err := uuid.Parse(token); err != nil {
		return 0, platform.ErrNotFound
	}

	key := stampTokenPrefix + token

	// GETDEL — atomic fetch and delete. Prevents any race condition where
	// two staff devices scan the same code simultaneously.
	val, err := rdb.GetDel(ctx, key).Result()
	if err != nil {
		return 0, platform.ErrNotFound
	}

	var rawID int64
	if _, err := fmt.Sscan(val, &rawID); err != nil {
		return 0, platform.ErrNotFound
	}

	return platform.CustomerID(rawID), nil
}
