-- Queries for Square OAuth token management.

-- name: UpsertSquareOAuthToken :exec
-- On re-authorisation (e.g. annual renewal) this overwrites the existing row.
INSERT INTO square_oauth_tokens
    (merchant_id, location_id, access_token, refresh_token, expires_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (merchant_id) DO UPDATE SET
    location_id   = EXCLUDED.location_id,
    access_token  = EXCLUDED.access_token,
    refresh_token = EXCLUDED.refresh_token,
    expires_at    = EXCLUDED.expires_at;

-- name: GetSquareOAuthToken :one
-- Returns the most recently stored token. There is only ever one row per
-- merchant, but this query is written so adding multi-tenant support later
-- requires only a WHERE clause addition.
SELECT merchant_id, location_id, access_token, refresh_token, expires_at
FROM square_oauth_tokens
ORDER BY updated_at DESC
LIMIT 1;

-- name: UpdateSquareOAuthToken :exec
-- Called after a successful refresh to persist the new token pair.
UPDATE square_oauth_tokens
SET access_token  = $2,
    refresh_token = $3,
    expires_at    = $4
WHERE merchant_id = $1;
