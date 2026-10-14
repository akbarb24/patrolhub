-- name: InsertNightIfMissing :exec
INSERT INTO patrol_night (patrol_date) VALUES ($1)
ON CONFLICT (patrol_date) DO NOTHING;

-- name: GetNightByDate :one
SELECT * FROM patrol_night WHERE patrol_date = $1;

-- name: GetNightByDateForUpdate :one
SELECT * FROM patrol_night WHERE patrol_date = $1 FOR UPDATE;

-- name: MarkReported :exec
UPDATE patrol_night
SET first_reported_at = $2
WHERE id = $1 AND first_reported_at IS NULL;

-- name: UpdateSituation :exec
UPDATE patrol_night SET situation_note = $2 WHERE id = $1;

-- name: SetNightStatus :exec
UPDATE patrol_night
SET status = $2,
    rain_requested_at = COALESCE(sqlc.narg(rain_requested_at)::timestamptz, rain_requested_at)
WHERE id = $1;

-- name: CloseNight :exec
UPDATE patrol_night
SET status = $2, fine_rate = $3, needs_review = $4, closed_at = $5
WHERE id = $1;

-- name: SetNeedsReview :exec
UPDATE patrol_night SET needs_review = $2 WHERE id = $1;

-- name: ListUnclosedNightsBefore :many
SELECT * FROM patrol_night
WHERE closed_at IS NULL AND patrol_date < $1
ORDER BY patrol_date;

-- name: ListNightsBetween :many
SELECT * FROM patrol_night
WHERE patrol_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
ORDER BY patrol_date;
