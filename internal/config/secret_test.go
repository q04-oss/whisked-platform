package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestSecret_NeverExposedViaFmt(t *testing.T) {
	s := NewSecret("super-secret-value")

	formatted := fmt.Sprintf("%v", s)
	if strings.Contains(formatted, "super-secret-value") {
		t.Errorf("secret value leaked via %%v: %q", formatted)
	}
	if formatted != "[REDACTED]" {
		t.Errorf("expected [REDACTED], got %q", formatted)
	}
}

func TestSecret_NeverExposedViaFmtS(t *testing.T) {
	s := NewSecret("super-secret-value")

	formatted := fmt.Sprintf("%s", s)
	if strings.Contains(formatted, "super-secret-value") {
		t.Errorf("secret value leaked via %%s: %q", formatted)
	}
}

func TestSecret_CannotMarshalToJSON(t *testing.T) {
	s := NewSecret("super-secret-value")

	_, err := json.Marshal(s)
	if err == nil {
		t.Error("expected JSON marshal to fail, but it succeeded")
	}

	type wrapper struct {
		Key Secret `json:"key"`
	}
	_, err = json.Marshal(wrapper{Key: s})
	if err == nil {
		t.Error("expected JSON marshal of wrapper struct to fail")
	}
}

func TestSecret_NeverExposedViaStructuredLog(t *testing.T) {
	s := NewSecret("super-secret-value")

	var buf strings.Builder
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("test", "secret", s)

	if strings.Contains(buf.String(), "super-secret-value") {
		t.Errorf("secret value leaked via slog: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "[REDACTED]") {
		t.Errorf("expected [REDACTED] in log output: %s", buf.String())
	}
}

func TestSecret_ExposeReturnsValue(t *testing.T) {
	s := NewSecret("actual-value")
	if s.Expose() != "actual-value" {
		t.Errorf("Expose() returned %q, want %q", s.Expose(), "actual-value")
	}
}

func TestSecret_IsEmpty(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty string", "", true},
		{"non-empty string", "value", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSecret(tt.input)
			if s.IsEmpty() != tt.want {
				t.Errorf("IsEmpty() = %v, want %v", s.IsEmpty(), tt.want)
			}
		})
	}
}
