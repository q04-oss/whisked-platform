-- Migration 004: Square OAuth tokens
--
-- Stores the OAuth access and refresh tokens for the connected Square merchant
-- account. The platform uses these to create orders on Belle's Square POS
-- when a customer places an in-app drink order.
--
-- Design notes:
--   • merchant_id is the Square-assigned merchant ID (e.g. "MLK2Q7WZMJ8JQ").
--     It is used as the unique key so re-running OAuth for the same merchant
--     is an upsert, not a duplicate row.
--   • access_token expires every 30 days. The application refreshes it
--     proactively when fewer than 24 hours remain.
--   • Tokens are stored in plaintext but the table has no SELECT grant to
--     the application role — access is via the service layer only.
--     At-rest encryption via Postgres pgcrypto is a future hardening step.

CREATE TABLE square_oauth_tokens (
    id            BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    merchant_id   TEXT        NOT NULL UNIQUE,
    location_id   TEXT        NOT NULL,
    access_token  TEXT        NOT NULL,
    refresh_token TEXT        NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Trigger: keep updated_at current on every write.
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER square_oauth_tokens_updated_at
    BEFORE UPDATE ON square_oauth_tokens
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
