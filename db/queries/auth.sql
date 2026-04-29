-- Queries for the auth domain.

-- name: CreateCustomerWithCredentials :one
-- Registers a new customer and stores their password hash in a single
-- transaction. Returns the new customer row.
WITH new_customer AS (
    INSERT INTO customers (email, display_name)
    VALUES ($1, $2)
    RETURNING id, email, display_name, created_at
)
INSERT INTO customer_credentials (customer_id, password_hash)
SELECT id, $3 FROM new_customer
RETURNING (SELECT id FROM new_customer),
          (SELECT email FROM new_customer),
          (SELECT display_name FROM new_customer),
          (SELECT created_at FROM new_customer);

-- name: GetCustomerByEmailWithHash :one
-- Used only during login. Returns the customer and their password hash
-- in one query to avoid a round trip.
SELECT
    c.id,
    c.email,
    c.display_name,
    c.created_at,
    cc.password_hash
FROM customers c
JOIN customer_credentials cc ON cc.customer_id = c.id
WHERE c.email = $1;
