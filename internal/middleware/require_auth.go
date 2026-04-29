package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"

	"github.com/q04-oss/whisked-platform/internal/platform"
)

// RequireAuth validates the Bearer JWT in the Authorization header and stores
// the authenticated CustomerID in the request context.
//
// Returns 401 if:
//   - The Authorization header is missing or malformed
//   - The token signature is invalid
//   - The token is expired
//   - The token's jti has been revoked (found in Redis revocation set)
func RequireAuth(jwtSecret string, rdb *redis.Client) func(http.Handler) http.Handler {
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

			jti, err := jtiFromClaims(claims)
			if err != nil {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			customerID, err := customerIDFromClaims(claims)
			if err != nil {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			// Check revocation set — a logged-out token's jti is stored here.
			revoked, err := rdb.Exists(r.Context(), revokedKey(jti)).Result()
			if err != nil {
				// Redis unavailable — fail closed for auth.
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
				return
			}
			if revoked > 0 {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			ctx := SetAuthenticatedCustomerID(r.Context(), customerID)
			// Also set the raw int64 for the rate limiter.
			ctx = SetAuthenticatedUserID(ctx, customerID.Int64())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// bearerToken extracts the token string from "Authorization: Bearer <token>".
func bearerToken(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", fmt.Errorf("missing Authorization header")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", fmt.Errorf("malformed Authorization header")
	}
	return parts[1], nil
}

func parseJWT(tokenStr, secret string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid claims")
	}
	return claims, nil
}

func revokedKey(jti string) string {
	return "whisked:revoked:" + jti
}

func jtiFromClaims(claims jwt.MapClaims) (string, error) {
	jti, ok := claims["jti"].(string)
	if !ok || jti == "" {
		return "", fmt.Errorf("missing jti")
	}
	return jti, nil
}

func customerIDFromClaims(claims jwt.MapClaims) (platform.CustomerID, error) {
	sub, ok := claims["sub"].(float64)
	if !ok {
		return 0, fmt.Errorf("invalid sub claim")
	}
	return platform.CustomerID(int64(sub)), nil
}
