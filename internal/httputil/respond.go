// Package httputil provides shared HTTP helpers used across all domain handlers.
package httputil

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/q04-oss/whisked-platform/internal/middleware"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

// Respond writes a JSON response with the given status code and body.
// If marshaling fails, it falls back to a 500 with a plain-text message.
func Respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("respond: failed to encode response", "error", err)
	}
}

// RespondError writes an appropriate error response.
//
// If err contains a ClientError in its chain, the ClientError's HTTP status
// and safe message are used. For all other errors, a 500 is returned with a
// generic message — the internal error is logged with full context, never sent
// to the client.
func RespondError(w http.ResponseWriter, r *http.Request, err error) {
	if ce, ok := platform.AsClientError(err); ok {
		Respond(w, ce.HTTPStatus, errorBody{Error: ce.Message})
		return
	}

	// Unexpected error — log with full context, return safe 500.
	slog.ErrorContext(r.Context(), "unexpected error",
		"error", err,
		"request_id", middleware.GetRequestID(r.Context()),
		"method", r.Method,
		"path", r.URL.Path,
	)
	Respond(w, http.StatusInternalServerError, errorBody{Error: "an unexpected error occurred"})
}

type errorBody struct {
	Error string `json:"error"`
}
