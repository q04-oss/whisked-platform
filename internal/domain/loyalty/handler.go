package loyalty

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/q04-oss/whisked-platform/internal/httputil"
	"github.com/q04-oss/whisked-platform/internal/middleware"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

type handler struct {
	service *Service
}

// ── GET /v1/loyalty/balance ───────────────────────────────────────────────────

func (h *handler) getBalance(w http.ResponseWriter, r *http.Request) {
	id := mustCustomerID(r)

	balance, err := h.service.GetBalance(r.Context(), id)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, balance)
}

// ── GET /v1/loyalty/history ───────────────────────────────────────────────────

func (h *handler) getHistory(w http.ResponseWriter, r *http.Request) {
	id := mustCustomerID(r)

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	events, err := h.service.GetHistory(r.Context(), id, limit, offset)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, events)
}

// ── POST /v1/loyalty/stamp ────────────────────────────────────────────────────

func (h *handler) stamp(w http.ResponseWriter, r *http.Request) {
	id := mustCustomerID(r)

	var body struct {
		LocationID     *int64 `json:"location_id"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.RespondError(w, r, platform.ErrBadRequest)
		return
	}

	params := EarnParams{
		CustomerID:     id,
		Source:         "in_bar",
		IdempotencyKey: body.IdempotencyKey,
	}
	if body.LocationID != nil {
		lid := platform.LocationID(*body.LocationID)
		params.LocationID = &lid
	}

	balance, err := h.service.Earn(r.Context(), params)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, balance)
}

// ── POST /v1/loyalty/redeem ───────────────────────────────────────────────────

func (h *handler) redeem(w http.ResponseWriter, r *http.Request) {
	id := mustCustomerID(r)

	balance, err := h.service.Redeem(r.Context(), id)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, balance)
}

func mustCustomerID(r *http.Request) platform.CustomerID {
	id, ok := middleware.AuthenticatedCustomerID(r.Context())
	if !ok {
		panic("loyalty: handler called without auth middleware")
	}
	return id
}
