package analytics

import (
	"encoding/json"
	"net/http"

	"github.com/q04-oss/whisked-platform/internal/httputil"
	"github.com/q04-oss/whisked-platform/internal/middleware"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

type handler struct {
	service *Service
}

// ── POST /v1/analytics/events ─────────────────────────────────────────────────

func (h *handler) ingest(w http.ResponseWriter, r *http.Request) {
	dnt := r.Header.Get("DNT") == "1"

	var body struct {
		EventType string         `json:"event_type"`
		VisitorID string         `json:"visitor_id"`
		SessionID string         `json:"session_id"`
		Page      string         `json:"page"`
		Metadata  map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.RespondError(w, r, platform.ErrBadRequest)
		return
	}

	params := IngestParams{
		EventType: body.EventType,
		SessionID: body.SessionID,
		Page:      body.Page,
		Metadata:  body.Metadata,
		IPAddress: clientIP(r),
		UserAgent: r.Header.Get("User-Agent"),
	}

	if body.VisitorID != "" {
		vid := platform.AnonymousVisitorID(body.VisitorID)
		params.VisitorID = &vid
	}

	// If the caller is authenticated, attach their CustomerID.
	if cid, ok := middleware.AuthenticatedCustomerID(r.Context()); ok {
		params.CustomerID = &cid
	}

	result, err := h.service.Ingest(r.Context(), params, dnt)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, result)
}

// ── POST /v1/analytics/identify ───────────────────────────────────────────────

func (h *handler) identify(w http.ResponseWriter, r *http.Request) {
	customerID, ok := middleware.AuthenticatedCustomerID(r.Context())
	if !ok {
		httputil.RespondError(w, r, platform.ErrUnauthenticated)
		return
	}

	var body struct {
		VisitorID string `json:"visitor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.RespondError(w, r, platform.ErrBadRequest)
		return
	}
	if body.VisitorID == "" {
		httputil.RespondError(w, r, platform.ErrBadRequest)
		return
	}

	visitorID := platform.AnonymousVisitorID(body.VisitorID)
	if err := h.service.LinkVisitor(r.Context(), visitorID, customerID); err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// clientIP extracts the client IP, preferring Railway's forwarded header.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}
