package customers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/q04-oss/whisked-platform/internal/app"
)

// Router mounts the customers domain routes onto the given chi router.
// All routes here require the caller to be authenticated — the auth middleware
// must be applied by the parent router before mounting this.
func Router(state *app.State) http.Handler {
	h := &handler{
		service: NewService(
			NewRepository(state.DB),
			state.Audit,
		),
	}

	r := chi.NewRouter()
	r.Get("/me", h.getMe)
	r.Patch("/me", h.updateMe)
	r.Delete("/me", h.deleteMe)
	return r
}
