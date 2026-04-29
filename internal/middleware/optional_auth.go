package middleware

import (
	"net/http"

	"github.com/redis/go-redis/v9"
)

// OptionalAuth attempts JWT validation and, if successful, stores the
// CustomerID in the context. Unlike RequireAuth, a missing or invalid token
// is not an error — the request proceeds without an authenticated identity.
//
// Used for endpoints that serve both anonymous and identified clients,
// such as analytics event ingestion.
func OptionalAuth(jwtSecret string, rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr, err := bearerToken(r)
			if err != nil {
				// No token — proceed anonymously.
				next.ServeHTTP(w, r)
				return
			}

			claims, err := parseJWT(tokenStr, jwtSecret)
			if err != nil {
				// Invalid token — proceed anonymously rather than rejecting.
				next.ServeHTTP(w, r)
				return
			}

			jti, err := jtiFromClaims(claims)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			// Check revocation — a logged-out token should not enrich context.
			revoked, err := rdb.Exists(r.Context(), revokedKey(jti)).Result()
			if err != nil || revoked > 0 {
				next.ServeHTTP(w, r)
				return
			}

			customerID, err := customerIDFromClaims(claims)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			ctx := SetAuthenticatedCustomerID(r.Context(), customerID)
			ctx = SetAuthenticatedUserID(ctx, customerID.Int64())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
