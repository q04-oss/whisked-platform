package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	hmacTimestampHeader = "X-Timestamp"
	hmacNonceHeader     = "X-Nonce"
	hmacSignatureHeader = "X-Signature"

	// Requests older than this are rejected regardless of signature validity.
	hmacMaxAge = 5 * time.Minute

	// Nonce TTL in Redis — must exceed hmacMaxAge to prevent replay.
	nonceTTL = 10 * time.Minute
)

// RequireHMAC validates that the request was signed by the iOS client.
//
// Signed message: "METHOD\nPATH\nTIMESTAMP\nNONCE\nBODY_HEX"
// Where BODY_HEX is the hex-encoded SHA-256 of the request body.
//
// Headers required:
//   X-Timestamp  — Unix seconds (string)
//   X-Nonce      — UUID v4
//   X-Signature  — HMAC-SHA256 hex
func RequireHMAC(key string, rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tsStr := r.Header.Get(hmacTimestampHeader)
			nonce := r.Header.Get(hmacNonceHeader)
			sig := r.Header.Get(hmacSignatureHeader)

			if tsStr == "" || nonce == "" || sig == "" {
				http.Error(w, "missing signing headers", http.StatusBadRequest)
				return
			}

			// Validate nonce is a UUID — prevents Redis key injection.
			if _, err := uuid.Parse(nonce); err != nil {
				http.Error(w, "invalid nonce format", http.StatusBadRequest)
				return
			}

			// Validate timestamp is recent.
			ts, err := strconv.ParseInt(tsStr, 10, 64)
			if err != nil {
				http.Error(w, "invalid timestamp", http.StatusBadRequest)
				return
			}
			age := time.Since(time.Unix(ts, 0))
			if age > hmacMaxAge || age < -hmacMaxAge {
				http.Error(w, "request expired", http.StatusUnauthorized)
				return
			}

			// Replay protection — SET NX EX rejects any nonce seen before.
			nonceKey := fmt.Sprintf("whisked:nonce:%s", nonce)
			ok, err := rdb.SetNX(r.Context(), nonceKey, 1, nonceTTL).Result()
			if err != nil {
				// Redis unavailable — fail closed for HMAC routes.
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
				return
			}
			if !ok {
				http.Error(w, "replayed request", http.StatusConflict)
				return
			}

			// Verify signature.
			body, err := readBody(r)
			if err != nil {
				http.Error(w, "could not read body", http.StatusBadRequest)
				return
			}

			bodyHash := sha256.Sum256(body)
			message := fmt.Sprintf("%s\n%s\n%s\n%s\n%s",
				r.Method,
				r.URL.Path,
				tsStr,
				nonce,
				hex.EncodeToString(bodyHash[:]),
			)

			mac := hmac.New(sha256.New, []byte(key))
			mac.Write([]byte(message))
			expected := hex.EncodeToString(mac.Sum(nil))

			if !constantTimeEqual(sig, expected) {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}

			// Attach the body back so downstream handlers can read it.
			r = r.WithContext(context.WithValue(r.Context(), bodyKey, body))
			next.ServeHTTP(w, r)
		})
	}
}

// constantTimeEqual compares two strings in constant time.
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var acc byte
	for i := 0; i < len(a); i++ {
		acc |= a[i] ^ b[i]
	}
	return acc == 0
}
