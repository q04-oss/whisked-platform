package loyalty

import (
	"encoding/json"
	"fmt"
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

// ── GET /v1/loyalty/qr-token ─────────────────────────────────────────────────

func (h *handler) qrToken(w http.ResponseWriter, r *http.Request) {
	id := mustCustomerID(r)

	token, err := h.service.IssueStampToken(r.Context(), id)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, token)
}

// ── GET /stamp?t=<token> ──────────────────────────────────────────────────────
// Public endpoint opened by staff scanning a customer QR code.
// Returns HTML — designed to be read on a phone browser in seconds.

func (h *handler) stampViaQR(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("t")
	if token == "" {
		stampPage(w, false, "", 0)
		return
	}

	customer, err := h.service.StampViaToken(r.Context(), token)
	if err != nil {
		stampPage(w, false, "", 0)
		return
	}

	balance, _ := h.service.GetBalance(r.Context(), customer.ID)
	var steeps int64
	if balance != nil {
		steeps = balance.SteepsEarned
	}

	name := customer.DisplayName
	if name == "" {
		name = customer.Email
	}
	stampPage(w, true, name, steeps)
}

func stampPage(w http.ResponseWriter, ok bool, name string, steeps int64) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`<!DOCTYPE html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><title>Whisked</title></head><body style="font-family:system-ui;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#F5F0E8"><div style="text-align:center;padding:32px"><p style="font-size:36px;margin:0">🔔</p><h2 style="font-weight:600;font-size:20px;margin:16px 0 8px;color:#1E1A14">Invalid or expired code</h2><p style="color:#7A7169;font-size:14px;margin:0">Ask the customer to refresh their app.</p></div></body></html>`))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><title>Whisked</title></head><body style="font-family:system-ui;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#F5F0E8"><div style="text-align:center;padding:32px"><p style="font-size:48px;margin:0">🔔</p><h2 style="font-weight:600;font-size:22px;margin:16px 0 6px;color:#1E1A14">Steep recorded</h2><p style="color:#7A7169;font-size:15px;margin:0">` + name + ` &mdash; ` + fmt.Sprintf("%d", steeps) + ` steeps</p></div></body></html>`))
}

// ── POST /v1/webhooks/square ──────────────────────────────────────────────────

func (h *handler) squareWebhook(w http.ResponseWriter, r *http.Request) {
	httputil.Respond(w, http.StatusOK, nil) // handled by shopifyHandler equivalent in square.go
}

func mustCustomerID(r *http.Request) platform.CustomerID {
	id, ok := middleware.AuthenticatedCustomerID(r.Context())
	if !ok {
		panic("loyalty: handler called without auth middleware")
	}
	return id
}
