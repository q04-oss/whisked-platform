package dashboard

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/config"
	"github.com/q04-oss/whisked-platform/internal/domain/auth"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

type mockRepository struct {
	staff    *staffRow
	overview *overviewRow
	funnel   *funnelRow
}

func (m *mockRepository) getOverview(_ context.Context) (*overviewRow, error) {
	if m.overview != nil {
		return m.overview, nil
	}
	return &overviewRow{}, nil
}

func (m *mockRepository) getSteepsByDay(_ context.Context, _ int) ([]dailySteepsRow, error) {
	return nil, nil
}

func (m *mockRepository) getRecentLoyaltyEvents(_ context.Context, _ int) ([]recentEventRow, error) {
	return nil, nil
}

func (m *mockRepository) getFunnelStats(_ context.Context) (*funnelRow, error) {
	if m.funnel != nil {
		return m.funnel, nil
	}
	return &funnelRow{}, nil
}

func (m *mockRepository) getCustomerList(_ context.Context, _, _ int) ([]customerListRow, error) {
	return nil, nil
}

func (m *mockRepository) getStaffByEmail(_ context.Context, email string) (*staffRow, error) {
	if m.staff != nil && m.staff.email == email {
		return m.staff, nil
	}
	return nil, nil
}

func testService(t *testing.T) (*Service, *mockRepository) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	repo := &mockRepository{}
	cfg := &config.Config{}
	cfg.JWTSecret = config.NewSecret("test-jwt-secret-that-is-long-enough-32chars")
	return NewService(repo, rdb, cfg, audit.New(nil)), repo
}

// ── Login ─────────────────────────────────────────────────────────────────────

func TestService_Login_Success(t *testing.T) {
	svc, repo := testService(t)

	hash, err := auth.HashPassword("staffpassword1")
	if err != nil {
		t.Fatalf("hashing password: %v", err)
	}
	repo.staff = &staffRow{
		id:           1,
		email:        "belle@whisked.ca",
		role:         "admin",
		passwordHash: hash,
		active:       true,
	}

	pair, err := svc.Login(context.Background(), "belle@whisked.ca", "staffpassword1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if pair.Role != "admin" {
		t.Errorf("role = %q, want %q", pair.Role, "admin")
	}
}

func TestService_Login_WrongPassword(t *testing.T) {
	svc, repo := testService(t)

	hash, _ := auth.HashPassword("correctpassword")
	repo.staff = &staffRow{
		id:           1,
		email:        "belle@whisked.ca",
		passwordHash: hash,
		active:       true,
	}

	_, err := svc.Login(context.Background(), "belle@whisked.ca", "wrongpassword")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	ce, ok := platform.AsClientError(err)
	if !ok || ce.HTTPStatus != 401 {
		t.Errorf("expected 401, got %v", err)
	}
}

func TestService_Login_InactiveAccount(t *testing.T) {
	svc, repo := testService(t)

	hash, _ := auth.HashPassword("password123")
	repo.staff = &staffRow{
		id:           1,
		email:        "inactive@whisked.ca",
		passwordHash: hash,
		active:       false, // deactivated
	}

	_, err := svc.Login(context.Background(), "inactive@whisked.ca", "password123")
	if err == nil {
		t.Fatal("expected error for inactive account")
	}
}

func TestService_Login_UnknownEmail(t *testing.T) {
	svc, _ := testService(t)

	_, err := svc.Login(context.Background(), "nobody@whisked.ca", "anypassword")
	if err == nil {
		t.Fatal("expected error for unknown email")
	}
	ce, ok := platform.AsClientError(err)
	if !ok || ce.HTTPStatus != 401 {
		t.Errorf("expected 401, got %v", err)
	}
}

// ── Funnel stats ──────────────────────────────────────────────────────────────

func TestService_GetFunnelStats_SmallSampleFlagged(t *testing.T) {
	svc, repo := testService(t)
	repo.funnel = &funnelRow{
		totalVisitors:  10, // n < 30
		totalCustomers: 5,
	}

	stats, err := svc.GetFunnelStats(context.Background(), platform.StaffID(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stats.SmallSample {
		t.Error("expected SmallSample = true for n < 30")
	}
}

func TestService_GetFunnelStats_RatesComputed(t *testing.T) {
	svc, repo := testService(t)
	repo.funnel = &funnelRow{
		totalVisitors:       100,
		identifiedVisitors:  40,
		totalCustomers:      40,
		customersWithSteeps: 30,
		repeatCustomers:     15,
	}

	stats, err := svc.GetFunnelStats(context.Background(), platform.StaffID(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.IdentificationRate != 0.4 {
		t.Errorf("IdentificationRate = %v, want 0.4", stats.IdentificationRate)
	}
	if stats.FirstSteepRate != 0.75 {
		t.Errorf("FirstSteepRate = %v, want 0.75", stats.FirstSteepRate)
	}
	if stats.RepeatRate != 0.5 {
		t.Errorf("RepeatRate = %v, want 0.5", stats.RepeatRate)
	}
	if stats.SmallSample {
		t.Error("expected SmallSample = false for n >= 30")
	}
}

// ── Overview ──────────────────────────────────────────────────────────────────

func TestService_GetOverview_RedemptionRateComputed(t *testing.T) {
	svc, repo := testService(t)
	repo.overview = &overviewRow{
		totalSteepsEarned:    18, // 2 eligible rewards
		totalRewardsRedeemed: 1,  // 1 redeemed → 50% rate
	}

	overview, err := svc.GetOverview(context.Background(), platform.StaffID(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overview.RedemptionRate != 0.5 {
		t.Errorf("RedemptionRate = %v, want 0.5", overview.RedemptionRate)
	}
}
