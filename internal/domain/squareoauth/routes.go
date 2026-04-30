package squareoauth

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/q04-oss/whisked-platform/internal/config"
	"github.com/q04-oss/whisked-platform/internal/integrations/square"
	"github.com/q04-oss/whisked-platform/internal/middleware"
)

// Router mounts the Square OAuth endpoints.
//
// GET /v1/square/oauth/connect  — staff-only, initiates the OAuth flow
// GET /v1/square/oauth/callback — public, Square redirects here after approval
//
// The connect route requires dashboard (staff) authentication because only
// the operator should be able to connect a Square merchant account.
// The callback route is public — Square does not send auth headers, and
// the CSRF state parameter provides the equivalent protection.
func Router(
	db *pgxpool.Pool,
	rdb *redis.Client,
	ts *square.TokenStore,
	cfg *config.Config,
) http.Handler {
	svc := NewService(db, rdb, ts, cfg.SquareLocationID)
	h := &handler{
		service: svc,
		cfg: handlerConfig{
			AppID:       cfg.SquareAppID,
			RedirectURL: cfg.SquareOAuthRedirectURL,
		},
	}

	r := chi.NewRouter()

	// connect requires staff credentials.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireStaffAuth(cfg.JWTSecret.Expose(), rdb))
		r.Get("/connect", h.connect)
	})

	// callback is called by Square — no auth, CSRF via state param.
	r.Get("/callback", h.callback)

	return r
}

// NewTokenStore constructs the in-memory token cache backed by the database.
// Called once at startup in app.NewState so all domains share a single instance.
func NewTokenStore(
	db *pgxpool.Pool,
	rdb *redis.Client,
	cfg *config.Config,
	httpClient *http.Client,
) (*square.TokenStore, error) {
	// Bootstrap a temporary service just to satisfy the TokenPersistence interface
	// during startup. The returned TokenStore is what gets passed to NewService later.
	persist := &Service{db: db, redis: rdb}

	oauthCfg := square.OAuthConfig{
		AppID:       cfg.SquareAppID,
		AppSecret:   cfg.SquareAppSecret.Expose(),
		RedirectURL: cfg.SquareOAuthRedirectURL,
	}

	return square.NewTokenStore(context.Background(), oauthCfg, persist, httpClient)
}
