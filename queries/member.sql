-- name: ListMembers :many
SELECT * FROM member ORDER BY house_block, name;

-- name: ListActiveMembers :many
SELECT * FROM member WHERE active ORDER BY house_block, name;

-- name: GetMember :one
SELECT * FROM member WHERE id = $1;

-- name: CreateMember :one
INSERT INTO member (house_block, name, phone, notes)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateMember :one
UPDATE member
SET house_block = $2, name = $3, phone = $4, notes = $5
WHERE id = $1
RETURNING *;

-- name: SetMemberActive :exec
UPDATE member SET active = $2 WHERE id = $1;
