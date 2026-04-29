package dashboard

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

// ── POST /v1/dashboard/auth/login ─────────────────────────────────────────────

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.RespondError(w, r, platform.ErrBadRequest)
		return
	}

	pair, err := h.service.Login(r.Context(), body.Email, body.Password)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, pair)
}

// ── GET /v1/dashboard/overview ────────────────────────────────────────────────

func (h *handler) overview(w http.ResponseWriter, r *http.Request) {
	staffID := mustStaffID(r)

	data, err := h.service.GetOverview(r.Context(), staffID)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, data)
}

// ── GET /v1/dashboard/loyalty ─────────────────────────────────────────────────

func (h *handler) loyaltySummary(w http.ResponseWriter, r *http.Request) {
	staffID := mustStaffID(r)

	days, _ := strconv.Atoi(r.URL.Query().Get("days"))

	data, err := h.service.GetLoyaltySummary(r.Context(), staffID, days)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, data)
}

// ── GET /v1/dashboard/loyalty/events ─────────────────────────────────────────

func (h *handler) recentEvents(w http.ResponseWriter, r *http.Request) {
	staffID := mustStaffID(r)

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	events, err := h.service.GetRecentLoyaltyEvents(r.Context(), staffID, limit)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, events)
}

// ── GET /v1/dashboard/funnel ──────────────────────────────────────────────────

func (h *handler) funnel(w http.ResponseWriter, r *http.Request) {
	staffID := mustStaffID(r)

	stats, err := h.service.GetFunnelStats(r.Context(), staffID)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, stats)
}

// ── GET /v1/dashboard/customers ───────────────────────────────────────────────

func (h *handler) customers(w http.ResponseWriter, r *http.Request) {
	staffID := mustStaffID(r)

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	customers, err := h.service.GetCustomerList(r.Context(), staffID, limit, offset)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, customers)
}

func mustStaffID(r *http.Request) platform.StaffID {
	id, ok := middleware.AuthenticatedStaffID(r.Context())
	if !ok {
		panic("dashboard: handler called without staff auth middleware")
	}
	return id
}
