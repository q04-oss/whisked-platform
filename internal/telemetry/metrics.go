package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds the Prometheus instruments for the whisked-platform API.
// Organized into HTTP infrastructure metrics and business domain metrics.
//
// Business metrics are defined here even when their cardinality is low — it
// keeps all metric definitions in one place and makes the dashboard query
// surface discoverable.
type Metrics struct {
	// HTTP infrastructure — RED metrics (Rate, Errors, Duration).
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	ActiveRequests  prometheus.Gauge

	// Security events — tracked separately from HTTP errors for alerting.
	AuthFailures    *prometheus.CounterVec // labels: reason
	HMACFailures    *prometheus.CounterVec // labels: reason
	RateLimitHits   *prometheus.CounterVec // labels: limiter_type

	// Business domain — loyalty.
	SteepsEarned    *prometheus.CounterVec // labels: source (in_bar, shopify)
	RewardsRedeemed prometheus.Counter

	// Business domain — identity.
	VisitorIdentifications prometheus.Counter // anonymous → identified links
}

func newMetrics(reg *prometheus.Registry) (*Metrics, error) {
	m := &Metrics{
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "whisked_http_requests_total",
			Help: "Total HTTP requests by method, path pattern, and status code.",
		}, []string{"method", "path", "status"}),

		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "whisked_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path"}),

		ActiveRequests: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "whisked_http_active_requests",
			Help: "Number of in-flight HTTP requests.",
		}),

		AuthFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "whisked_auth_failures_total",
			Help: "Authentication failures by reason.",
		}, []string{"reason"}),

		HMACFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "whisked_hmac_failures_total",
			Help: "HMAC validation failures by reason.",
		}, []string{"reason"}),

		RateLimitHits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "whisked_rate_limit_hits_total",
			Help: "Rate limit rejections by limiter type (ip, user).",
		}, []string{"limiter_type"}),

		SteepsEarned: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "whisked_steeps_earned_total",
			Help: "Steeps earned by source channel.",
		}, []string{"source"}),

		RewardsRedeemed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "whisked_rewards_redeemed_total",
			Help: "Total loyalty rewards redeemed.",
		}),

		VisitorIdentifications: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "whisked_visitor_identifications_total",
			Help: "Anonymous visitors linked to identified customer accounts.",
		}),
	}

	collectors := []prometheus.Collector{
		m.RequestsTotal,
		m.RequestDuration,
		m.ActiveRequests,
		m.AuthFailures,
		m.HMACFailures,
		m.RateLimitHits,
		m.SteepsEarned,
		m.RewardsRedeemed,
		m.VisitorIdentifications,
	}

	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}

	return m, nil
}
