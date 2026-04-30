// Package app defines the shared application state injected into every handler.
package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/config"
	"github.com/q04-oss/whisked-platform/internal/domain/squareoauth"
	"github.com/q04-oss/whisked-platform/internal/integrations/square"
	"github.com/q04-oss/whisked-platform/internal/telemetry"
)

// State is the shared application state passed to every handler via closure.
// All fields are safe for concurrent use. State is constructed once at startup
// and never mutated after the server begins serving requests.
type State struct {
	DB           *pgxpool.Pool
	Redis        *redis.Client
	Config       *config.Config
	Telemetry    *telemetry.Provider
	Audit        *audit.Writer
	HTTP         *http.Client
	Square       *square.Client     // nil when neither static token nor OAuth token is configured
	SquareTokens *square.TokenStore // nil when SQUARE_APP_ID is not configured
}

// NewState constructs the application state from its dependencies.
// It bootstraps the Square token store from the database so OAuth tokens
// are available immediately on startup without a restart.
func NewState(
	ctx context.Context,
	db *pgxpool.Pool,
	rdb *redis.Client,
	cfg *config.Config,
	tel *telemetry.Provider,
) (*State, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}

	s := &State{
		DB:        db,
		Redis:     rdb,
		Config:    cfg,
		Telemetry: tel,
		Audit:     audit.New(db),
		HTTP:      httpClient,
	}

	// Prefer OAuth tokens when the app is configured for it.
	if cfg.SquareAppID != "" {
		ts, err := squareoauth.NewTokenStore(db, rdb, cfg, httpClient)
		if err != nil {
			return nil, fmt.Errorf("initializing Square token store: %w", err)
		}
		s.SquareTokens = ts
		s.Square = square.NewFromTokenStore(ts, httpClient)
	} else {
		// Fall back to static access token (used before OAuth is set up).
		s.Square = square.NewFromConfig(cfg.SquareAccessToken.Expose(), httpClient)
	}

	return s, nil
}
