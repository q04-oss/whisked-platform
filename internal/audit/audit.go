// Package audit writes structured records of security-relevant events to the
// append-only audit_log table. Every authentication event, authorization
// decision, admin action, payment event, and customer data access pattern
// produces an audit record.
//
// Audit records are written to PostgreSQL, not just stdout, so they are
// durable, queryable, and retained per privacy policy. The audit_log table
// is protected against UPDATE and DELETE at the database level.
//
// Writers call audit.Write and move on — the write is best-effort and does
// not affect the response. Audit write failures are logged as errors but
// never returned to the client or used to reject a request.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ActorType identifies the category of actor that produced the event.
type ActorType string

const (
	ActorCustomer ActorType = "customer"
	ActorStaff    ActorType = "staff"
	ActorSystem   ActorType = "system"
)

// EventType is the action recorded in the audit log.
// Values are kebab-case strings that read naturally in queries and alerts.
type EventType string

const (
	// Authentication
	EventAuthLogin          EventType = "auth.login"
	EventAuthLoginFailed    EventType = "auth.login_failed"
	EventAuthLogout         EventType = "auth.logout"
	EventAuthTokenRefresh   EventType = "auth.token_refresh"
	EventAuthTokenRevoked   EventType = "auth.token_revoked"

	// Customer data access
	EventCustomerRead       EventType = "customer.read"
	EventCustomerUpdated    EventType = "customer.updated"
	EventCustomerDeleted    EventType = "customer.deleted"

	// Loyalty
	EventSteepEarned        EventType = "loyalty.steep_earned"
	EventRewardRedeemed     EventType = "loyalty.reward_redeemed"
	EventReplayRejected     EventType = "loyalty.replay_rejected"

	// Admin
	EventAdminAction        EventType = "admin.action"

	// Security
	EventHMACFailure        EventType = "security.hmac_failure"
	EventRateLimitExceeded  EventType = "security.rate_limit_exceeded"
	EventWebhookReceived    EventType = "webhook.received"
	EventWebhookRejected    EventType = "webhook.rejected"
)

// Entry is a single audit log record.
type Entry struct {
	EventType  EventType
	ActorID    *int64     // nil for unauthenticated events
	ActorType  ActorType
	TargetID   *string    // ID of the resource being acted on
	TargetType *string    // type name of the resource
	IPAddress  *string
	RequestID  *string
	Metadata   any        // arbitrary structured data, serialized to JSONB
}

// Writer writes audit entries to the database.
type Writer struct {
	db *pgxpool.Pool
}

// New returns a Writer that persists audit entries to the given pool.
func New(db *pgxpool.Pool) *Writer {
	return &Writer{db: db}
}

// Write persists an audit entry. Failures are logged but never propagated —
// an audit write failure must not affect the outcome of the request it records.
func (w *Writer) Write(ctx context.Context, e Entry) {
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		slog.ErrorContext(ctx, "audit: failed to marshal metadata",
			"event_type", e.EventType,
			"error", err,
		)
		meta = []byte("null")
	}

	_, err = w.db.Exec(ctx,
		`INSERT INTO audit_log
			(event_type, actor_id, actor_type, target_id, target_type,
			 ip_address, request_id, metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		string(e.EventType),
		e.ActorID,
		string(e.ActorType),
		e.TargetID,
		e.TargetType,
		e.IPAddress,
		e.RequestID,
		meta,
		time.Now().UTC(),
	)
	if err != nil {
		slog.ErrorContext(ctx, "audit: failed to write entry",
			"event_type", e.EventType,
			"error", err,
		)
	}
}

// FromRequest populates request-scoped fields from an HTTP request.
// Use this to avoid repeating IP and request ID extraction in every handler.
func FromRequest(r *http.Request) (ip *string, requestID *string) {
	raw := r.Header.Get("X-Forwarded-For")
	if raw == "" {
		raw = r.RemoteAddr
	}
	if raw != "" {
		ip = &raw
	}
	rid := r.Header.Get("X-Request-ID")
	if rid != "" {
		requestID = &rid
	}
	return ip, requestID
}
