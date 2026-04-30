// Package squareoauth manages the Square OAuth 2.0 connection for the
// platform's merchant account.
//
// Flow:
//  1. Staff visits GET /v1/square/oauth/connect (requires dashboard auth).
//     The handler generates a CSRF state token, stores it in Redis with a
//     10-minute TTL, and redirects to Square's authorization page.
//  2. Belle logs in at Square, approves the permission request, and is
//     redirected back to GET /v1/square/oauth/callback with a short-lived code.
//  3. The callback validates the state token (CSRF), exchanges the code for
//     access + refresh tokens via Square's /oauth2/token endpoint, and persists
//     the token pair to the database.
//  4. The in-memory TokenStore is updated immediately — no restart needed.
package squareoauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/q04-oss/whisked-platform/internal/integrations/square"
)

const (
	stateKeyPrefix = "whisked:square-oauth-state:"
	stateTTL       = 10 * time.Minute
)

// Service manages the OAuth token lifecycle for the Square integration.
type Service struct {
	db         *pgxpool.Pool
	redis      *redis.Client
	tokenStore *square.TokenStore
	// locationID is the Square location ID for the merchant. Square's token
	// exchange response does not include location information, so we source
	// it from SQUARE_LOCATION_ID in config and store it alongside the token.
	locationID string
}

// NewService constructs the OAuth service.
func NewService(db *pgxpool.Pool, rdb *redis.Client, ts *square.TokenStore, locationID string) *Service {
	return &Service{db: db, redis: rdb, tokenStore: ts, locationID: locationID}
}

// AuthorizationURL builds the Square authorization URL and stores the CSRF
// state token in Redis. The URL is returned to the handler for redirection.
func (s *Service) AuthorizationURL(ctx context.Context, appID, redirectURL string) (string, error) {
	state, err := generateState()
	if err != nil {
		return "", fmt.Errorf("generating state: %w", err)
	}

	if err := s.redis.Set(ctx, stateKeyPrefix+state, "1", stateTTL).Err(); err != nil {
		return "", fmt.Errorf("storing state: %w", err)
	}

	url := fmt.Sprintf(
		"https://connect.squareup.com/oauth2/authorize?client_id=%s&scope=%s&session=false&state=%s",
		appID,
		"ORDERS_WRITE+PAYMENTS_READ+CUSTOMERS_READ+CUSTOMERS_WRITE",
		state,
	)
	return url, nil
}

// HandleCallback validates the CSRF state, exchanges the code for tokens,
// and persists them. The in-memory TokenStore is updated immediately.
func (s *Service) HandleCallback(ctx context.Context, code, state string) error {
	// Validate and consume the state token atomically (prevents CSRF and replay).
	deleted, err := s.redis.GetDel(ctx, stateKeyPrefix+state).Result()
	if err != nil || deleted == "" {
		return errors.New("invalid or expired state parameter")
	}

	record, err := s.tokenStore.ExchangeCode(ctx, code)
	if err != nil {
		return fmt.Errorf("exchanging code: %w", err)
	}
	// Square's token response does not include the location ID.
	// We source it from config (SQUARE_LOCATION_ID).
	record.LocationID = s.locationID

	if err := s.tokenStore.Store(ctx, record); err != nil {
		return fmt.Errorf("storing token: %w", err)
	}

	slog.Info("square oauth connected",
		"merchant_id", record.MerchantID,
		"expires_at", record.ExpiresAt.Format(time.RFC3339),
	)
	return nil
}

// LoadToken implements square.TokenPersistence.
// Called at startup to hydrate the in-memory TokenStore from the database.
// Returns nil, nil if no token is stored yet (OAuth not yet completed).
func (s *Service) LoadToken(ctx context.Context) (*square.TokenRecord, error) {
	var r square.TokenRecord
	err := s.db.QueryRow(ctx, `
		SELECT merchant_id, location_id, access_token, refresh_token, expires_at
		FROM square_oauth_tokens
		ORDER BY updated_at DESC
		LIMIT 1
	`).Scan(&r.MerchantID, &r.LocationID, &r.AccessToken, &r.RefreshToken, &r.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // OAuth not yet completed
	}
	if err != nil {
		return nil, fmt.Errorf("loading square token: %w", err)
	}
	return &r, nil
}

// SaveToken implements square.TokenPersistence.
// Upserts the token row so re-running OAuth for the same merchant overwrites the old token.
func (s *Service) SaveToken(ctx context.Context, t *square.TokenRecord) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO square_oauth_tokens
		    (merchant_id, location_id, access_token, refresh_token, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (merchant_id) DO UPDATE SET
		    access_token  = EXCLUDED.access_token,
		    refresh_token = EXCLUDED.refresh_token,
		    expires_at    = EXCLUDED.expires_at
	`, t.MerchantID, t.LocationID, t.AccessToken, t.RefreshToken, t.ExpiresAt)
	return err
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
