package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// Rate limit parameters. Per-user limits are stricter than per-IP because
// authenticated users have verified identity — anomalous request rates are
// more suspicious, and false positives are recoverable (the user can back off).
const (
	ipLimitWindow      = time.Minute
	ipLimitRequests    = 60
	userLimitWindow    = time.Minute
	userLimitRequests  = 120 // higher ceiling — authenticated traffic is trusted further
)

// userIDContextKey is the context key where authenticated user IDs are stored.
// Auth middleware sets this; rate limit middleware reads it.
type userIDContextKey struct{}

// SetAuthenticatedUserID stores the authenticated user's ID in the context.
// Called by the auth middleware after JWT validation.
func SetAuthenticatedUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDContextKey{}, userID)
}

// authenticatedUserID retrieves the user ID from the context.
// Returns 0 and false if the request is unauthenticated.
func authenticatedUserID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDContextKey{}).(int64)
	return id, ok && id != 0
}

// RateLimit enforces per-user limits for authenticated requests and per-IP
// limits for unauthenticated requests. Uses Redis fixed-window counters.
//
// On Redis failure the middleware fails open — rate limiting degrades rather
// than blocking legitimate traffic. This is the correct trade-off for
// availability; DoS mitigation at Railway's infrastructure layer is the
// backstop for Redis unavailability.
func RateLimit(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var allowed bool
			var err error
			var limiterType string

			if uid, ok := authenticatedUserID(r.Context()); ok {
				allowed, err = checkLimit(r.Context(), rdb,
					fmt.Sprintf("whisked:rl:user:%d", uid),
					userLimitWindow,
					userLimitRequests,
				)
				limiterType = "user"
			} else {
				allowed, err = checkLimit(r.Context(), rdb,
					fmt.Sprintf("whisked:rl:ip:%s", clientIP(r)),
					ipLimitWindow,
					ipLimitRequests,
				)
				limiterType = "ip"
			}

			if err != nil {
				// Redis unavailable — fail open.
				_ = limiterType
				next.ServeHTTP(w, r)
				return
			}

			if !allowed {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// checkLimit implements a fixed-window counter in Redis. The window key
// rotates every windowDuration so counters self-expire naturally.
func checkLimit(ctx context.Context, rdb *redis.Client, keyPrefix string, window time.Duration, limit int64) (bool, error) {
	bucket := time.Now().Unix() / int64(window.Seconds())
	key := fmt.Sprintf("%s:%d", keyPrefix, bucket)

	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		// First request in this window — set expiry to 2× window so the key
		// outlives the window and doesn't expire before the window ends.
		rdb.Expire(ctx, key, 2*window)
	}

	return count <= limit, nil
}

// clientIP extracts the client IP from the request, preferring the
// X-Forwarded-For header set by Railway's proxy.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
