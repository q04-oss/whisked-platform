package auth

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/q04-oss/whisked-platform/internal/app"
)

// Router mounts the auth domain routes. All routes here are public —
// no authentication middleware is applied.
func Router(state *app.State) http.Handler {
	h := &handler{
		service: NewService(
			NewRepository(state.DB),
			state.Redis,
			state.Config,
			state.Audit,
			state.Square,
		),
	}

	r := chi.NewRouter()
	r.Post("/register", h.register)
	r.Post("/login", h.login)
	r.Post("/refresh", h.refresh)
	r.Post("/logout", h.logout)
	return r
}
