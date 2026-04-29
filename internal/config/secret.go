// Package config loads runtime configuration from environment variables and
// wraps sensitive values in the Secret type so they cannot be accidentally
// logged or serialized.
//
// Call config.Load() once at startup. If any required variable is missing or
// any value fails validation, Load returns an error and the program should exit.
// This fail-fast behavior ensures misconfigured deployments are caught
// immediately rather than failing at the first request that needs the value.
package config

import (
	"errors"
	"log/slog"
)

// Secret wraps a sensitive string value. It never appears in logs, fmt output,
// or JSON serialization. Call Expose() only at the exact point of use.
type Secret struct {
	v string
}

func NewSecret(v string) Secret { return Secret{v: v} }

func (s Secret) IsEmpty() bool { return s.v == "" }

// Expose returns the raw value. Keep call sites minimal and obvious.
func (s Secret) Expose() string { return s.v }

// String redacts — safe for fmt.Sprintf, errors, and any string context.
func (s Secret) String() string { return "[REDACTED]" }

// LogValue implements slog.LogValuer so structured logs never see the value.
func (s Secret) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// MarshalJSON prevents accidental serialization into API responses.
func (s Secret) MarshalJSON() ([]byte, error) {
	return nil, errors.New("secrets cannot be marshaled to JSON")
}
