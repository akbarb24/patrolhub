-- name: InsertPhoto :one
INSERT INTO patrol_photo (night_id, storage_key, taken_at, latitude, longitude, size_bytes)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CountPhotosByNight :one
SELECT count(*) FROM patrol_photo WHERE night_id = $1;

-- name: ListPhotosByNight :many
SELECT * FROM patrol_photo WHERE night_id = $1 ORDER BY taken_at, id;

-- name: GetPhoto :one
SELECT * FROM patrol_photo WHERE id = $1;

-- name: ListPhotosOlderThan :many
SELECT * FROM patrol_photo WHERE created_at < $1 ORDER BY id;

-- name: DeletePhoto :exec
DELETE FROM patrol_photo WHERE id = $1;
