-- Read-only aggregate queries for the internal dashboard.
-- All queries here are analytics reads — they never mutate operational data.

-- name: GetDashboardOverview :one
SELECT
    (SELECT COUNT(*)                                                                     FROM customers)                                                   AS total_customers,
    (SELECT COUNT(DISTINCT customer_id) FROM loyalty_events WHERE created_at >= NOW() - INTERVAL '30 days') AS active_customers_30d,
    (SELECT COUNT(*) FROM loyalty_events WHERE event_type = 'steep_earned'  AND created_at >= NOW() - INTERVAL '7 days')  AS steeps_this_week,
    (SELECT COUNT(*) FROM loyalty_events WHERE event_type = 'steep_earned'  AND created_at >= NOW() - INTERVAL '30 days') AS steeps_this_month,
    (SELECT COUNT(*) FROM loyalty_events WHERE event_type = 'reward_redeemed')                             AS total_rewards_redeemed,
    (SELECT COUNT(*) FROM loyalty_events WHERE event_type = 'steep_earned')                                AS total_steeps_earned;

-- name: GetSteepsByDay :many
-- Returns daily steep counts for the last N days, broken out by source.
SELECT
    DATE(created_at AT TIME ZONE 'UTC') AS date,
    source,
    COUNT(*)                            AS count
FROM loyalty_events
WHERE event_type = 'steep_earned'
  AND created_at >= NOW() - ($1::int || ' days')::INTERVAL
GROUP BY DATE(created_at AT TIME ZONE 'UTC'), source
ORDER BY date DESC;

-- name: GetRecentLoyaltyEvents :many
SELECT
    le.id,
    le.event_type,
    le.source,
    le.created_at,
    c.email        AS customer_email,
    c.display_name AS customer_name
FROM loyalty_events le
JOIN customers c ON c.id = le.customer_id
ORDER BY le.created_at DESC
LIMIT $1;

-- name: GetFunnelStats :one
SELECT
    (SELECT COUNT(*) FROM anonymous_visitors)                                AS total_visitors,
    (SELECT COUNT(*) FROM anonymous_visitors WHERE linked_to IS NOT NULL)    AS identified_visitors,
    (SELECT COUNT(*) FROM customers)                                         AS total_customers,
    (SELECT COUNT(DISTINCT customer_id) FROM loyalty_events WHERE event_type = 'steep_earned') AS customers_with_steeps,
    (SELECT COUNT(DISTINCT customer_id) FROM loyalty_events WHERE event_type = 'steep_earned'
       AND customer_id IN (
           SELECT DISTINCT customer_id FROM loyalty_events
           WHERE event_type = 'steep_earned'
           GROUP BY customer_id HAVING COUNT(*) > 1
       )
    ) AS repeat_customers;

-- name: GetCustomerList :many
SELECT
    c.id,
    c.email,
    c.display_name,
    c.created_at,
    COUNT(le.id) FILTER (WHERE le.event_type = 'steep_earned')    AS steeps_earned,
    COUNT(le.id) FILTER (WHERE le.event_type = 'reward_redeemed') AS rewards_redeemed,
    MAX(le.created_at)                                             AS last_activity
FROM customers c
LEFT JOIN loyalty_events le ON le.customer_id = c.id
GROUP BY c.id
ORDER BY c.created_at DESC
LIMIT $1 OFFSET $2;

-- name: GetCustomerDetail :one
SELECT
    c.id,
    c.email,
    c.display_name,
    c.created_at,
    COUNT(le.id) FILTER (WHERE le.event_type = 'steep_earned')    AS steeps_earned,
    COUNT(le.id) FILTER (WHERE le.event_type = 'reward_redeemed') AS rewards_redeemed,
    MAX(le.created_at)                                             AS last_activity
FROM customers c
LEFT JOIN loyalty_events le ON le.customer_id = c.id
WHERE c.id = $1
GROUP BY c.id;
