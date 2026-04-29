// Package telemetry initializes OpenTelemetry distributed tracing and
// Prometheus metrics instrumentation. Both are wired up at startup and
// injected into the HTTP middleware chain.
//
// Tracing uses OTLP HTTP export. If OTEL_EXPORTER_OTLP_ENDPOINT is not set,
// a no-op tracer is used — traces are silently discarded. This makes local
// development work without an observability backend.
//
// Metrics are exposed on a /metrics endpoint using the Prometheus text format.
// The registry is separate from prometheus.DefaultRegisterer so application
// metrics are isolated from any library that registers with the default.
package telemetry

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Config holds the telemetry configuration loaded from environment variables.
type Config struct {
	ServiceName    string
	ServiceVersion string
	// OTLPEndpoint is the OTLP HTTP endpoint for trace export.
	// Empty string disables tracing (no-op tracer used instead).
	OTLPEndpoint string
}

// Provider owns the tracer provider and Prometheus registry.
// Call Shutdown when the server stops to flush pending spans.
type Provider struct {
	tracer   trace.TracerProvider
	tp       *sdktrace.TracerProvider // nil when using no-op
	registry *prometheus.Registry
	Metrics  *Metrics
}

// Tracer returns a named tracer from this provider.
func (p *Provider) Tracer(name string) trace.Tracer {
	return p.tracer.Tracer(name)
}

// MetricsHandler returns an HTTP handler that serves Prometheus metrics.
func (p *Provider) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// Shutdown flushes and closes the tracer provider. Call on server shutdown.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.tp != nil {
		return p.tp.Shutdown(ctx)
	}
	return nil
}

// New initializes telemetry. If OTLPEndpoint is empty, a no-op tracer is
// used and no external connections are made.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	tp, tracerProvider, err := buildTracerProvider(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("building tracer provider: %w", err)
	}
	otel.SetTracerProvider(tracerProvider)

	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	metrics, err := newMetrics(registry)
	if err != nil {
		return nil, fmt.Errorf("registering metrics: %w", err)
	}

	return &Provider{
		tracer:   tracerProvider,
		tp:       tp,
		registry: registry,
		Metrics:  metrics,
	}, nil
}

func buildTracerProvider(ctx context.Context, cfg Config) (*sdktrace.TracerProvider, trace.TracerProvider, error) {
	if cfg.OTLPEndpoint == "" {
		return nil, noop.NewTracerProvider(), nil
	}

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(cfg.OTLPEndpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("creating OTLP exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("creating OTel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	return tp, tp, nil
}
