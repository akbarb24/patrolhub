-- name: InsertAudit :exec
INSERT INTO audit_log (entity, entity_id, action, actor, ip, user_agent, reason, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListAuditByNight :many
-- Night history: entries on the night itself, plus entries on child rows that carry night_id in detail.
SELECT * FROM audit_log
WHERE (entity = 'patrol_night' AND entity_id = sqlc.arg(night_id)::bigint)
   OR (detail ->> 'night_id')::bigint = sqlc.arg(night_id)::bigint
ORDER BY created_at, id;
