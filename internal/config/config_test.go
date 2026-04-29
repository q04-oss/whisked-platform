package config

import (
	"testing"
)

func TestValidatePIN(t *testing.T) {
	tests := []struct {
		name    string
		pin     string
		wantErr bool
	}{
		{"valid PIN", "matcha01", false},
		{"valid PIN with symbols", "m@tch4-2026", false},
		{"too short", "abc", true},
		{"exactly 8 same chars", "aaaaaaaa", true},
		{"8 chars but valid", "abcdefgh", false},
		{"all same char long", "111111111", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePIN("TEST_PIN", tt.pin)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePIN(%q) error = %v, wantErr %v", tt.pin, err, tt.wantErr)
			}
		})
	}
}

func TestLoad_MissingRequiredVars(t *testing.T) {
	// Ensure all env vars are unset.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("HMAC_SHARED_KEY", "")
	t.Setenv("ADMIN_PIN", "")

	_, err := Load()
	if err == nil {
		t.Error("expected error when required vars are missing")
	}
}

func TestLoad_JWTSecretTooShort(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("REDIS_URL", "redis://x")
	t.Setenv("JWT_SECRET", "tooshort")
	t.Setenv("HMAC_SHARED_KEY", "validkey")
	t.Setenv("ADMIN_PIN", "validpin1")

	_, err := Load()
	if err == nil {
		t.Error("expected error for short JWT_SECRET")
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/whisked")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("JWT_SECRET", "this-is-a-valid-jwt-secret-32chars!!")
	t.Setenv("HMAC_SHARED_KEY", "hmac-shared-key-value")
	t.Setenv("ADMIN_PIN", "securepin99")
	t.Setenv("PORT", "9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.DatabaseURL.IsEmpty() {
		t.Error("DatabaseURL should not be empty")
	}
}
