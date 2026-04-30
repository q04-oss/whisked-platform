// Package server constructs the HTTP router and applies the middleware chain.
//
// Middleware runs outermost-first (top of this file = first to execute).
// Every request passes through the full global stack regardless of route.
// Route-specific middleware (HMAC, authentication) is applied inside the
// relevant route groups, not globally.
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/q04-oss/whisked-platform/internal/app"
	"github.com/q04-oss/whisked-platform/internal/domain/analytics"
	"github.com/q04-oss/whisked-platform/internal/domain/auth"
	"github.com/q04-oss/whisked-platform/internal/domain/customers"
	"github.com/q04-oss/whisked-platform/internal/domain/dashboard"
	"github.com/q04-oss/whisked-platform/internal/domain/loyalty"
	"github.com/q04-oss/whisked-platform/internal/domain/squareoauth"
	"github.com/q04-oss/whisked-platform/internal/middleware"
)

// New builds and returns the root HTTP handler with the full middleware stack.
func New(state *app.State) http.Handler {
	r := chi.NewRouter()

	// ── Global middleware ──────────────────────────────────────────────────────
	// Applied to every request in the order listed.
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.RateLimit(state.Redis))

	// ── Observability ──────────────────────────────────────────────────────────
	r.Use(func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "whisked-api")
	})

	// ── Internal ───────────────────────────────────────────────────────────────
	r.Get("/health", handleHealth(state))
	r.Handle("/metrics", state.Telemetry.MetricsHandler())

	// ── QR stamp page — HTML, opened by staff scanning a customer QR code ──────
	// Not under /v1 because it returns HTML, not JSON, and is opened in a browser.
	r.Get("/stamp", loyalty.StampHandler(state))

	// ── API v1 ─────────────────────────────────────────────────────────────────
	r.Route("/v1", func(r chi.Router) {

		// ── Public routes — website and unauthenticated iOS ────────────────────
		r.Mount("/auth", auth.Router(state))
		// r.Mount("/chat",    chat.Router(state))    // Claude brand search
		// r.Mount("/catalog", catalog.Router(state)) // public menu

		// ── Authenticated routes — identified customers ─────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(
				state.Config.JWTSecret.Expose(),
				state.Redis,
			))
			r.Mount("/customers", customers.Router(state))
		})

		// ── Loyalty — mixed auth (routes handle their own middleware) ──────────
		r.Mount("/loyalty", loyalty.Router(state))

		// ── Analytics — public ingest + authenticated identify ─────────────────
		r.Mount("/analytics", analytics.Router(state))

		// ── Dashboard — separate staff auth boundary ───────────────────────────
		r.Mount("/dashboard", dashboard.Router(state))

		// ── Square OAuth — connect + callback for merchant account linking ─────
		// connect requires staff auth; callback is public (CSRF via state param).
		if state.SquareTokens != nil {
			r.Mount("/square/oauth", squareoauth.Router(
				state.DB,
				state.Redis,
				state.SquareTokens,
				state.Config,
			))
		}

		// ── iOS routes — HMAC signed + authenticated ───────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireHMAC(
				state.Config.HMACSharedKey.Expose(),
				state.Redis,
			))
			// r.Use(middleware.RequireAuth(state))
			// r.Mount("/loyalty", loyalty.Router(state))
		})

		// ── Shopify webhooks ───────────────────────────────────────────────────
		// r.Mount("/webhooks/shopify", shopify.Router(state))

		// ── Dashboard — separate auth boundary ────────────────────────────────
		// r.Group(func(r chi.Router) {
		//     r.Use(middleware.RequireDashboardAuth(state))
		//     r.Mount("/dashboard", dashboard.Router(state))
		// })
	})

	return r
}

func handleHealth(state *app.State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := state.DB.Ping(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := state.Redis.Ping(r.Context()).Err(); err != nil {
			http.Error(w, "redis unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
