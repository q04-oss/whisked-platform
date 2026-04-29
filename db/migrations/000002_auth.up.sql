-- Migration 002: customer credentials
--
-- Stores password hashes separately from customer profiles. This separation
-- means adding alternative auth methods (Apple Sign In, magic links) doesn't
-- require touching the customers table, and credential data can be queried
-- independently without loading the full profile.

CREATE TABLE customer_credentials (
    customer_id   BIGINT PRIMARY KEY REFERENCES customers (id) ON DELETE CASCADE,
    -- Argon2id hash. Format: $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
