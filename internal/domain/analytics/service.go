package analytics

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

// repository is the interface Service requires from its data layer.
type repository interface {
	upsertVisitor(ctx context.Context, visitorID platform.AnonymousVisitorID) error
	insertEvent(ctx context.Context, p insertEventParams) error
	linkVisitor(ctx context.Context, visitorID platform.AnonymousVisitorID, customerID platform.CustomerID) error
	getVisitorLink(ctx context.Context, visitorID platform.AnonymousVisitorID) (*platform.CustomerID, error)
}

// Service handles behavioral event ingestion and visitor identity management.
type Service struct {
	repo  repository
	audit *audit.Writer
}

// NewService returns a Service wired to its dependencies.
func NewService(repo repository, audit *audit.Writer) *Service {
	return &Service{repo: repo, audit: audit}
}

// Ingest records a behavioral event. Returns IngestResult indicating whether
// the event was tracked — DNT clients receive {tracked: false} without error.
func (s *Service) Ingest(ctx context.Context, params IngestParams, dnt bool) (IngestResult, error) {
	if dnt {
		return IngestResult{Tracked: false}, nil
	}

	if err := validateIngestParams(params); err != nil {
		return IngestResult{}, err
	}

	// If a visitor ID is present, ensure the visitor row exists before inserting
	// the event (FK constraint). Upsert is safe — last_seen is always current.
	if params.VisitorID != nil {
		if err := s.repo.upsertVisitor(ctx, *params.VisitorID); err != nil {
			return IngestResult{}, fmt.Errorf("analytics.Ingest: upserting visitor: %w", err)
		}
	}

	err := s.repo.insertEvent(ctx, insertEventParams{
		eventType:  params.EventType,
		customerID: params.CustomerID,
		visitorID:  params.VisitorID,
		sessionID:  nullableString(params.SessionID),
		page:       nullableString(params.Page),
		metadata:   params.Metadata,
		ipAddress:  nullableString(params.IPAddress),
		userAgent:  nullableString(params.UserAgent),
	})
	if err != nil {
		return IngestResult{}, fmt.Errorf("analytics.Ingest: inserting event: %w", err)
	}

	return IngestResult{Tracked: true}, nil
}

// LinkVisitor associates an anonymous visitor's behavioral history with their
// identified customer account. Idempotent — safe to call multiple times.
// Fires a visitor.identified analytic event on first successful link.
func (s *Service) LinkVisitor(ctx context.Context, visitorID platform.AnonymousVisitorID, customerID platform.CustomerID) error {
	existing, err := s.repo.getVisitorLink(ctx, visitorID)
	if err != nil {
		return fmt.Errorf("analytics.LinkVisitor: checking existing link: %w", err)
	}
	if existing != nil {
		// Already linked — idempotent success.
		return nil
	}

	// Upsert the visitor row in case they haven't sent any events yet.
	if err := s.repo.upsertVisitor(ctx, visitorID); err != nil {
		return fmt.Errorf("analytics.LinkVisitor: upserting visitor: %w", err)
	}

	if err := s.repo.linkVisitor(ctx, visitorID, customerID); err != nil {
		return fmt.Errorf("analytics.LinkVisitor: linking: %w", err)
	}

	// Record the identification event in the analytic log.
	s.repo.insertEvent(ctx, insertEventParams{
		eventType:  EventVisitorIdentified,
		customerID: &customerID,
		visitorID:  &visitorID,
	})

	s.audit.Write(ctx, audit.Entry{
		EventType:  audit.EventCustomerRead, // closest sentinel — visitor identification
		ActorType:  audit.ActorCustomer,
		ActorID:    ptr(customerID.Int64()),
		TargetType: ptr("visitor"),
		TargetID:   ptr(visitorID.String()),
		Metadata:   map[string]string{"action": "visitor_identified"},
	})

	return nil
}

// ── Validation ────────────────────────────────────────────────────────────────

func validateIngestParams(p IngestParams) error {
	if p.EventType == "" {
		return platform.Wrap(platform.ErrBadRequest, fmt.Errorf("event_type is required"))
	}
	if !knownEventTypes[p.EventType] {
		return platform.Wrap(platform.ErrBadRequest, fmt.Errorf("unknown event_type: %q", p.EventType))
	}
	if p.VisitorID == nil && p.CustomerID == nil {
		return platform.Wrap(platform.ErrBadRequest, fmt.Errorf("visitor_id or customer_id is required"))
	}
	if p.VisitorID != nil {
		if _, err := uuid.Parse(p.VisitorID.String()); err != nil {
			return platform.Wrap(platform.ErrBadRequest, fmt.Errorf("visitor_id must be a UUID"))
		}
	}
	return nil
}

func nullableString(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func ptr[T any](v T) *T { return &v }
