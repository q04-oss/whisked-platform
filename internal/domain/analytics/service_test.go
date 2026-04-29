package analytics

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

// mockRepository satisfies the analytics repository interface for unit tests.
type mockRepository struct {
	events   []insertEventParams
	visitors map[string]*platform.CustomerID // visitorID → linked customerID
}

func newMockRepo() *mockRepository {
	return &mockRepository{
		visitors: make(map[string]*platform.CustomerID),
	}
}

func (m *mockRepository) upsertVisitor(_ context.Context, visitorID platform.AnonymousVisitorID) error {
	if _, ok := m.visitors[visitorID.String()]; !ok {
		m.visitors[visitorID.String()] = nil
	}
	return nil
}

func (m *mockRepository) insertEvent(_ context.Context, p insertEventParams) error {
	m.events = append(m.events, p)
	return nil
}

func (m *mockRepository) linkVisitor(_ context.Context, visitorID platform.AnonymousVisitorID, customerID platform.CustomerID) error {
	m.visitors[visitorID.String()] = &customerID
	return nil
}

func (m *mockRepository) getVisitorLink(_ context.Context, visitorID platform.AnonymousVisitorID) (*platform.CustomerID, error) {
	cid, ok := m.visitors[visitorID.String()]
	if !ok {
		return nil, nil
	}
	return cid, nil
}

func testService(t *testing.T) (*Service, *mockRepository) {
	t.Helper()
	repo := newMockRepo()
	return NewService(repo, audit.New(nil)), repo
}

func newVisitorID() platform.AnonymousVisitorID {
	return platform.AnonymousVisitorID(uuid.NewString())
}

// ── Ingest ────────────────────────────────────────────────────────────────────

func TestService_Ingest_StoresEvent(t *testing.T) {
	svc, repo := testService(t)
	vid := newVisitorID()

	result, err := svc.Ingest(context.Background(), IngestParams{
		EventType: EventPageViewed,
		VisitorID: &vid,
		Page:      "/menu",
	}, false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Tracked {
		t.Error("expected Tracked = true")
	}
	if len(repo.events) != 1 {
		t.Errorf("events = %d, want 1", len(repo.events))
	}
	if repo.events[0].eventType != EventPageViewed {
		t.Errorf("event_type = %q, want %q", repo.events[0].eventType, EventPageViewed)
	}
}

func TestService_Ingest_DNTNotTracked(t *testing.T) {
	svc, repo := testService(t)
	vid := newVisitorID()

	result, err := svc.Ingest(context.Background(), IngestParams{
		EventType: EventPageViewed,
		VisitorID: &vid,
	}, true /* dnt = true */)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Tracked {
		t.Error("expected Tracked = false for DNT request")
	}
	if len(repo.events) != 0 {
		t.Errorf("expected no events stored for DNT request, got %d", len(repo.events))
	}
}

func TestService_Ingest_UnknownEventTypeRejected(t *testing.T) {
	svc, _ := testService(t)
	vid := newVisitorID()

	_, err := svc.Ingest(context.Background(), IngestParams{
		EventType: "made.up.event",
		VisitorID: &vid,
	}, false)

	if err == nil {
		t.Fatal("expected error for unknown event type")
	}
	ce, ok := platform.AsClientError(err)
	if !ok || ce.HTTPStatus != 400 {
		t.Errorf("expected 400 ClientError, got %v", err)
	}
}

func TestService_Ingest_RequiresVisitorOrCustomer(t *testing.T) {
	svc, _ := testService(t)

	_, err := svc.Ingest(context.Background(), IngestParams{
		EventType: EventPageViewed,
		// Neither VisitorID nor CustomerID set.
	}, false)

	if err == nil {
		t.Fatal("expected error when neither visitor_id nor customer_id is provided")
	}
	ce, ok := platform.AsClientError(err)
	if !ok || ce.HTTPStatus != 400 {
		t.Errorf("expected 400 ClientError, got %v", err)
	}
}

func TestService_Ingest_InvalidVisitorID(t *testing.T) {
	svc, _ := testService(t)
	badID := platform.AnonymousVisitorID("not-a-uuid; DROP TABLE analytic_events; --")

	_, err := svc.Ingest(context.Background(), IngestParams{
		EventType: EventPageViewed,
		VisitorID: &badID,
	}, false)

	if err == nil {
		t.Fatal("expected error for invalid visitor ID")
	}
	ce, ok := platform.AsClientError(err)
	if !ok || ce.HTTPStatus != 400 {
		t.Errorf("expected 400 ClientError, got %v", err)
	}
}

func TestService_Ingest_IdentifiedCustomer(t *testing.T) {
	svc, repo := testService(t)
	customerID := platform.CustomerID(42)

	result, err := svc.Ingest(context.Background(), IngestParams{
		EventType:  EventAppOpened,
		CustomerID: &customerID,
	}, false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Tracked {
		t.Error("expected Tracked = true")
	}
	if repo.events[0].customerID == nil || *repo.events[0].customerID != customerID {
		t.Error("customer ID not stored correctly")
	}
}

// ── LinkVisitor ───────────────────────────────────────────────────────────────

func TestService_LinkVisitor_LinksHistory(t *testing.T) {
	svc, repo := testService(t)
	visitorID := newVisitorID()
	customerID := platform.CustomerID(1)

	if err := svc.LinkVisitor(context.Background(), visitorID, customerID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	link := repo.visitors[visitorID.String()]
	if link == nil || *link != customerID {
		t.Error("visitor was not linked to customer")
	}
}

func TestService_LinkVisitor_FiresIdentifiedEvent(t *testing.T) {
	svc, repo := testService(t)
	visitorID := newVisitorID()
	customerID := platform.CustomerID(1)

	svc.LinkVisitor(context.Background(), visitorID, customerID)

	var found bool
	for _, e := range repo.events {
		if e.eventType == EventVisitorIdentified {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected visitor.identified event after linking")
	}
}

func TestService_LinkVisitor_Idempotent(t *testing.T) {
	svc, repo := testService(t)
	visitorID := newVisitorID()
	customerID := platform.CustomerID(1)

	svc.LinkVisitor(context.Background(), visitorID, customerID)
	svc.LinkVisitor(context.Background(), visitorID, customerID)

	// Only one identified event should be fired.
	var count int
	for _, e := range repo.events {
		if e.eventType == EventVisitorIdentified {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 visitor.identified event, got %d", count)
	}
}
