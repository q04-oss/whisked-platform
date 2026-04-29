// Package auth handles customer registration, login, token issuance, refresh,
// and logout. It owns the credential lifecycle independently of the customer
// profile — the customers domain manages profile data; this domain manages
// how customers prove their identity.
//
// Tokens:
//   - Access tokens are short-lived JWTs (15 min). Verified on every request.
//   - Refresh tokens are long-lived (7 days), stored in Redis. Exchanged for a
//     new token pair. Rotated on each use — a refresh token can only be used once.
//   - Logout revokes the refresh token from Redis immediately.
package auth

import (
	"time"

	"github.com/q04-oss/whisked-platform/internal/platform"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
)

// RegisterParams carries the inputs for new customer registration.
type RegisterParams struct {
	Email       string
	DisplayName string
	Password    string
}

// LoginParams carries the inputs for authentication.
type LoginParams struct {
	Email    string
	Password string
}

// TokenPair is the response returned after successful registration or login.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // access token TTL in seconds
}

// claims is the JWT payload. Unexported — constructed and parsed only within
// this package.
type claims struct {
	CustomerID int64  `json:"sub"`
	SessionID  string `json:"jti"` // used for revocation
}

// authenticatedCustomer is the minimal customer info the auth domain needs
// internally. The auth domain does not import the customers package — it owns
// its own minimal slice of the data.
type authenticatedCustomer struct {
	id           platform.CustomerID
	email        string
	displayName  string
	passwordHash string
	createdAt    time.Time
}
