// Package app defines the shared application state injected into every handler.
package app

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/config"
	"github.com/q04-oss/whisked-platform/internal/integrations/square"
	"github.com/q04-oss/whisked-platform/internal/telemetry"
)

// State is the shared application state passed to every handler via closure.
// All fields are safe for concurrent use. State is constructed once at startup
// and never mutated after the server begins serving requests.
type State struct {
	DB        *pgxpool.Pool
	Redis     *redis.Client
	Config    *config.Config
	Telemetry *telemetry.Provider
	Audit     *audit.Writer
	HTTP      *http.Client
	Square    *square.Client // nil when SQUARE_ACCESS_TOKEN is not configured
}

// NewState constructs the application state from its dependencies.
func NewState(
	db *pgxpool.Pool,
	rdb *redis.Client,
	cfg *config.Config,
	tel *telemetry.Provider,
) *State {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	return &State{
		DB:        db,
		Redis:     rdb,
		Config:    cfg,
		Telemetry: tel,
		Audit:     audit.New(db),
		HTTP:      httpClient,
		Square:    square.NewFromConfig(cfg.SquareAccessToken.Expose(), httpClient),
	}
}
