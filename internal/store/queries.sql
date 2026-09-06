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

-- name: InsertMemory :one
INSERT INTO memory (
  id, scope_id, visibility, owner_id, tier, kind, title, body, identifiers,
  status, source, verification, created_by
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
RETURNING id, status;

-- name: GetMemory :one
SELECT id, scope_id, visibility, owner_id, tier, kind, title, body, identifiers,
  status, superseded_by, source, verification, created_by
FROM memory
WHERE id = $1;

-- name: SetMemorySupersededBy :execrows
UPDATE memory
SET superseded_by = $2, status = 'superseded', updated_at = now()
WHERE id = $1 AND superseded_by IS NULL;

-- name: InsertMemoryEdge :exec
INSERT INTO memory_edge (from_id, to_id, relation, created_by)
VALUES ($1, $2, $3, $4);

-- name: InsertAudit :exec
INSERT INTO audit (actor_id, action, subject_type, subject_id, scope_id, reason, request_id)
VALUES ($1, $2, $3, $4, $5, $6, $7);
