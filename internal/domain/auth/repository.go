package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/q04-oss/whisked-platform/internal/platform"
)

// PGXRepository is the PostgreSQL implementation of the auth repository.
type PGXRepository struct {
	db *pgxpool.Pool
}

// NewRepository returns a PGXRepository backed by the given pool.
func NewRepository(db *pgxpool.Pool) *PGXRepository {
	return &PGXRepository{db: db}
}

func (r *PGXRepository) createCustomerWithCredentials(ctx context.Context, email, displayName, passwordHash string) (*authenticatedCustomer, error) {
	var c authenticatedCustomer
	var rawID int64

	err := r.db.QueryRow(ctx,
		`WITH new_customer AS (
		     INSERT INTO customers (email, display_name)
		     VALUES ($1, $2)
		     RETURNING id, email, display_name, created_at
		 )
		 INSERT INTO customer_credentials (customer_id, password_hash)
		 SELECT id, $3 FROM new_customer
		 RETURNING
		     (SELECT id FROM new_customer),
		     (SELECT email FROM new_customer),
		     (SELECT display_name FROM new_customer),
		     (SELECT created_at FROM new_customer)`,
		email,
		displayName,
		passwordHash,
	).Scan(&rawID, &c.email, &c.displayName, &c.createdAt)
	if err != nil {
		return nil, err
	}

	c.id = platform.CustomerID(rawID)
	return &c, nil
}

func (r *PGXRepository) getCustomerByEmailWithHash(ctx context.Context, email string) (*authenticatedCustomer, error) {
	var c authenticatedCustomer
	var rawID int64

	err := r.db.QueryRow(ctx,
		`SELECT c.id, c.email, c.display_name, c.created_at, cc.password_hash
		 FROM customers c
		 JOIN customer_credentials cc ON cc.customer_id = c.id
		 WHERE c.email = $1`,
		email,
	).Scan(&rawID, &c.email, &c.displayName, &c.createdAt, &c.passwordHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, platform.ErrNotFound
		}
		return nil, fmt.Errorf("scanning customer: %w", err)
	}

	c.id = platform.CustomerID(rawID)
	return &c, nil
}

func (r *PGXRepository) linkSquareCustomer(ctx context.Context, customerID platform.CustomerID, squareCustomerID string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE customers SET square_customer_id = $2 WHERE id = $1`,
		customerID.Int64(), squareCustomerID,
	)
	return err
}

// isDuplicateEmail detects a PostgreSQL unique constraint violation on email.
func isDuplicateEmail(err error) bool {
	return strings.Contains(err.Error(), "unique") &&
		strings.Contains(err.Error(), "customers_email_key")
}
