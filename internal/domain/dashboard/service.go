package dashboard

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/q04-oss/whisked-platform/internal/audit"
	"github.com/q04-oss/whisked-platform/internal/config"
	"github.com/q04-oss/whisked-platform/internal/domain/auth"
	"github.com/q04-oss/whisked-platform/internal/platform"
)

// repository is the interface Service requires from its data layer.
type repository interface {
	getOverview(ctx context.Context) (*overviewRow, error)
	getSteepsByDay(ctx context.Context, days int) ([]dailySteepsRow, error)
	getRecentLoyaltyEvents(ctx context.Context, limit int) ([]recentEventRow, error)
	getFunnelStats(ctx context.Context) (*funnelRow, error)
	getCustomerList(ctx context.Context, limit, offset int) ([]customerListRow, error)
	getStaffByEmail(ctx context.Context, email string) (*staffRow, error)
}

// Service handles dashboard authentication and analytics queries.
type Service struct {
	repo  repository
	redis *redis.Client
	cfg   *config.Config
	audit *audit.Writer
}

// NewService returns a Service wired to its dependencies.
func NewService(repo repository, rdb *redis.Client, cfg *config.Config, audit *audit.Writer) *Service {
	return &Service{repo: repo, redis: rdb, cfg: cfg, audit: audit}
}

// Login authenticates a staff member and returns a token pair.
// Deliberately vague on failure — no distinction between "not found" and "wrong password".
func (s *Service) Login(ctx context.Context, email, password string) (*StaffTokenPair, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return nil, platform.ErrUnauthenticated
	}

	staff, err := s.repo.getStaffByEmail(ctx, email)
	if err != nil || staff == nil {
		s.audit.Write(ctx, audit.Entry{
			EventType: audit.EventAuthLoginFailed,
			ActorType: audit.ActorSystem,
			Metadata:  map[string]string{"source": "dashboard", "reason": "staff_not_found"},
		})
		return nil, platform.ErrUnauthenticated
	}

	if !staff.active {
		s.audit.Write(ctx, audit.Entry{
			EventType: audit.EventAuthLoginFailed,
			ActorType: audit.ActorStaff,
			ActorID:   ptr(staff.id),
			Metadata:  map[string]string{"source": "dashboard", "reason": "account_inactive"},
		})
		return nil, platform.ErrUnauthenticated
	}

	ok, err := auth.VerifyPassword(password, staff.passwordHash)
	if err != nil || !ok {
		s.audit.Write(ctx, audit.Entry{
			EventType: audit.EventAuthLoginFailed,
			ActorType: audit.ActorStaff,
			ActorID:   ptr(staff.id),
			Metadata:  map[string]string{"source": "dashboard", "reason": "wrong_password"},
		})
		return nil, platform.ErrUnauthenticated
	}

	pair, refreshJTI, err := issueStaffTokenPair(staff.id, staff.role, s.cfg.JWTSecret.Expose())
	if err != nil {
		return nil, fmt.Errorf("dashboard.Login: issuing tokens: %w", err)
	}

	if err := s.redis.Set(ctx, staffRefreshKey(refreshJTI), staff.id, staffRefreshTokenTTL).Err(); err != nil {
		return nil, fmt.Errorf("dashboard.Login: storing refresh token: %w", err)
	}

	s.audit.Write(ctx, audit.Entry{
		EventType:  audit.EventAuthLogin,
		ActorType:  audit.ActorStaff,
		ActorID:    ptr(staff.id),
		TargetType: ptr("staff"),
		TargetID:   ptr(fmt.Sprintf("%d", staff.id)),
		Metadata:   map[string]string{"source": "dashboard"},
	})

	return pair, nil
}

// GetOverview returns the top-level metrics snapshot.
// Every call is audit-logged with the requesting staff member's identity.
func (s *Service) GetOverview(ctx context.Context, staffID platform.StaffID) (*Overview, error) {
	row, err := s.repo.getOverview(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard.GetOverview: %w", err)
	}

	var redemptionRate float64
	totalEligible := row.totalSteepsEarned / 9
	if totalEligible > 0 {
		redemptionRate = float64(row.totalRewardsRedeemed) / float64(totalEligible)
	}

	s.auditDashboardRead(ctx, staffID, "overview")

	return &Overview{
		TotalCustomers:       row.totalCustomers,
		ActiveCustomers30d:   row.activeCustomers30d,
		SteepsThisWeek:       row.steepsThisWeek,
		SteepsThisMonth:      row.steepsThisMonth,
		TotalSteepsEarned:    row.totalSteepsEarned,
		TotalRewardsRedeemed: row.totalRewardsRedeemed,
		RedemptionRate:       redemptionRate,
	}, nil
}

// GetLoyaltySummary returns loyalty program analytics with daily chart data.
func (s *Service) GetLoyaltySummary(ctx context.Context, staffID platform.StaffID, days int) (*LoyaltySummary, error) {
	if days <= 0 || days > 365 {
		days = 30
	}

	overview, err := s.repo.getOverview(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard.GetLoyaltySummary: %w", err)
	}

	daily, err := s.repo.getSteepsByDay(ctx, days)
	if err != nil {
		return nil, fmt.Errorf("dashboard.GetLoyaltySummary: %w", err)
	}

	points := make([]DailyCount, 0, len(daily))
	var inBar, shopify int64
	for _, row := range daily {
		points = append(points, DailyCount{
			Date:   row.date,
			Count:  row.count,
			Series: row.source,
		})
		switch row.source {
		case "in_bar":
			inBar += row.count
		case "shopify":
			shopify += row.count
		}
	}

	s.auditDashboardRead(ctx, staffID, "loyalty_summary")

	return &LoyaltySummary{
		TotalSteepsEarned:    overview.totalSteepsEarned,
		TotalRewardsRedeemed: overview.totalRewardsRedeemed,
		InBarSteeps:          inBar,
		ShopifySteeps:        shopify,
		SteepsByDay:          points,
	}, nil
}

// GetRecentLoyaltyEvents returns the latest N loyalty events for the live feed.
func (s *Service) GetRecentLoyaltyEvents(ctx context.Context, staffID platform.StaffID, limit int) ([]RecentLoyaltyEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	rows, err := s.repo.getRecentLoyaltyEvents(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("dashboard.GetRecentLoyaltyEvents: %w", err)
	}

	events := make([]RecentLoyaltyEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, RecentLoyaltyEvent{
			ID:            row.id,
			EventType:     row.eventType,
			Source:        row.source,
			CustomerEmail: row.customerEmail,
			CustomerName:  row.customerName,
			CreatedAt:     row.createdAt,
		})
	}

	s.auditDashboardRead(ctx, staffID, "recent_loyalty_events")
	return events, nil
}

// GetFunnelStats returns conversion funnel metrics with statistical context.
func (s *Service) GetFunnelStats(ctx context.Context, staffID platform.StaffID) (*FunnelStats, error) {
	row, err := s.repo.getFunnelStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard.GetFunnelStats: %w", err)
	}

	stats := &FunnelStats{
		TotalVisitors:       row.totalVisitors,
		IdentifiedVisitors:  row.identifiedVisitors,
		TotalCustomers:      row.totalCustomers,
		CustomersWithSteeps: row.customersWithSteeps,
		RepeatCustomers:     row.repeatCustomers,
		// Flag small samples so leadership doesn't over-interpret early data.
		SmallSample: row.totalVisitors < 30 || row.totalCustomers < 30,
	}

	if row.totalVisitors > 0 {
		stats.IdentificationRate = float64(row.identifiedVisitors) / float64(row.totalVisitors)
	}
	if row.totalCustomers > 0 {
		stats.FirstSteepRate = float64(row.customersWithSteeps) / float64(row.totalCustomers)
	}
	if row.customersWithSteeps > 0 {
		stats.RepeatRate = float64(row.repeatCustomers) / float64(row.customersWithSteeps)
	}

	s.auditDashboardRead(ctx, staffID, "funnel_stats")
	return stats, nil
}

// GetCustomerList returns a paginated list of customers with loyalty stats.
func (s *Service) GetCustomerList(ctx context.Context, staffID platform.StaffID, limit, offset int) ([]CustomerSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}

	rows, err := s.repo.getCustomerList(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("dashboard.GetCustomerList: %w", err)
	}

	customers := make([]CustomerSummary, 0, len(rows))
	for _, row := range rows {
		customers = append(customers, CustomerSummary{
			ID:              row.id,
			Email:           row.email,
			DisplayName:     row.displayName,
			JoinedAt:        row.createdAt,
			SteepsEarned:    row.steepsEarned,
			RewardsRedeemed: row.rewardsRedeemed,
			LastActivity:    row.lastActivity,
		})
	}

	s.auditDashboardRead(ctx, staffID, "customer_list")
	return customers, nil
}

// auditDashboardRead records every dashboard data access with the staff identity.
// This ensures any data access pattern can be reviewed for insider threat detection.
func (s *Service) auditDashboardRead(ctx context.Context, staffID platform.StaffID, resource string) {
	s.audit.Write(ctx, audit.Entry{
		EventType:  audit.EventCustomerRead,
		ActorType:  audit.ActorStaff,
		ActorID:    ptr(staffID.Int64()),
		TargetType: ptr("dashboard"),
		TargetID:   ptr(resource),
	})
}

func ptr[T any](v T) *T { return &v }
