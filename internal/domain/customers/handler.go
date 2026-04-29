package customers

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

// ── GET /v1/customers/me ──────────────────────────────────────────────────────

func (h *handler) getMe(w http.ResponseWriter, r *http.Request) {
	id := mustCustomerID(r)

	customer, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, toProfile(customer))
}

// ── PATCH /v1/customers/me ────────────────────────────────────────────────────

func (h *handler) updateMe(w http.ResponseWriter, r *http.Request) {
	id := mustCustomerID(r)

	var body struct {
		DisplayName *string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.RespondError(w, r, platform.ErrBadRequest)
		return
	}

	customer, err := h.service.Update(r.Context(), id, UpdateParams{
		DisplayName: body.DisplayName,
	})
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, toProfile(customer))
}

// ── DELETE /v1/customers/me ───────────────────────────────────────────────────

func (h *handler) deleteMe(w http.ResponseWriter, r *http.Request) {
	id := mustCustomerID(r)

	if err := h.service.Delete(r.Context(), id); err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// mustCustomerID extracts the authenticated customer ID from the request context.
// Panics if auth middleware has not run — a misconfigured route, not a client error.
func mustCustomerID(r *http.Request) platform.CustomerID {
	id, ok := middleware.AuthenticatedCustomerID(r.Context())
	if !ok {
		panic("customers: handler called without auth middleware")
	}
	return id
}
