package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/config"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

// repository is the interface the Service requires from its data layer.
type repository interface {
	createCustomerWithCredentials(ctx context.Context, email, displayName, passwordHash string) (*authenticatedCustomer, error)
	getCustomerByEmailWithHash(ctx context.Context, email string) (*authenticatedCustomer, error)
}

// Service handles registration, login, token refresh, and logout.
type Service struct {
	repo  repository
	redis *redis.Client
	cfg   *config.Config
	audit *audit.Writer
}

// NewService returns a Service wired to its dependencies.
func NewService(repo repository, rdb *redis.Client, cfg *config.Config, audit *audit.Writer) *Service {
	return &Service{repo: repo, redis: rdb, cfg: cfg, audit: audit}
}

// Register creates a new customer account and returns a token pair.
func (s *Service) Register(ctx context.Context, params RegisterParams) (*TokenPair, error) {
	if err := validateRegisterParams(params); err != nil {
		return nil, err
	}

	params.Email = normalizeEmail(params.Email)

	hash, err := HashPassword(params.Password)
	if err != nil {
		return nil, fmt.Errorf("auth.Register: hashing password: %w", err)
	}

	customer, err := s.repo.createCustomerWithCredentials(ctx, params.Email, params.DisplayName, hash)
	if err != nil {
		if isDuplicateEmail(err) {
			return nil, platform.ErrConflict
		}
		return nil, fmt.Errorf("auth.Register: %w", err)
	}

	pair, refreshJTI, err := issueTokenPair(customer.id, s.cfg.JWTSecret.Expose())
	if err != nil {
		return nil, fmt.Errorf("auth.Register: issuing tokens: %w", err)
	}

	if err := s.storeRefreshToken(ctx, refreshJTI, customer.id); err != nil {
		return nil, fmt.Errorf("auth.Register: storing refresh token: %w", err)
	}

	s.audit.Write(ctx, audit.Entry{
		EventType:  audit.EventAuthLogin,
		ActorType:  audit.ActorCustomer,
		ActorID:    ptr(customer.id.Int64()),
		TargetType: ptr("customer"),
		TargetID:   ptr(customer.id.String()),
		Metadata:   map[string]string{"method": "register"},
	})

	return pair, nil
}

// Login authenticates a customer by email and password.
// Returns ErrUnauthenticated for any credential failure — no distinction
// between "email not found" and "wrong password" is surfaced to callers.
func (s *Service) Login(ctx context.Context, params LoginParams) (*TokenPair, error) {
	if params.Email == "" || params.Password == "" {
		return nil, platform.ErrUnauthenticated
	}

	customer, err := s.repo.getCustomerByEmailWithHash(ctx, normalizeEmail(params.Email))
	if err != nil {
		// Deliberately vague — don't distinguish "not found" from "wrong password".
		s.audit.Write(ctx, audit.Entry{
			EventType: audit.EventAuthLoginFailed,
			ActorType: audit.ActorSystem,
			Metadata:  map[string]string{"reason": "customer_not_found"},
		})
		return nil, platform.ErrUnauthenticated
	}

	ok, err := VerifyPassword(params.Password, customer.passwordHash)
	if err != nil || !ok {
		s.audit.Write(ctx, audit.Entry{
			EventType: audit.EventAuthLoginFailed,
			ActorType: audit.ActorCustomer,
			ActorID:   ptr(customer.id.Int64()),
			Metadata:  map[string]string{"reason": "wrong_password"},
		})
		return nil, platform.ErrUnauthenticated
	}

	pair, refreshJTI, err := issueTokenPair(customer.id, s.cfg.JWTSecret.Expose())
	if err != nil {
		return nil, fmt.Errorf("auth.Login: issuing tokens: %w", err)
	}

	if err := s.storeRefreshToken(ctx, refreshJTI, customer.id); err != nil {
		return nil, fmt.Errorf("auth.Login: storing refresh token: %w", err)
	}

	s.audit.Write(ctx, audit.Entry{
		EventType:  audit.EventAuthLogin,
		ActorType:  audit.ActorCustomer,
		ActorID:    ptr(customer.id.Int64()),
		TargetType: ptr("customer"),
		TargetID:   ptr(customer.id.String()),
	})

	return pair, nil
}

// Refresh exchanges a valid refresh token for a new token pair.
// The old refresh token is revoked on use — single-use rotation.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	claims, err := parseToken(refreshToken, s.cfg.JWTSecret.Expose())
	if err != nil {
		return nil, platform.ErrUnauthenticated
	}

	tokenType, _ := claims["type"].(string)
	if tokenType != "refresh" {
		return nil, platform.ErrUnauthenticated
	}

	jti, err := jtiFromClaims(claims)
	if err != nil {
		return nil, platform.ErrUnauthenticated
	}

	customerID, err := customerIDFromClaims(claims)
	if err != nil {
		return nil, platform.ErrUnauthenticated
	}

	// Verify refresh token exists in Redis — it was revoked on last use or logout.
	key := refreshKey(jti)
	exists, err := s.redis.Exists(ctx, key).Result()
	if err != nil || exists == 0 {
		return nil, platform.ErrUnauthenticated
	}

	// Revoke the old refresh token before issuing new ones — single-use rotation.
	s.redis.Del(ctx, key)

	pair, newRefreshJTI, err := issueTokenPair(customerID, s.cfg.JWTSecret.Expose())
	if err != nil {
		return nil, fmt.Errorf("auth.Refresh: issuing tokens: %w", err)
	}

	if err := s.storeRefreshToken(ctx, newRefreshJTI, customerID); err != nil {
		return nil, fmt.Errorf("auth.Refresh: storing refresh token: %w", err)
	}

	s.audit.Write(ctx, audit.Entry{
		EventType:  audit.EventAuthTokenRefresh,
		ActorType:  audit.ActorCustomer,
		ActorID:    ptr(customerID.Int64()),
	})

	return pair, nil
}

// Logout revokes the customer's refresh token, preventing future token refreshes.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	claims, err := parseToken(refreshToken, s.cfg.JWTSecret.Expose())
	if err != nil {
		// Already invalid — treat as successful logout.
		return nil
	}

	jti, err := jtiFromClaims(claims)
	if err != nil {
		return nil
	}

	customerID, _ := customerIDFromClaims(claims)

	s.redis.Del(ctx, refreshKey(jti))

	s.audit.Write(ctx, audit.Entry{
		EventType:  audit.EventAuthLogout,
		ActorType:  audit.ActorCustomer,
		ActorID:    ptr(customerID.Int64()),
	})

	return nil
}

// ── Redis helpers ─────────────────────────────────────────────────────────────

func (s *Service) storeRefreshToken(ctx context.Context, jti string, customerID platform.CustomerID) error {
	return s.redis.Set(ctx, refreshKey(jti), customerID.Int64(), refreshTokenTTL).Err()
}

func refreshKey(jti string) string {
	return "whisked:refresh:" + jti
}

// ── Validation ────────────────────────────────────────────────────────────────

func validateRegisterParams(p RegisterParams) error {
	if p.Email == "" {
		return platform.Wrap(platform.ErrBadRequest, fmt.Errorf("email is required"))
	}
	if !strings.Contains(p.Email, "@") {
		return platform.Wrap(platform.ErrBadRequest, fmt.Errorf("email is invalid"))
	}
	if len(p.Password) < 8 {
		return platform.Wrap(platform.ErrBadRequest, fmt.Errorf("password must be at least 8 characters"))
	}
	if len(p.DisplayName) > 100 {
		return platform.Wrap(platform.ErrBadRequest, fmt.Errorf("display_name exceeds 100 characters"))
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func ptr[T any](v T) *T { return &v }
