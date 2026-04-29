-- Queries for the customers domain.
-- sqlc reads these files and generates type-safe Go in internal/gen/dbgen/.
-- Run `make generate` after modifying any query.

-- name: CreateCustomer :one
INSERT INTO customers (email, display_name)
VALUES ($1, $2)
RETURNING *;

-- name: GetCustomerByID :one
SELECT * FROM customers
WHERE id = $1;

-- name: GetCustomerByEmail :one
SELECT * FROM customers
WHERE email = $1;

-- name: GetCustomerByShopifyID :one
SELECT * FROM customers
WHERE shopify_customer_id = $1;

-- name: UpdateCustomer :one
UPDATE customers
SET
    display_name = COALESCE($2, display_name),
    updated_at   = NOW()
WHERE id = $1
RETURNING *;

-- name: LinkShopifyCustomer :one
UPDATE customers
SET
    shopify_customer_id = $2,
    updated_at          = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteCustomer :exec
DELETE FROM customers
WHERE id = $1;

-- name: LinkAnonymousVisitor :exec
UPDATE anonymous_visitors
SET
    linked_to = $2,
    linked_at = NOW()
WHERE id = $1
  AND linked_to IS NULL;
