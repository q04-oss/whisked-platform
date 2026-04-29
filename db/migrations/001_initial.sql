-- Migration 001: initial schema
--
-- Creates the foundational tables for customers, locations, loyalty events,
-- behavioral analytics, and the append-only audit log.
--
-- Design notes:
--   - audit_log is protected by a trigger that prevents UPDATE and DELETE.
--     Append-only at the database level, not just by application convention.
--   - loyalty_events and analytic_events are also append-only by trigger.
--   - All timestamps are TIMESTAMPTZ — the application always works in UTC.
--   - IDs are BIGSERIAL — headroom for global scale without overflow risk.

-- ── Customers ─────────────────────────────────────────────────────────────────

CREATE TABLE customers (
    id           BIGSERIAL PRIMARY KEY,
    email        TEXT UNIQUE,
    display_name TEXT,
    -- Links this customer to their Shopify account for loyalty sync.
    shopify_customer_id TEXT UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_customers_email ON customers (email) WHERE email IS NOT NULL;
CREATE INDEX idx_customers_shopify_id ON customers (shopify_customer_id) WHERE shopify_customer_id IS NOT NULL;

-- ── Anonymous visitors ────────────────────────────────────────────────────────

CREATE TABLE anonymous_visitors (
    id           TEXT PRIMARY KEY,  -- UUID assigned at first website visit
    linked_to    BIGINT REFERENCES customers (id),  -- set when visitor identifies
    linked_at    TIMESTAMPTZ,
    first_seen   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Locations ─────────────────────────────────────────────────────────────────

CREATE TABLE locations (
    id           BIGSERIAL PRIMARY KEY,
    name         TEXT NOT NULL,
    address      TEXT,
    type         TEXT NOT NULL CHECK (type IN ('permanent', 'popup', 'wholesale')),
    active       BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the first location.
INSERT INTO locations (name, address, type) VALUES
    ('Whisked Jasper Ave', '11931 Jasper Ave, Edmonton, Alberta', 'permanent');

-- ── Loyalty events ────────────────────────────────────────────────────────────

CREATE TABLE loyalty_events (
    id           BIGSERIAL PRIMARY KEY,
    customer_id  BIGINT NOT NULL REFERENCES customers (id),
    event_type   TEXT NOT NULL CHECK (event_type IN ('steep_earned', 'reward_redeemed', 'bonus_earned', 'adjustment')),
    source       TEXT NOT NULL CHECK (source IN ('in_bar', 'shopify', 'manual')),
    location_id  BIGINT REFERENCES locations (id),
    -- For Shopify-sourced events, the order reference.
    shopify_order_id TEXT,
    -- Idempotency key — prevents duplicate loyalty credit for the same real-world event.
    idempotency_key TEXT UNIQUE,
    metadata     JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_loyalty_events_customer ON loyalty_events (customer_id, created_at DESC);

-- ── Analytic events ───────────────────────────────────────────────────────────

CREATE TABLE analytic_events (
    id           BIGSERIAL PRIMARY KEY,
    event_type   TEXT NOT NULL,
    -- One of customer_id or visitor_id will be set; never both.
    customer_id  BIGINT REFERENCES customers (id),
    visitor_id   TEXT REFERENCES anonymous_visitors (id),
    location_id  BIGINT REFERENCES locations (id),
    session_id   TEXT,
    page         TEXT,
    metadata     JSONB,
    ip_address   INET,
    user_agent   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_analytic_events_customer    ON analytic_events (customer_id, created_at DESC) WHERE customer_id IS NOT NULL;
CREATE INDEX idx_analytic_events_visitor     ON analytic_events (visitor_id, created_at DESC)  WHERE visitor_id IS NOT NULL;
CREATE INDEX idx_analytic_events_type_time   ON analytic_events (event_type, created_at DESC);

-- ── Audit log ─────────────────────────────────────────────────────────────────

CREATE TABLE audit_log (
    id           BIGSERIAL PRIMARY KEY,
    event_type   TEXT NOT NULL,
    actor_id     BIGINT,
    actor_type   TEXT NOT NULL CHECK (actor_type IN ('customer', 'staff', 'system')),
    target_id    TEXT,
    target_type  TEXT,
    ip_address   TEXT,
    request_id   TEXT,
    metadata     JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_log_actor      ON audit_log (actor_id, created_at DESC) WHERE actor_id IS NOT NULL;
CREATE INDEX idx_audit_log_event_type ON audit_log (event_type, created_at DESC);
CREATE INDEX idx_audit_log_time       ON audit_log (created_at DESC);

-- Enforce append-only semantics at the database level.
-- No application bug or SQL client error can modify or delete audit records.
CREATE OR REPLACE FUNCTION prevent_audit_modification()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only: % on % is forbidden', TG_OP, TG_TABLE_NAME;
END;
$$;

CREATE TRIGGER audit_log_immutable
    BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_modification();

-- Apply the same protection to the event logs.
CREATE TRIGGER loyalty_events_immutable
    BEFORE UPDATE OR DELETE ON loyalty_events
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_modification();

CREATE TRIGGER analytic_events_immutable
    BEFORE UPDATE OR DELETE ON analytic_events
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_modification();

-- ── Staff (dashboard users) ───────────────────────────────────────────────────

CREATE TABLE staff (
    id           BIGSERIAL PRIMARY KEY,
    email        TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL,
    role         TEXT NOT NULL CHECK (role IN ('admin', 'viewer')),
    -- Argon2id hash of the staff member's password.
    password_hash TEXT NOT NULL,
    active       BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
