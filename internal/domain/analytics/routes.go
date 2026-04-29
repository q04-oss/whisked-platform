package analytics

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/q04-oss/whisked-platform/internal/app"
	"github.com/q04-oss/whisked-platform/internal/middleware"
)

// Router mounts the analytics domain routes.
//
// The ingest endpoint is public — anonymous visitors don't have JWTs.
// Auth middleware is applied as optional context enrichment: if a valid JWT
// is present, the CustomerID is attached; if not, only the VisitorID is used.
//
// The identify endpoint requires authentication — a customer must be logged
// in to link their visitor history to their account.
func Router(state *app.State) http.Handler {
	h := &handler{
		service: NewService(
			NewRepository(state.DB),
			state.Audit,
		),
	}

	r := chi.NewRouter()

	// Public ingestion — available to anonymous visitors.
	// OptionalAuth enriches the context with CustomerID if a valid JWT is present.
	r.With(middleware.OptionalAuth(
		state.Config.JWTSecret.Expose(),
		state.Redis,
	)).Post("/events", h.ingest)

	// Identify — requires authentication.
	r.With(middleware.RequireAuth(
		state.Config.JWTSecret.Expose(),
		state.Redis,
	)).Post("/identify", h.identify)

	return r
}
