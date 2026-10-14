-- name: UpsertAttendance :exec
INSERT INTO patrol_attendance (night_id, member_id, status, source)
VALUES ($1, $2, $3, $4)
ON CONFLICT (night_id, member_id) DO NOTHING;

-- name: ListAttendanceByNight :many
SELECT a.*, m.house_block, m.name AS member_name
FROM patrol_attendance a
JOIN member m ON m.id = a.member_id
WHERE a.night_id = $1
ORDER BY m.house_block, m.name;

-- name: ListAttendanceBetween :many
SELECT a.*, n.patrol_date, n.closed_at, m.house_block, m.name AS member_name
FROM patrol_attendance a
JOIN patrol_night n ON n.id = a.night_id
JOIN member m ON m.id = a.member_id
WHERE n.patrol_date BETWEEN sqlc.arg(from_date)::date AND sqlc.arg(to_date)::date
ORDER BY n.patrol_date, m.house_block, m.name;

-- name: SetAttendanceStatus :exec
UPDATE patrol_attendance SET status = $3, updated_at = now()
WHERE night_id = $1 AND member_id = $2;

-- name: SetAttendanceFine :exec
UPDATE patrol_attendance SET fine_amount = $3, updated_at = now()
WHERE night_id = $1 AND member_id = $2;

-- name: DeleteAbsentAttendanceNotIn :exec
-- Removes ABSENT rows of people dropped from the team; PRESENT and WALK_IN rows are kept.
DELETE FROM patrol_attendance
WHERE night_id = sqlc.arg(night_id)
  AND status = 'ABSENT'
  AND source <> 'WALK_IN'
  AND member_id <> ALL(sqlc.arg(member_ids)::bigint[]);
