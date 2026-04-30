package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	migrations "github.com/q04-oss/whisked-platform/db"
	"github.com/q04-oss/whisked-platform/internal/app"
	"github.com/q04-oss/whisked-platform/internal/config"
	"github.com/q04-oss/whisked-platform/internal/db"
	redisclient "github.com/q04-oss/whisked-platform/internal/redis"
	"github.com/q04-oss/whisked-platform/internal/server"
	"github.com/q04-oss/whisked-platform/internal/telemetry"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// ── Config ─────────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	slog.Info("config loaded", "port", cfg.Port)

	// ── Telemetry ──────────────────────────────────────────────────────────────
	tel, err := telemetry.New(ctx, telemetry.Config{
		ServiceName:    "whisked-api",
		ServiceVersion: "0.1.0",
		OTLPEndpoint:   os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	})
	if err != nil {
		return fmt.Errorf("initializing telemetry: %w", err)
	}
	defer tel.Shutdown(context.Background())
	slog.Info("telemetry initialized")

	// ── Database ───────────────────────────────────────────────────────────────
	pool, err := db.Connect(ctx, cfg.DatabaseURL.Expose())
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()
	slog.Info("database connected")

	// ── Migrations ─────────────────────────────────────────────────────────────
	// Run on every startup. ErrNoChange is treated as success — safe to call
	// multiple times. Migrations are embedded in the binary so no filesystem
	// access is needed in production.
	if err := db.Migrate(pool, migrations.FS); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}
	slog.Info("migrations applied")

	// ── Redis ──────────────────────────────────────────────────────────────────
	rdb, err := redisclient.Connect(ctx, cfg.RedisURL.Expose())
	if err != nil {
		return fmt.Errorf("connecting to Redis: %w", err)
	}
	defer rdb.Close()
	slog.Info("redis connected")

	// ── State ──────────────────────────────────────────────────────────────────
	state, err := app.NewState(ctx, pool, rdb, cfg, tel)
	if err != nil {
		return fmt.Errorf("initializing state: %w", err)
	}

	// ── Server ─────────────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: server.New(state),

		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		slog.Info("shutting down gracefully")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		slog.Info("shutdown complete")
		return nil
	}
}
