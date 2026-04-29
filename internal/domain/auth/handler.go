package auth

import (
	"encoding/json"
	"net/http"

	"github.com/q04-oss/whisked-platform/internal/httputil"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

type handler struct {
	service *Service
}

// ── POST /v1/auth/register ────────────────────────────────────────────────────

func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.RespondError(w, r, platform.ErrBadRequest)
		return
	}

	pair, err := h.service.Register(r.Context(), RegisterParams{
		Email:       body.Email,
		DisplayName: body.DisplayName,
		Password:    body.Password,
	})
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusCreated, pair)
}

// ── POST /v1/auth/login ───────────────────────────────────────────────────────

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.RespondError(w, r, platform.ErrBadRequest)
		return
	}

	pair, err := h.service.Login(r.Context(), LoginParams{
		Email:    body.Email,
		Password: body.Password,
	})
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, pair)
}

// ── POST /v1/auth/refresh ─────────────────────────────────────────────────────

func (h *handler) refresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.RespondError(w, r, platform.ErrBadRequest)
		return
	}
	if body.RefreshToken == "" {
		httputil.RespondError(w, r, platform.ErrBadRequest)
		return
	}

	pair, err := h.service.Refresh(r.Context(), body.RefreshToken)
	if err != nil {
		httputil.RespondError(w, r, err)
		return
	}

	httputil.Respond(w, http.StatusOK, pair)
}

// ── POST /v1/auth/logout ──────────────────────────────────────────────────────

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	// Best-effort decode — logout always succeeds from the client's perspective.
	json.NewDecoder(r.Body).Decode(&body)

	h.service.Logout(r.Context(), body.RefreshToken)
	w.WriteHeader(http.StatusNoContent)
}
