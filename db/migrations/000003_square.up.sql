-- Migration 003: Square integration
--
-- Adds square_customer_id to customers so Square payment events can be
-- automatically matched to Whisked accounts without any action from the
-- customer or staff at the point of sale.
--
-- The stamp_tokens table backs the QR code loyalty validation flow.
-- Each token is short-lived, single-use, and cryptographically random.
-- The table is a secondary backstop — Redis is the primary store.
-- Rows older than 10 minutes are safe to delete.

ALTER TABLE customers
    ADD COLUMN square_customer_id TEXT UNIQUE;

CREATE INDEX idx_customers_square_id ON customers (square_customer_id)
    WHERE square_customer_id IS NOT NULL;
