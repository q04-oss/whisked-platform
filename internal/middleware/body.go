package middleware

import (
	"bytes"
	"io"
	"net/http"
)

type bodyContextKey string

const bodyKey bodyContextKey = "raw_body"

// readBody reads and replaces r.Body so it can be read again by handlers.
func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return []byte{}, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// GetBody retrieves the raw body stored by the HMAC middleware.
// Falls back to reading r.Body directly if not present.
func GetBody(r *http.Request) ([]byte, error) {
	if b, ok := r.Context().Value(bodyKey).([]byte); ok {
		return b, nil
	}
	return readBody(r)
}
