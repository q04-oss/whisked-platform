-- Queries for the analytics domain.
-- All writes are append-only — analytics events are never updated or deleted
-- through application code. The DB trigger in migration 001 enforces this.

-- name: UpsertAnonymousVisitor :exec
-- Creates a visitor record on first event, updates last_seen on subsequent ones.
INSERT INTO anonymous_visitors (id, first_seen, last_seen)
VALUES ($1, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET last_seen = NOW();

-- name: InsertAnalyticEvent :exec
INSERT INTO analytic_events
    (event_type, customer_id, visitor_id, session_id, page, metadata, ip_address, user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: LinkAnonymousVisitor :exec
-- Associates a visitor's behavioral history with their identified customer account.
-- WHERE clause prevents overwriting an existing link.
UPDATE anonymous_visitors
SET linked_to = $2, linked_at = NOW()
WHERE id = $1 AND linked_to IS NULL;

-- name: GetVisitorCustomerLink :one
SELECT linked_to FROM anonymous_visitors WHERE id = $1;
