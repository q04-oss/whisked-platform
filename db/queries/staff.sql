-- Queries for staff authentication (dashboard login).

-- name: GetStaffByEmail :one
SELECT id, email, display_name, role, password_hash, active
FROM staff
WHERE email = $1;

-- name: CreateStaff :one
INSERT INTO staff (email, display_name, role, password_hash)
VALUES ($1, $2, $3, $4)
RETURNING id, email, display_name, role, active, created_at;
