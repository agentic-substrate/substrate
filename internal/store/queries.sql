-- name: Ping :one
SELECT 1::int AS ok;

-- name: GetScope :one
SELECT id, kind, parent_id, key, team_id, depth, path::text AS path
FROM scope
WHERE id = $1;

-- name: LookupScope :one
SELECT id, kind, parent_id, key, team_id, depth, path::text AS path
FROM scope
WHERE kind = $1
  AND key = $2
  AND parent_id IS NOT DISTINCT FROM $3;

-- name: ListActiveInstructions :many
SELECT id, scope_id, kind, key, body
FROM instruction
WHERE status = 'active'
  AND scope_id = ANY(@scope_ids::uuid[]);

-- name: ListActivePreferences :many
SELECT id, scope_id, key, body
FROM preference
WHERE status = 'active'
  AND scope_id = ANY(@scope_ids::uuid[]);
