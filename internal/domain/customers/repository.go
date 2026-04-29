package customers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/q04-oss/whisked-platform/internal/platform"
)

// PGXRepository is the PostgreSQL implementation of the repository interface
// consumed by Service. All queries use parameterized bindings — no string
// construction.
//
// NOTE: This implementation uses pgx directly. Once `make generate` is run,
// these queries will be replaced with sqlc-generated equivalents from
// internal/gen/dbgen. The interface contract and domain types remain unchanged.
type PGXRepository struct {
	db *pgxpool.Pool
}

// NewRepository returns a PGXRepository backed by the given pool.
func NewRepository(db *pgxpool.Pool) *PGXRepository {
	return &PGXRepository{db: db}
}

func (r *PGXRepository) create(ctx context.Context, params CreateParams) (*Customer, error) {
	row := r.db.QueryRow(ctx,
		`INSERT INTO customers (email, display_name)
		 VALUES ($1, $2)
		 RETURNING id, email, display_name, shopify_customer_id, created_at, updated_at`,
		params.Email,
		params.DisplayName,
	)
	return scanCustomer(row)
}

func (r *PGXRepository) getByID(ctx context.Context, id platform.CustomerID) (*Customer, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, email, display_name, shopify_customer_id, created_at, updated_at
		 FROM customers WHERE id = $1`,
		id.Int64(),
	)
	return scanCustomer(row)
}

func (r *PGXRepository) getByEmail(ctx context.Context, email string) (*Customer, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, email, display_name, shopify_customer_id, created_at, updated_at
		 FROM customers WHERE email = $1`,
		email,
	)
	return scanCustomer(row)
}

func (r *PGXRepository) update(ctx context.Context, id platform.CustomerID, params UpdateParams) (*Customer, error) {
	row := r.db.QueryRow(ctx,
		`UPDATE customers
		 SET display_name = COALESCE($2, display_name),
		     updated_at   = NOW()
		 WHERE id = $1
		 RETURNING id, email, display_name, shopify_customer_id, created_at, updated_at`,
		id.Int64(),
		params.DisplayName,
	)
	return scanCustomer(row)
}

func (r *PGXRepository) delete(ctx context.Context, id platform.CustomerID) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM customers WHERE id = $1`,
		id.Int64(),
	)
	return err
}

func (r *PGXRepository) linkAnonymousVisitor(ctx context.Context, visitorID platform.AnonymousVisitorID, customerID platform.CustomerID) error {
	_, err := r.db.Exec(ctx,
		`UPDATE anonymous_visitors
		 SET linked_to = $2, linked_at = NOW()
		 WHERE id = $1 AND linked_to IS NULL`,
		visitorID.String(),
		customerID.Int64(),
	)
	return err
}

// scanCustomer scans a single customer row into a Customer struct.
func scanCustomer(row pgx.Row) (*Customer, error) {
	var (
		c         Customer
		rawID     int64
		rawShopID *string
		rawTime   time.Time
	)

	err := row.Scan(
		&rawID,
		&c.Email,
		&c.DisplayName,
		&rawShopID,
		&rawTime,
		&c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, platform.ErrNotFound
		}
		return nil, fmt.Errorf("scanning customer: %w", err)
	}

	c.ID = platform.CustomerID(rawID)
	c.CreatedAt = rawTime

	if rawShopID != nil {
		sid := platform.ShopifyCustomerID(*rawShopID)
		c.ShopifyCustomerID = &sid
	}

	return &c, nil
}
