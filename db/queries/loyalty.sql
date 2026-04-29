-- Queries for the loyalty domain.

-- name: GetLoyaltyBalance :one
-- Derives the current balance directly from the append-only event log.
-- There is no mutable steeps_count column — the log is the source of truth.
SELECT
    COUNT(*) FILTER (WHERE event_type = 'steep_earned')    AS steeps_earned,
    COUNT(*) FILTER (WHERE event_type = 'reward_redeemed') AS rewards_redeemed
FROM loyalty_events
WHERE customer_id = $1;

-- name: InsertLoyaltyEvent :one
INSERT INTO loyalty_events
    (customer_id, event_type, source, location_id, shopify_order_id, idempotency_key, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, customer_id, event_type, source, location_id, created_at;

-- name: GetLoyaltyHistory :many
SELECT id, customer_id, event_type, source, location_id, shopify_order_id, metadata, created_at
FROM loyalty_events
WHERE customer_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: IdempotencyKeyExists :one
SELECT EXISTS(
    SELECT 1 FROM loyalty_events WHERE idempotency_key = $1
) AS exists;

-- name: GetCustomerIDByEmail :one
-- Used by the Shopify webhook to link an order to a customer.
-- The loyalty domain does not import the customers package — it owns this query.
SELECT id FROM customers WHERE email = $1;
