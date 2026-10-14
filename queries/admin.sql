-- name: GetAdminByUsername :one
SELECT * FROM admin_user WHERE username = $1;

-- name: CreateAdmin :one
INSERT INTO admin_user (username, password_hash) VALUES ($1, $2)
RETURNING *;
