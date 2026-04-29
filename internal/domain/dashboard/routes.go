package dashboard

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/q04-oss/whisked-platform/internal/app"
	"github.com/q04-oss/whisked-platform/internal/middleware"
)

// Router mounts the dashboard domain routes under a separate authentication
// boundary. All routes except /auth/login require a valid staff JWT.
// Customer JWTs are explicitly rejected by RequireStaffAuth.
func Router(state *app.State) http.Handler {
	h := &handler{
		service: NewService(
			NewRepository(state.DB),
			state.Redis,
			state.Config,
			state.Audit,
		),
	}

	r := chi.NewRouter()

	// Public within the dashboard boundary — staff login.
	r.Post("/auth/login", h.login)

	// All other dashboard routes require a staff JWT.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireStaffAuth(
			state.Config.JWTSecret.Expose(),
			state.Redis,
		))

		r.Get("/overview",        h.overview)
		r.Get("/loyalty",         h.loyaltySummary)
		r.Get("/loyalty/events",  h.recentEvents)
		r.Get("/funnel",          h.funnel)
		r.Get("/customers",       h.customers)
	})

	return r
}
