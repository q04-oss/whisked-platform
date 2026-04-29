package loyalty

import (
	"context"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

// mockRepository satisfies the loyalty repository interface for unit tests.
type mockRepository struct {
	events     []insertEventParams
	customers  map[string]platform.CustomerID // email → id
	redeemErr  error
}

func newMockRepo() *mockRepository {
	return &mockRepository{
		customers: make(map[string]platform.CustomerID),
	}
}

func (m *mockRepository) getBalanceRow(_ context.Context, customerID platform.CustomerID) (balanceRow, error) {
	var earned, redeemed int64
	for _, e := range m.events {
		if e.customerID != customerID {
			continue
		}
		switch e.eventType {
		case "steep_earned":
			earned++
		case "reward_redeemed":
			redeemed++
		}
	}
	return balanceRow{steepsEarned: earned, rewardsRedeemed: redeemed}, nil
}

func (m *mockRepository) insertEvent(_ context.Context, p insertEventParams) (*Event, error) {
	// Check for duplicate idempotency key.
	for _, e := range m.events {
		if e.idempotencyKey != "" && e.idempotencyKey == p.idempotencyKey {
			return nil, fmt.Errorf("unique constraint idempotency_key violation")
		}
	}
	m.events = append(m.events, p)
	return &Event{EventType: p.eventType, Source: p.source}, nil
}

func (m *mockRepository) redeemInTransaction(_ context.Context, customerID platform.CustomerID, idemKey string) error {
	if m.redeemErr != nil {
		return m.redeemErr
	}
	var earned, redeemed int64
	for _, e := range m.events {
		if e.customerID != customerID {
			continue
		}
		switch e.eventType {
		case "steep_earned":
			earned++
		case "reward_redeemed":
			redeemed++
		}
	}
	available := (earned / SteepsPerReward) - redeemed
	if available <= 0 {
		return platform.Wrap(platform.ErrBadRequest, fmt.Errorf("no rewards available"))
	}
	m.events = append(m.events, insertEventParams{
		customerID:     customerID,
		eventType:      "reward_redeemed",
		source:         "in_bar",
		idempotencyKey: idemKey,
	})
	return nil
}

func (m *mockRepository) getHistory(_ context.Context, customerID platform.CustomerID, limit, offset int) ([]Event, error) {
	var events []Event
	for _, e := range m.events {
		if e.customerID == customerID {
			events = append(events, Event{EventType: e.eventType, Source: e.source})
		}
	}
	end := offset + limit
	if offset >= len(events) {
		return []Event{}, nil
	}
	if end > len(events) {
		end = len(events)
	}
	return events[offset:end], nil
}

func (m *mockRepository) getCustomerIDByEmail(_ context.Context, email string) (platform.CustomerID, error) {
	id, ok := m.customers[email]
	if !ok {
		return 0, platform.ErrNotFound
	}
	return id, nil
}

func testService(t *testing.T) (*Service, *mockRepository) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	repo := newMockRepo()
	return NewService(repo, rdb, audit.New(nil)), repo
}

func newKey() string { return uuid.NewString() }

// earnN earns n steeps for the customer, using a unique idempotency key each time.
func earnN(t *testing.T, svc *Service, customerID platform.CustomerID, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		_, err := svc.Earn(context.Background(), EarnParams{
			CustomerID:     customerID,
			Source:         "in_bar",
			IdempotencyKey: newKey(),
		})
		if err != nil {
			t.Fatalf("earn %d: %v", i, err)
		}
	}
}

// ── Balance computation ───────────────────────────────────────────────────────

func TestComputeBalance(t *testing.T) {
	tests := []struct {
		name            string
		steepsEarned    int64
		rewardsRedeemed int64
		wantAvailable   int64
		wantProgress    int64
		wantUntil       int64
	}{
		{"zero", 0, 0, 0, 0, 9},
		{"8 steeps — not yet at threshold", 8, 0, 0, 8, 1},
		{"9 steeps — one reward available", 9, 0, 1, 0, 9},
		{"9 steeps — reward already redeemed", 9, 1, 0, 0, 9},
		{"18 steeps — two rewards available", 18, 0, 2, 0, 9},
		{"18 steeps — one redeemed, one available", 18, 1, 1, 0, 9},
		{"10 steeps — one available, 1 progress", 10, 0, 1, 1, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := computeBalance(platform.CustomerID(1), tt.steepsEarned, tt.rewardsRedeemed)
			if b.Available != tt.wantAvailable {
				t.Errorf("Available = %d, want %d", b.Available, tt.wantAvailable)
			}
			if b.Progress != tt.wantProgress {
				t.Errorf("Progress = %d, want %d", b.Progress, tt.wantProgress)
			}
			if b.Until != tt.wantUntil {
				t.Errorf("Until = %d, want %d", b.Until, tt.wantUntil)
			}
		})
	}
}

// ── Earn ──────────────────────────────────────────────────────────────────────

func TestService_Earn_RecordsSteep(t *testing.T) {
	svc, repo := testService(t)
	customerID := platform.CustomerID(1)

	balance, err := svc.Earn(context.Background(), EarnParams{
		CustomerID:     customerID,
		Source:         "in_bar",
		IdempotencyKey: newKey(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if balance.SteepsEarned != 1 {
		t.Errorf("SteepsEarned = %d, want 1", balance.SteepsEarned)
	}
	if len(repo.events) != 1 {
		t.Errorf("events count = %d, want 1", len(repo.events))
	}
}

func TestService_Earn_IdempotentOnRetry(t *testing.T) {
	svc, repo := testService(t)
	customerID := platform.CustomerID(1)
	key := newKey()

	params := EarnParams{CustomerID: customerID, Source: "in_bar", IdempotencyKey: key}

	if _, err := svc.Earn(context.Background(), params); err != nil {
		t.Fatalf("first earn: %v", err)
	}
	if _, err := svc.Earn(context.Background(), params); err != nil {
		t.Fatalf("retry earn: %v", err)
	}

	// Only one event should be recorded.
	if len(repo.events) != 1 {
		t.Errorf("expected 1 event after retry, got %d", len(repo.events))
	}
}

func TestService_Earn_MissingIdempotencyKey(t *testing.T) {
	svc, _ := testService(t)

	_, err := svc.Earn(context.Background(), EarnParams{
		CustomerID: platform.CustomerID(1),
		Source:     "in_bar",
	})
	if err == nil {
		t.Fatal("expected error for missing idempotency key")
	}
	ce, ok := platform.AsClientError(err)
	if !ok || ce.HTTPStatus != 400 {
		t.Errorf("expected 400 ClientError, got %v", err)
	}
}

func TestService_Earn_InvalidIdempotencyKey(t *testing.T) {
	svc, _ := testService(t)

	_, err := svc.Earn(context.Background(), EarnParams{
		CustomerID:     platform.CustomerID(1),
		Source:         "in_bar",
		IdempotencyKey: "not-a-uuid",
	})
	if err == nil {
		t.Fatal("expected error for non-UUID idempotency key")
	}
}

// ── Redeem ────────────────────────────────────────────────────────────────────

func TestService_Redeem_Success(t *testing.T) {
	svc, _ := testService(t)
	customerID := platform.CustomerID(1)

	earnN(t, svc, customerID, SteepsPerReward)

	balance, err := svc.Redeem(context.Background(), customerID)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if balance.Available != 0 {
		t.Errorf("Available = %d, want 0 after redemption", balance.Available)
	}
	if balance.RewardsRedeemed != 1 {
		t.Errorf("RewardsRedeemed = %d, want 1", balance.RewardsRedeemed)
	}
}

func TestService_Redeem_InsufficientSteeps(t *testing.T) {
	svc, _ := testService(t)
	customerID := platform.CustomerID(1)

	earnN(t, svc, customerID, SteepsPerReward-1)

	_, err := svc.Redeem(context.Background(), customerID)
	if err == nil {
		t.Fatal("expected error for insufficient steeps")
	}
	ce, ok := platform.AsClientError(err)
	if !ok || ce.HTTPStatus != 400 {
		t.Errorf("expected 400 ClientError, got %v", err)
	}
}

func TestService_Redeem_CannotExceedAvailable(t *testing.T) {
	svc, _ := testService(t)
	customerID := platform.CustomerID(1)

	// Earn exactly one reward.
	earnN(t, svc, customerID, SteepsPerReward)

	// Redeem it.
	if _, err := svc.Redeem(context.Background(), customerID); err != nil {
		t.Fatalf("first redeem: %v", err)
	}

	// Second redemption must fail — no rewards available.
	_, err := svc.Redeem(context.Background(), customerID)
	if err == nil {
		t.Fatal("expected error on second redemption with no rewards available")
	}
}

// ── Shopify ───────────────────────────────────────────────────────────────────

func TestService_ProcessShopifyOrder_KnownCustomer(t *testing.T) {
	svc, repo := testService(t)
	customerID := platform.CustomerID(1)
	repo.customers["belle@whisked.ca"] = customerID

	err := svc.ProcessShopifyOrder(context.Background(), "shopify-order-123", "belle@whisked.ca")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	balance, _ := svc.GetBalance(context.Background(), customerID)
	if balance.SteepsEarned != 1 {
		t.Errorf("SteepsEarned = %d, want 1", balance.SteepsEarned)
	}
}

func TestService_ProcessShopifyOrder_UnknownCustomer(t *testing.T) {
	svc, _ := testService(t)

	// Should not error — no customer account, no loyalty credit, but not an error.
	err := svc.ProcessShopifyOrder(context.Background(), "order-456", "unknown@example.com")
	if err != nil {
		t.Errorf("unexpected error for unknown customer: %v", err)
	}
}

func TestService_ProcessShopifyOrder_Idempotent(t *testing.T) {
	svc, repo := testService(t)
	customerID := platform.CustomerID(1)
	repo.customers["belle@whisked.ca"] = customerID

	// Same order ID twice.
	svc.ProcessShopifyOrder(context.Background(), "order-789", "belle@whisked.ca")
	svc.ProcessShopifyOrder(context.Background(), "order-789", "belle@whisked.ca")

	balance, _ := svc.GetBalance(context.Background(), customerID)
	if balance.SteepsEarned != 1 {
		t.Errorf("SteepsEarned = %d, want 1 (idempotent)", balance.SteepsEarned)
	}
}
