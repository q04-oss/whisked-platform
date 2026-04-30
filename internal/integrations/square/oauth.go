package square

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	oauthBaseURL    = "https://connect.squareup.com/oauth2"
	tokenExpirySoon = 24 * time.Hour // refresh when fewer than this remain
)

// OAuthConfig holds the Square application credentials needed for OAuth.
type OAuthConfig struct {
	AppID          string
	AppSecret      string
	RedirectURL    string
}

// TokenRecord mirrors the database row for a Square OAuth token pair.
// Exported so the squareoauth service can read and write it without the
// square package importing the database layer.
type TokenRecord struct {
	MerchantID   string
	LocationID   string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// TokenPersistence is implemented by the squareoauth service to load and
// save tokens. The square package does not import the database layer directly —
// this interface keeps the dependency pointing inward.
type TokenPersistence interface {
	LoadToken(ctx context.Context) (*TokenRecord, error)
	SaveToken(ctx context.Context, t *TokenRecord) error
}

// TokenStore wraps a persisted OAuth token pair and refreshes it transparently.
// It is safe for concurrent use.
type TokenStore struct {
	cfg     OAuthConfig
	persist TokenPersistence
	http    *http.Client

	mu           sync.RWMutex
	accessToken  string
	refreshToken string
	expiresAt    time.Time
	merchantID   string
	locationID   string
}

// NewTokenStore loads the current token from persistence and returns a
// TokenStore ready for use.
func NewTokenStore(ctx context.Context, cfg OAuthConfig, persist TokenPersistence, httpClient *http.Client) (*TokenStore, error) {
	ts := &TokenStore{
		cfg:     cfg,
		persist: persist,
		http:    httpClient,
	}
	if err := ts.reload(ctx); err != nil {
		return nil, err
	}
	return ts, nil
}

// AccessToken returns the current access token, refreshing it first if it
// expires within tokenExpirySoon.
func (ts *TokenStore) AccessToken(ctx context.Context) (string, error) {
	ts.mu.RLock()
	token, expiry := ts.accessToken, ts.expiresAt
	ts.mu.RUnlock()

	if token == "" {
		return "", fmt.Errorf("square: no OAuth token — complete /v1/square/oauth/connect first")
	}
	if time.Until(expiry) < tokenExpirySoon {
		if err := ts.refresh(ctx); err != nil {
			return "", fmt.Errorf("square: token refresh: %w", err)
		}
		ts.mu.RLock()
		token = ts.accessToken
		ts.mu.RUnlock()
	}
	return token, nil
}

// MerchantID returns the Square merchant ID from the stored token.
func (ts *TokenStore) MerchantID() string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.merchantID
}

// LocationID returns the Square location ID from the stored token.
func (ts *TokenStore) LocationID() string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.locationID
}

// Store saves a newly obtained token pair. Called by the OAuth callback handler.
func (ts *TokenStore) Store(ctx context.Context, record *TokenRecord) error {
	if err := ts.persist.SaveToken(ctx, record); err != nil {
		return err
	}
	ts.mu.Lock()
	ts.accessToken = record.AccessToken
	ts.refreshToken = record.RefreshToken
	ts.expiresAt = record.ExpiresAt
	ts.merchantID = record.MerchantID
	ts.locationID = record.LocationID
	ts.mu.Unlock()
	return nil
}

// ExchangeCode exchanges the authorization code received from Square's
// OAuth callback for an access token and refresh token.
func (ts *TokenStore) ExchangeCode(ctx context.Context, code string) (*TokenRecord, error) {
	body, _ := json.Marshal(map[string]string{
		"client_id":     ts.cfg.AppID,
		"client_secret": ts.cfg.AppSecret,
		"code":          code,
		"grant_type":    "authorization_code",
		"redirect_uri":  ts.cfg.RedirectURL,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthBaseURL+"/token", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Square-Version", "2024-01-18")

	resp, err := ts.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchanging code: %w", err)
	}
	defer resp.Body.Close()

	return ts.decodeTokenResponse(resp)
}

// refresh obtains a new access token using the stored refresh token.
func (ts *TokenStore) refresh(ctx context.Context) error {
	ts.mu.RLock()
	refreshToken := ts.refreshToken
	merchantID := ts.merchantID
	locationID := ts.locationID
	ts.mu.RUnlock()

	body, _ := json.Marshal(map[string]string{
		"client_id":     ts.cfg.AppID,
		"client_secret": ts.cfg.AppSecret,
		"refresh_token": refreshToken,
		"grant_type":    "refresh_token",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthBaseURL+"/token", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Square-Version", "2024-01-18")

	resp, err := ts.http.Do(req)
	if err != nil {
		return fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	record, err := ts.decodeTokenResponse(resp)
	if err != nil {
		return err
	}
	record.MerchantID = merchantID
	record.LocationID = locationID

	if err := ts.persist.SaveToken(ctx, record); err != nil {
		return fmt.Errorf("persisting refreshed token: %w", err)
	}
	ts.mu.Lock()
	ts.accessToken = record.AccessToken
	ts.refreshToken = record.RefreshToken
	ts.expiresAt = record.ExpiresAt
	ts.mu.Unlock()
	return nil
}

// reload hydrates the in-memory cache from the persistence layer.
// A missing token is not an error — the OAuth flow hasn't been run yet.
func (ts *TokenStore) reload(ctx context.Context) error {
	record, err := ts.persist.LoadToken(ctx)
	if err != nil {
		return fmt.Errorf("loading Square token: %w", err)
	}
	if record == nil {
		return nil // OAuth not yet completed
	}
	ts.mu.Lock()
	ts.accessToken = record.AccessToken
	ts.refreshToken = record.RefreshToken
	ts.expiresAt = record.ExpiresAt
	ts.merchantID = record.MerchantID
	ts.locationID = record.LocationID
	ts.mu.Unlock()
	return nil
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
	MerchantID   string `json:"merchant_id"`
	Errors       []struct {
		Detail string `json:"detail"`
	} `json:"errors"`
}

func (ts *TokenStore) decodeTokenResponse(resp *http.Response) (*TokenRecord, error) {
	var result oauthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("square oauth: %s", result.Errors[0].Detail)
	}
	expiresAt, err := time.Parse(time.RFC3339, result.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("parsing token expiry: %w", err)
	}
	return &TokenRecord{
		MerchantID:   result.MerchantID,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}
