// Command seed bootstraps the first staff account in a fresh Whisked deployment.
//
// Run once after applying migrations to create the initial admin user.
// Subsequent runs with the same email are safe — the command is idempotent.
//
// Usage:
//
//	DATABASE_URL=postgres://... go run ./cmd/seed \
//	  -email=admin@whisked.ca \
//	  -name="Belle" \
//	  -password=yourpassword \
//	  -role=admin
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/q04-oss/whisked-platform/internal/domain/auth"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if err := run(); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		email       = flag.String("email", "", "Staff email address (required)")
		displayName = flag.String("name", "", "Display name (required)")
		password    = flag.String("password", "", "Password — min 8 characters (required)")
		role        = flag.String("role", "admin", "Role: admin or viewer")
	)
	flag.Parse()

	if err := validateFlags(*email, *displayName, *password, *role); err != nil {
		flag.Usage()
		return err
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("pinging database: %w", err)
	}

	email := strings.ToLower(strings.TrimSpace(*email))

	// Check if the staff account already exists — idempotent.
	var existingID int64
	err = db.QueryRow(ctx, `SELECT id FROM staff WHERE email = $1`, email).Scan(&existingID)
	if err == nil {
		slog.Info("staff account already exists — no changes made", "email", email, "id", existingID)
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("checking for existing staff: %w", err)
	}

	hash, err := auth.HashPassword(*password)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	var id int64
	err = db.QueryRow(ctx,
		`INSERT INTO staff (email, display_name, role, password_hash)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		email,
		strings.TrimSpace(*displayName),
		*role,
		hash,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("creating staff account: %w", err)
	}

	slog.Info("staff account created",
		"id", id,
		"email", email,
		"role", *role,
	)
	return nil
}

func validateFlags(email, displayName, password, role string) error {
	if strings.TrimSpace(email) == "" {
		return fmt.Errorf("-email is required")
	}
	if !strings.Contains(email, "@") {
		return fmt.Errorf("-email is not a valid email address")
	}
	if strings.TrimSpace(displayName) == "" {
		return fmt.Errorf("-name is required")
	}
	if len(password) < 8 {
		return fmt.Errorf("-password must be at least 8 characters")
	}
	if role != "admin" && role != "viewer" {
		return fmt.Errorf("-role must be 'admin' or 'viewer'")
	}
	return nil
}
