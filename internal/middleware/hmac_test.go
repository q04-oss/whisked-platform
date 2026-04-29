package middleware

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// testRedis starts an in-process Redis server for testing.
// It is closed automatically when the test ends.
func testRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

// signedRequest builds a valid HMAC-signed request for testing.
func signedRequest(t *testing.T, key, method, path string, body []byte) *http.Request {
	t.Helper()

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := uuid.NewString()

	bodyHash := sha256.Sum256(body)
	message := fmt.Sprintf("%s\n%s\n%s\n%s\n%s",
		method, path, ts, nonce,
		hex.EncodeToString(bodyHash[:]),
	)

	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(message))
	sig := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set(hmacTimestampHeader, ts)
	req.Header.Set(hmacNonceHeader, nonce)
	req.Header.Set(hmacSignatureHeader, sig)
	return req
}

func TestRequireHMAC_ValidRequest(t *testing.T) {
	rdb := testRedis(t)
	key := "test-hmac-key"

	handler := RequireHMAC(key, rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := signedRequest(t, key, http.MethodPost, "/v1/loyalty/stamp", []byte(`{"location_id":1}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRequireHMAC_TamperedSignature(t *testing.T) {
	rdb := testRedis(t)
	key := "test-hmac-key"

	handler := RequireHMAC(key, rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := signedRequest(t, key, http.MethodPost, "/v1/loyalty/stamp", []byte(`{"location_id":1}`))
	req.Header.Set(hmacSignatureHeader, "0000000000000000000000000000000000000000000000000000000000000000")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for tampered signature, got %d", w.Code)
	}
}

func TestRequireHMAC_WrongKey(t *testing.T) {
	rdb := testRedis(t)

	handler := RequireHMAC("correct-key", rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := signedRequest(t, "wrong-key", http.MethodPost, "/v1/loyalty/stamp", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong key, got %d", w.Code)
	}
}

func TestRequireHMAC_ReplayRejected(t *testing.T) {
	rdb := testRedis(t)
	key := "test-hmac-key"

	handler := RequireHMAC(key, rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := signedRequest(t, key, http.MethodPost, "/v1/loyalty/stamp", nil)

	// First request succeeds.
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w1.Code)
	}

	// Identical request (same nonce) must be rejected.
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req.Clone(context.Background()))
	if w2.Code != http.StatusConflict {
		t.Errorf("replayed request: expected 409, got %d", w2.Code)
	}
}

func TestRequireHMAC_ExpiredTimestamp(t *testing.T) {
	rdb := testRedis(t)
	key := "test-hmac-key"

	handler := RequireHMAC(key, rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Build request with a timestamp 10 minutes in the past.
	oldTS := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	nonce := uuid.NewString()

	bodyHash := sha256.Sum256(nil)
	message := fmt.Sprintf("POST\n/v1/loyalty/stamp\n%s\n%s\n%s",
		oldTS, nonce, hex.EncodeToString(bodyHash[:]),
	)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(message))
	sig := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/v1/loyalty/stamp", nil)
	req.Header.Set(hmacTimestampHeader, oldTS)
	req.Header.Set(hmacNonceHeader, nonce)
	req.Header.Set(hmacSignatureHeader, sig)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for expired timestamp, got %d", w.Code)
	}
}

func TestRequireHMAC_MissingHeaders(t *testing.T) {
	rdb := testRedis(t)
	key := "test-hmac-key"

	handler := RequireHMAC(key, rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/loyalty/stamp", nil)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing headers, got %d", w.Code)
	}
}

func TestRequireHMAC_InvalidNonceFormat(t *testing.T) {
	rdb := testRedis(t)
	key := "test-hmac-key"

	handler := RequireHMAC(key, rdb)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/loyalty/stamp", nil)
	req.Header.Set(hmacTimestampHeader, strconv.FormatInt(time.Now().Unix(), 10))
	req.Header.Set(hmacNonceHeader, "not-a-uuid; DROP TABLE audit_log; --")
	req.Header.Set(hmacSignatureHeader, "irrelevant")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid nonce format, got %d", w.Code)
	}
}
