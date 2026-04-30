package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration loaded from environment variables.
// Secrets are typed as Secret — they cannot be logged or serialized.
type Config struct {
	Port int

	// Database
	DatabaseURL Secret

	// Redis — required. Used for rate limiting, nonce deduplication, and
	// idempotent loyalty operations.
	RedisURL Secret

	// Auth
	JWTSecret Secret

	// iOS request signing — all mobile requests must carry a valid HMAC.
	HMACSharedKey Secret

	// Shopify — verifies orders/paid webhook authenticity.
	// If not set, the webhook endpoint returns 503.
	ShopifyWebhookSecret Secret

	// Square — payment processing and customer directory.
	//
	// OAuth flow (preferred):
	//   SquareAppID + SquareAppSecret drive the OAuth connect/callback endpoints.
	//   Tokens are stored in the database after Belle completes the one-time
	//   OAuth flow at GET /v1/square/oauth/connect.
	//
	// Legacy static token (fallback, used before OAuth is completed):
	//   SquareAccessToken is used when no OAuth token is in the database.
	//   Once OAuth is complete this variable can be removed.
	//
	// SquareWebhookSigningKey: verifies Square webhook payload authenticity.
	// SquareNotificationURL must exactly match the webhook URL in Square dashboard.
	SquareAppID            string
	SquareAppSecret        Secret
	SquareOAuthRedirectURL string
	SquareAccessToken      Secret // legacy — superseded by OAuth tokens in DB
	SquareWebhookSigningKey Secret
	SquareLocationID        string
	SquareNotificationURL   string

	// Anthropic — powers the brand chat experience on the website.
	AnthropicAPIKey Secret

	// Operator
	AdminPIN Secret
}

func Load() (*Config, error) {
	var missing []string

	get := func(key string) Secret {
		v := os.Getenv(key)
		if v == "" {
			missing = append(missing, key)
		}
		return NewSecret(v)
	}

	cfg := &Config{
		DatabaseURL:   get("DATABASE_URL"),
		RedisURL:      get("REDIS_URL"),
		JWTSecret:     get("JWT_SECRET"),
		HMACSharedKey: get("HMAC_SHARED_KEY"),
		AdminPIN:      get("ADMIN_PIN"),

		// Optional
		ShopifyWebhookSecret:    NewSecret(os.Getenv("SHOPIFY_WEBHOOK_SECRET")),
		SquareAppID:             os.Getenv("SQUARE_APP_ID"),
		SquareAppSecret:         NewSecret(os.Getenv("SQUARE_APP_SECRET")),
		SquareOAuthRedirectURL:  os.Getenv("SQUARE_OAUTH_REDIRECT_URL"),
		SquareAccessToken:       NewSecret(os.Getenv("SQUARE_ACCESS_TOKEN")),
		SquareWebhookSigningKey: NewSecret(os.Getenv("SQUARE_WEBHOOK_SIGNING_KEY")),
		SquareLocationID:        os.Getenv("SQUARE_LOCATION_ID"),
		SquareNotificationURL:   os.Getenv("SQUARE_NOTIFICATION_URL"),
		AnthropicAPIKey:         NewSecret(os.Getenv("ANTHROPIC_API_KEY")),
		Port:                    optionalInt("PORT", 8080),
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %v", missing)
	}

	if len(cfg.JWTSecret.Expose()) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}

	if err := validatePIN("ADMIN_PIN", cfg.AdminPIN.Expose()); err != nil {
		return nil, err
	}

	return cfg, nil
}

func optionalInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// validatePIN rejects weak PINs at startup so misconfigured deployments fail fast.
func validatePIN(key, pin string) error {
	if len(pin) < 8 {
		return fmt.Errorf("`%s` must be at least 8 characters", key)
	}
	first := pin[0]
	for i := 1; i < len(pin); i++ {
		if pin[i] != first {
			return nil
		}
	}
	return fmt.Errorf("`%s` must not be all the same character", key)
}
