-- name: ListPaymentsForPeriod :many
SELECT * FROM fine_payment WHERE period = $1 ORDER BY paid_at, id;

-- name: InsertPayment :one
INSERT INTO fine_payment (member_id, period, amount, paid_at, recorded_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeletePayment :exec
DELETE FROM fine_payment WHERE id = $1;
