-- name: ListSettings :many
SELECT * FROM setting ORDER BY key;

-- name: UpsertSetting :exec
INSERT INTO setting (key, value) VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
