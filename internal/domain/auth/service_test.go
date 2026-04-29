package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/config"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

// mockRepository satisfies the auth repository interface for unit tests.
type mockRepository struct {
	customers     map[string]*authenticatedCustomer
	nextID        int64
	createErr     error
}

func newMockRepo() *mockRepository {
	return &mockRepository{
		customers: make(map[string]*authenticatedCustomer),
		nextID:    1,
	}
}

func (m *mockRepository) createCustomerWithCredentials(_ context.Context, email, displayName, passwordHash string) (*authenticatedCustomer, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	if _, exists := m.customers[email]; exists {
		return nil, errors.New("unique constraint customers_email_key")
	}
	c := &authenticatedCustomer{
		id:           platform.CustomerID(m.nextID),
		email:        email,
		displayName:  displayName,
		passwordHash: passwordHash,
	}
	m.customers[email] = c
	m.nextID++
	return c, nil
}

func (m *mockRepository) getCustomerByEmailWithHash(_ context.Context, email string) (*authenticatedCustomer, error) {
	c, ok := m.customers[email]
	if !ok {
		return nil, platform.ErrNotFound
	}
	return c, nil
}

func testService(t *testing.T) (*Service, *mockRepository) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	repo := newMockRepo()
	cfg := &config.Config{}
	// Use a test JWT secret via unexported field access through config.NewSecret.
	// We set JWTSecret directly since config is in the same module.
	cfg.JWTSecret = config.NewSecret("test-jwt-secret-that-is-long-enough-32chars")
	svc := NewService(repo, rdb, cfg, audit.New(nil))
	return svc, repo
}

func TestService_Register(t *testing.T) {
	tests := []struct {
		name    string
		params  RegisterParams
		wantErr bool
		errIs   *platform.ClientError
	}{
		{
			name:    "valid registration",
			params:  RegisterParams{Email: "belle@whisked.ca", Password: "matcha2026", DisplayName: "Belle"},
			wantErr: false,
		},
		{
			name:    "missing email",
			params:  RegisterParams{Password: "matcha2026"},
			wantErr: true,
			errIs:   platform.ErrBadRequest,
		},
		{
			name:    "password too short",
			params:  RegisterParams{Email: "a@b.com", Password: "short"},
			wantErr: true,
			errIs:   platform.ErrBadRequest,
		},
		{
			name:    "invalid email",
			params:  RegisterParams{Email: "notanemail", Password: "matcha2026"},
			wantErr: true,
			errIs:   platform.ErrBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := testService(t)
			pair, err := svc.Register(context.Background(), tt.params)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errIs != nil {
					ce, ok := platform.AsClientError(err)
					if !ok {
						t.Fatalf("expected ClientError, got %T: %v", err, err)
					}
					if ce.HTTPStatus != tt.errIs.HTTPStatus {
						t.Errorf("status = %d, want %d", ce.HTTPStatus, tt.errIs.HTTPStatus)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pair.AccessToken == "" || pair.RefreshToken == "" {
				t.Error("expected non-empty token pair")
			}
		})
	}
}

func TestService_Login_WrongPassword(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	// Register first.
	_, err := svc.Register(ctx, RegisterParams{
		Email:    "belle@whisked.ca",
		Password: "correctpassword",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Login with wrong password.
	_, err = svc.Login(ctx, LoginParams{
		Email:    "belle@whisked.ca",
		Password: "wrongpassword",
	})
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	ce, ok := platform.AsClientError(err)
	if !ok || ce.HTTPStatus != 401 {
		t.Errorf("expected 401 ClientError, got %v", err)
	}
}

func TestService_Login_UnknownEmail(t *testing.T) {
	svc, _ := testService(t)

	_, err := svc.Login(context.Background(), LoginParams{
		Email:    "nobody@whisked.ca",
		Password: "anypassword",
	})
	if err == nil {
		t.Fatal("expected error for unknown email")
	}
	ce, ok := platform.AsClientError(err)
	if !ok || ce.HTTPStatus != 401 {
		t.Errorf("expected 401 ClientError, got %v", err)
	}
}

func TestService_Refresh_SingleUse(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	pair, err := svc.Register(ctx, RegisterParams{
		Email:    "belle@whisked.ca",
		Password: "matcha2026",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// First refresh succeeds.
	newPair, err := svc.Refresh(ctx, pair.RefreshToken)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if newPair.AccessToken == "" {
		t.Error("expected new access token")
	}

	// Second use of the original refresh token must fail — single-use rotation.
	_, err = svc.Refresh(ctx, pair.RefreshToken)
	if err == nil {
		t.Error("expected error on second use of refresh token")
	}
}

func TestService_Logout_InvalidatesRefreshToken(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	pair, err := svc.Register(ctx, RegisterParams{
		Email:    "belle@whisked.ca",
		Password: "matcha2026",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := svc.Logout(ctx, pair.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}

	// Refresh after logout must fail.
	_, err = svc.Refresh(ctx, pair.RefreshToken)
	if err == nil {
		t.Error("expected error refreshing after logout")
	}
}

func TestService_DuplicateEmail(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	params := RegisterParams{Email: "belle@whisked.ca", Password: "matcha2026"}

	if _, err := svc.Register(ctx, params); err != nil {
		t.Fatalf("first registration: %v", err)
	}

	_, err := svc.Register(ctx, params)
	if err == nil {
		t.Fatal("expected error for duplicate email")
	}
	ce, ok := platform.AsClientError(err)
	if !ok || ce.HTTPStatus != 409 {
		t.Errorf("expected 409 ClientError, got %v", err)
	}
}
