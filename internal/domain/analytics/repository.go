package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/q04-oss/whisked-platform/internal/platform"
)

// PGXRepository is the PostgreSQL implementation of the analytics repository.
type PGXRepository struct {
	db *pgxpool.Pool
}

// NewRepository returns a PGXRepository backed by the given pool.
func NewRepository(db *pgxpool.Pool) *PGXRepository {
	return &PGXRepository{db: db}
}

type insertEventParams struct {
	eventType  string
	customerID *platform.CustomerID
	visitorID  *platform.AnonymousVisitorID
	sessionID  *string
	page       *string
	metadata   any
	ipAddress  *string
	userAgent  *string
}

func (r *PGXRepository) upsertVisitor(ctx context.Context, visitorID platform.AnonymousVisitorID) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO anonymous_visitors (id, first_seen, last_seen)
		 VALUES ($1, NOW(), NOW())
		 ON CONFLICT (id) DO UPDATE SET last_seen = NOW()`,
		visitorID.String(),
	)
	return err
}

func (r *PGXRepository) insertEvent(ctx context.Context, p insertEventParams) error {
	meta, err := json.Marshal(p.metadata)
	if err != nil {
		meta = []byte("null")
	}

	var customerID *int64
	if p.customerID != nil {
		v := p.customerID.Int64()
		customerID = &v
	}

	var visitorID *string
	if p.visitorID != nil {
		v := p.visitorID.String()
		visitorID = &v
	}

	_, err = r.db.Exec(ctx,
		`INSERT INTO analytic_events
		     (event_type, customer_id, visitor_id, session_id, page, metadata, ip_address, user_agent)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		p.eventType,
		customerID,
		visitorID,
		p.sessionID,
		p.page,
		meta,
		p.ipAddress,
		p.userAgent,
	)
	return err
}

func (r *PGXRepository) linkVisitor(ctx context.Context, visitorID platform.AnonymousVisitorID, customerID platform.CustomerID) error {
	_, err := r.db.Exec(ctx,
		`UPDATE anonymous_visitors
		 SET linked_to = $2, linked_at = NOW()
		 WHERE id = $1 AND linked_to IS NULL`,
		visitorID.String(),
		customerID.Int64(),
	)
	return err
}

func (r *PGXRepository) getVisitorLink(ctx context.Context, visitorID platform.AnonymousVisitorID) (*platform.CustomerID, error) {
	var rawID *int64
	err := r.db.QueryRow(ctx,
		`SELECT linked_to FROM anonymous_visitors WHERE id = $1`,
		visitorID.String(),
	).Scan(&rawID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scanning visitor link: %w", err)
	}
	if rawID == nil {
		return nil, nil
	}
	cid := platform.CustomerID(*rawID)
	return &cid, nil
}
