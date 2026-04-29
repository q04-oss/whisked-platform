package loyalty

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/q04-oss/whisked-platform/internal/app"
	"github.com/q04-oss/whisked-platform/internal/middleware"
)

// Router mounts the loyalty domain routes.
//
// Authenticated routes (balance, history) require a valid JWT.
// Mutation routes (stamp, redeem) additionally require HMAC signing — they
// must originate from the iOS app.
// The Shopify webhook route is public but validates its own HMAC internally.
func Router(state *app.State) http.Handler {
	svc := NewService(
		NewRepository(state.DB),
		state.Redis,
		state.Audit,
	)
	h := &handler{service: svc}
	sh := &shopifyHandler{
		service:       svc,
		webhookSecret: state.Config.ShopifyWebhookSecret.Expose(),
		audit:         state.Audit,
	}

	r := chi.NewRouter()

	// Authenticated read routes — JWT required.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(
			state.Config.JWTSecret.Expose(),
			state.Redis,
		))
		r.Get("/balance", h.getBalance)
		r.Get("/history", h.getHistory)
	})

	// Authenticated mutation routes — JWT + HMAC required.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireHMAC(
			state.Config.HMACSharedKey.Expose(),
			state.Redis,
		))
		r.Use(middleware.RequireAuth(
			state.Config.JWTSecret.Expose(),
			state.Redis,
		))
		r.Post("/stamp", h.stamp)
		r.Post("/redeem", h.redeem)
	})

	// Shopify webhook — public, self-validating.
	r.Post("/webhooks/shopify", sh.handleOrderPaid)

	return r
}
