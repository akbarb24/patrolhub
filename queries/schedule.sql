-- name: ListRulesForDate :many
-- Rules active on the given date for the given ISO weekday.
SELECT * FROM schedule_rule
WHERE day_of_week = sqlc.arg(dow)::smallint
  AND effective_from <= sqlc.arg(on_date)::date
  AND (effective_to IS NULL OR effective_to >= sqlc.arg(on_date)::date)
ORDER BY member_id;

-- name: ListRulesByMember :many
SELECT * FROM schedule_rule WHERE member_id = $1 ORDER BY effective_from, id;

-- name: CreateRule :one
INSERT INTO schedule_rule (member_id, day_of_week, effective_from)
VALUES ($1, $2, $3)
RETURNING *;

-- name: EndRule :exec
UPDATE schedule_rule SET effective_to = $2 WHERE id = $1;

-- name: ListOverridesForDate :many
SELECT * FROM schedule_override WHERE patrol_date = $1 ORDER BY id;

-- name: ListOverridesBetween :many
SELECT * FROM schedule_override
WHERE patrol_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
ORDER BY patrol_date, id;

-- name: CreateOverride :one
INSERT INTO schedule_override (patrol_date, action, member_id, replacement_member_id, note)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeleteOverride :exec
DELETE FROM schedule_override WHERE id = $1;
