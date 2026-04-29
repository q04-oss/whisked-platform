package middleware

import (
	"context"
	"net/http"

	"github.com/redis/go-redis/v9"

	"github.com/q04-oss/whisked-platform/internal/platform"
)

type staffIDContextKey struct{}

// SetAuthenticatedStaffID stores the authenticated staff member's ID in context.
func SetAuthenticatedStaffID(ctx context.Context, id platform.StaffID) context.Context {
	return context.WithValue(ctx, staffIDContextKey{}, id)
}

// AuthenticatedStaffID retrieves the staff ID from context.
func AuthenticatedStaffID(ctx context.Context) (platform.StaffID, bool) {
	id, ok := ctx.Value(staffIDContextKey{}).(platform.StaffID)
	return id, ok
}

// RequireStaffAuth validates a staff JWT. It is stricter than RequireAuth in
// two ways:
//   1. The token must carry type == "staff" — customer tokens are explicitly rejected
//   2. Redis failure is fail-closed — dashboard access is unavailable if Redis is down
func RequireStaffAuth(jwtSecret string, rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr, err := bearerToken(r)
			if err != nil {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			claims, err := parseJWT(tokenStr, jwtSecret)
			if err != nil {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			// Reject customer tokens — the type claim must be "staff".
			tokenType, _ := claims["type"].(string)
			if tokenType != "staff" {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			jti, err := jtiFromClaims(claims)
			if err != nil {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			// Check staff revocation set.
			revoked, err := rdb.Exists(r.Context(), staffRevokedKey(jti)).Result()
			if err != nil {
				// Fail closed — dashboard is unavailable if Redis is down.
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
				return
			}
			if revoked > 0 {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			staffID, err := staffIDFromClaims(claims)
			if err != nil {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			ctx := SetAuthenticatedStaffID(r.Context(), staffID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func staffRevokedKey(jti string) string { return "whisked:staff-revoked:" + jti }

// staffIDFromClaims extracts the StaffID from JWT claims.
func staffIDFromClaims(claims map[string]any) (platform.StaffID, error) {
	sub, ok := claims["sub"].(float64)
	if !ok {
		return 0, errInvalidClaims
	}
	return platform.StaffID(int64(sub)), nil
}

var errInvalidClaims = platform.ErrUnauthenticated
