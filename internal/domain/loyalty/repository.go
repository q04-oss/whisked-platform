package loyalty

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/q04-oss/whisked-platform/internal/platform"
)

// PGXRepository is the PostgreSQL implementation of the loyalty repository.
type PGXRepository struct {
	db *pgxpool.Pool
}

// NewRepository returns a PGXRepository backed by the given pool.
func NewRepository(db *pgxpool.Pool) *PGXRepository {
	return &PGXRepository{db: db}
}

type balanceRow struct {
	steepsEarned    int64
	rewardsRedeemed int64
}

func (r *PGXRepository) getBalanceRow(ctx context.Context, customerID platform.CustomerID) (balanceRow, error) {
	var row balanceRow
	err := r.db.QueryRow(ctx,
		`SELECT
		     COUNT(*) FILTER (WHERE event_type = 'steep_earned'),
		     COUNT(*) FILTER (WHERE event_type = 'reward_redeemed')
		 FROM loyalty_events
		 WHERE customer_id = $1`,
		customerID.Int64(),
	).Scan(&row.steepsEarned, &row.rewardsRedeemed)
	return row, err
}

type insertEventParams struct {
	customerID     platform.CustomerID
	eventType      string
	source         string
	locationID     *platform.LocationID
	shopifyOrderID *platform.ShopifyOrderID
	idempotencyKey string
}

func (r *PGXRepository) insertEvent(ctx context.Context, p insertEventParams) (*Event, error) {
	var (
		e        Event
		rawID    int64
		rawLocID *int64
	)

	var locID *int64
	if p.locationID != nil {
		v := p.locationID.Int64()
		locID = &v
	}

	var shopifyID *string
	if p.shopifyOrderID != nil {
		s := p.shopifyOrderID.String()
		shopifyID = &s
	}

	err := r.db.QueryRow(ctx,
		`INSERT INTO loyalty_events
		     (customer_id, event_type, source, location_id, shopify_order_id, idempotency_key)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, customer_id, event_type, source, location_id, created_at`,
		p.customerID.Int64(),
		p.eventType,
		p.source,
		locID,
		shopifyID,
		nullableString(p.idempotencyKey),
	).Scan(&rawID, new(int64), &e.EventType, &e.Source, &rawLocID, &e.CreatedAt)
	if err != nil {
		return nil, err
	}

	e.ID = platform.LoyaltyEventID(rawID)
	if rawLocID != nil {
		lid := platform.LocationID(*rawLocID)
		e.LocationID = &lid
	}
	return &e, nil
}

// redeemInTransaction atomically verifies the customer has available rewards
// and inserts a redemption event. Returns ErrBadRequest if no rewards are available.
func (r *PGXRepository) redeemInTransaction(ctx context.Context, customerID platform.CustomerID, idempotencyKey string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var steepsEarned, rewardsRedeemed int64
	err = tx.QueryRow(ctx,
		`SELECT
		     COUNT(*) FILTER (WHERE event_type = 'steep_earned'),
		     COUNT(*) FILTER (WHERE event_type = 'reward_redeemed')
		 FROM loyalty_events
		 WHERE customer_id = $1`,
		customerID.Int64(),
	).Scan(&steepsEarned, &rewardsRedeemed)
	if err != nil {
		return fmt.Errorf("computing balance in transaction: %w", err)
	}

	available := (steepsEarned / SteepsPerReward) - rewardsRedeemed
	if available <= 0 {
		return platform.Wrap(platform.ErrBadRequest, fmt.Errorf("no rewards available"))
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO loyalty_events (customer_id, event_type, source, idempotency_key)
		 VALUES ($1, 'reward_redeemed', 'in_bar', $2)`,
		customerID.Int64(),
		idempotencyKey,
	)
	if err != nil {
		if isDuplicateIdempotencyKey(err) {
			// Already redeemed — treat as success.
			return nil
		}
		return fmt.Errorf("inserting redemption event: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *PGXRepository) getHistory(ctx context.Context, customerID platform.CustomerID, limit, offset int) ([]Event, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, event_type, source, location_id, created_at
		 FROM loyalty_events
		 WHERE customer_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`,
		customerID.Int64(), limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var (
			e        Event
			rawID    int64
			rawLocID *int64
		)
		if err := rows.Scan(&rawID, &e.EventType, &e.Source, &rawLocID, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.ID = platform.LoyaltyEventID(rawID)
		if rawLocID != nil {
			lid := platform.LocationID(*rawLocID)
			e.LocationID = &lid
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (r *PGXRepository) getCustomerIDByEmail(ctx context.Context, email string) (platform.CustomerID, error) {
	var rawID int64
	err := r.db.QueryRow(ctx,
		`SELECT id FROM customers WHERE email = $1`, email,
	).Scan(&rawID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, platform.ErrNotFound
		}
		return 0, err
	}
	return platform.CustomerID(rawID), nil
}

type stampPageCustomer struct {
	ID          platform.CustomerID
	DisplayName string
	Email       string
}

func (r *PGXRepository) getCustomerIDBySquareID(ctx context.Context, squareCustomerID string) (platform.CustomerID, error) {
	var rawID int64
	err := r.db.QueryRow(ctx,
		`SELECT id FROM customers WHERE square_customer_id = $1`, squareCustomerID,
	).Scan(&rawID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, platform.ErrNotFound
		}
		return 0, err
	}
	return platform.CustomerID(rawID), nil
}

func (r *PGXRepository) getCustomerForStampPage(ctx context.Context, id platform.CustomerID) (stampPageCustomer, error) {
	var c stampPageCustomer
	var rawID int64
	err := r.db.QueryRow(ctx,
		`SELECT id, display_name, email FROM customers WHERE id = $1`, id.Int64(),
	).Scan(&rawID, &c.DisplayName, &c.Email)
	if err != nil {
		return stampPageCustomer{}, err
	}
	c.ID = platform.CustomerID(rawID)
	return c, nil
}

func isDuplicateIdempotencyKey(err error) bool {
	return strings.Contains(err.Error(), "unique") &&
		strings.Contains(err.Error(), "idempotency_key")
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
