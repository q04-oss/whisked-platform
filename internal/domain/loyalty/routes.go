package loyalty

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/q04-oss/whisked-platform/internal/app"
	"github.com/q04-oss/whisked-platform/internal/middleware"
)

// Router mounts the loyalty domain routes.
//
// Route security summary:
//   - balance, history, qr-token: JWT required
//   - stamp, redeem:              JWT + HMAC (must originate from iOS app)
//   - webhooks/shopify:           public, Shopify HMAC validated internally
//   - webhooks/square:            public, Square HMAC validated internally
//
// The /stamp QR endpoint is mounted at the root by server.go (not here)
// because it returns HTML and is accessed by staff phone cameras, not the app.
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

	sqh := &squareWebhookHandler{
		service:         svc,
		signingKey:      state.Config.SquareWebhookSigningKey.Expose(),
		notificationURL: state.Config.SquareNotificationURL,
		audit:           state.Audit,
	}

	r := chi.NewRouter()

	// Authenticated read routes — JWT required.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(
			state.Config.JWTSecret.Expose(),
			state.Redis,
		))
		r.Get("/balance",  h.getBalance)
		r.Get("/history",  h.getHistory)
		r.Get("/qr-token", h.qrToken)
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
		r.Post("/stamp",  h.stamp)
		r.Post("/redeem", h.redeem)
	})

	// Webhook routes — public, self-validating.
	r.Post("/webhooks/shopify", sh.handleOrderPaid)
	r.Post("/webhooks/square",  sqh.handle)

	return r
}

// StampHandler returns the public QR stamp handler for mounting at the root.
// Accessed by staff scanning a customer QR code — returns HTML, not JSON.
func StampHandler(state *app.State) http.HandlerFunc {
	svc := NewService(NewRepository(state.DB), state.Redis, state.Audit)
	h := &handler{service: svc}
	return h.stampViaQR
}
