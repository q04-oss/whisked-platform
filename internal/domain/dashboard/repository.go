package dashboard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGXRepository is the PostgreSQL implementation of the dashboard repository.
// All methods are read-only — the dashboard never mutates operational data.
type PGXRepository struct {
	db *pgxpool.Pool
}

// NewRepository returns a PGXRepository backed by the given pool.
func NewRepository(db *pgxpool.Pool) *PGXRepository {
	return &PGXRepository{db: db}
}

type overviewRow struct {
	totalCustomers       int64
	activeCustomers30d   int64
	steepsThisWeek       int64
	steepsThisMonth      int64
	totalRewardsRedeemed int64
	totalSteepsEarned    int64
}

func (r *PGXRepository) getOverview(ctx context.Context) (*overviewRow, error) {
	var row overviewRow
	err := r.db.QueryRow(ctx,
		`SELECT
		     (SELECT COUNT(*) FROM customers),
		     (SELECT COUNT(DISTINCT customer_id) FROM loyalty_events WHERE created_at >= NOW() - INTERVAL '30 days'),
		     (SELECT COUNT(*) FROM loyalty_events WHERE event_type = 'steep_earned' AND created_at >= NOW() - INTERVAL '7 days'),
		     (SELECT COUNT(*) FROM loyalty_events WHERE event_type = 'steep_earned' AND created_at >= NOW() - INTERVAL '30 days'),
		     (SELECT COUNT(*) FROM loyalty_events WHERE event_type = 'reward_redeemed'),
		     (SELECT COUNT(*) FROM loyalty_events WHERE event_type = 'steep_earned')`,
	).Scan(
		&row.totalCustomers,
		&row.activeCustomers30d,
		&row.steepsThisWeek,
		&row.steepsThisMonth,
		&row.totalRewardsRedeemed,
		&row.totalSteepsEarned,
	)
	return &row, err
}

type dailySteepsRow struct {
	date   string
	source string
	count  int64
}

func (r *PGXRepository) getSteepsByDay(ctx context.Context, days int) ([]dailySteepsRow, error) {
	rows, err := r.db.Query(ctx,
		`SELECT
		     DATE(created_at AT TIME ZONE 'UTC')::text,
		     source,
		     COUNT(*)
		 FROM loyalty_events
		 WHERE event_type = 'steep_earned'
		   AND created_at >= NOW() - ($1 * INTERVAL '1 day')
		 GROUP BY DATE(created_at AT TIME ZONE 'UTC'), source
		 ORDER BY 1 DESC`,
		days,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []dailySteepsRow
	for rows.Next() {
		var row dailySteepsRow
		if err := rows.Scan(&row.date, &row.source, &row.count); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

type recentEventRow struct {
	id            int64
	eventType     string
	source        string
	createdAt     time.Time
	customerEmail string
	customerName  string
}

func (r *PGXRepository) getRecentLoyaltyEvents(ctx context.Context, limit int) ([]recentEventRow, error) {
	rows, err := r.db.Query(ctx,
		`SELECT
		     le.id, le.event_type, le.source, le.created_at,
		     c.email, c.display_name
		 FROM loyalty_events le
		 JOIN customers c ON c.id = le.customer_id
		 ORDER BY le.created_at DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []recentEventRow
	for rows.Next() {
		var row recentEventRow
		if err := rows.Scan(&row.id, &row.eventType, &row.source, &row.createdAt,
			&row.customerEmail, &row.customerName); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

type funnelRow struct {
	totalVisitors       int64
	identifiedVisitors  int64
	totalCustomers      int64
	customersWithSteeps int64
	repeatCustomers     int64
}

func (r *PGXRepository) getFunnelStats(ctx context.Context) (*funnelRow, error) {
	var row funnelRow
	err := r.db.QueryRow(ctx,
		`SELECT
		     (SELECT COUNT(*) FROM anonymous_visitors),
		     (SELECT COUNT(*) FROM anonymous_visitors WHERE linked_to IS NOT NULL),
		     (SELECT COUNT(*) FROM customers),
		     (SELECT COUNT(DISTINCT customer_id) FROM loyalty_events WHERE event_type = 'steep_earned'),
		     (SELECT COUNT(*) FROM (
		         SELECT customer_id FROM loyalty_events
		         WHERE event_type = 'steep_earned'
		         GROUP BY customer_id HAVING COUNT(*) > 1
		     ) sub)`,
	).Scan(
		&row.totalVisitors,
		&row.identifiedVisitors,
		&row.totalCustomers,
		&row.customersWithSteeps,
		&row.repeatCustomers,
	)
	return &row, err
}

type customerListRow struct {
	id              int64
	email           string
	displayName     string
	createdAt       time.Time
	steepsEarned    int64
	rewardsRedeemed int64
	lastActivity    *time.Time
}

func (r *PGXRepository) getCustomerList(ctx context.Context, limit, offset int) ([]customerListRow, error) {
	rows, err := r.db.Query(ctx,
		`SELECT
		     c.id, c.email, c.display_name, c.created_at,
		     COUNT(le.id) FILTER (WHERE le.event_type = 'steep_earned'),
		     COUNT(le.id) FILTER (WHERE le.event_type = 'reward_redeemed'),
		     MAX(le.created_at)
		 FROM customers c
		 LEFT JOIN loyalty_events le ON le.customer_id = c.id
		 GROUP BY c.id
		 ORDER BY c.created_at DESC
		 LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []customerListRow
	for rows.Next() {
		var row customerListRow
		if err := rows.Scan(&row.id, &row.email, &row.displayName, &row.createdAt,
			&row.steepsEarned, &row.rewardsRedeemed, &row.lastActivity); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// ── Staff auth queries ─────────────────────────────────────────────────────────

type staffRow struct {
	id           int64
	email        string
	displayName  string
	role         string
	passwordHash string
	active       bool
}

func (r *PGXRepository) getStaffByEmail(ctx context.Context, email string) (*staffRow, error) {
	var row staffRow
	err := r.db.QueryRow(ctx,
		`SELECT id, email, display_name, role, password_hash, active
		 FROM staff WHERE email = $1`,
		email,
	).Scan(&row.id, &row.email, &row.displayName, &row.role, &row.passwordHash, &row.active)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scanning staff: %w", err)
	}
	return &row, nil
}
