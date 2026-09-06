-- name: Ping :one
SELECT 1::int AS ok;

-- name: GetScope :one
SELECT id, kind, parent_id, key, team_id, depth, path::text AS path
FROM scope
WHERE id = $1;
