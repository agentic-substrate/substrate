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

-- name: GetIngestReceipt :one
SELECT client_id, principal_id, subject_type, subject_id
FROM ingest_receipt
WHERE client_id = $1;

-- name: InsertIngestReceipt :exec
INSERT INTO ingest_receipt (client_id, principal_id, subject_type, subject_id)
VALUES ($1, $2, $3, $4);

-- name: ListMemoryCache :many
SELECT id, scope_id, title, body, identifiers, status, updated_at
FROM memory
WHERE status IN ('confirmed', 'probable')
  AND updated_at > @since
ORDER BY updated_at ASC;

-- name: ListApprovedSkills :many
SELECT s.name, v.git_path, v.git_sha
FROM skill s
JOIN skill_version v ON v.id = s.active_version_id
WHERE s.active_version_id IS NOT NULL
  AND s.scope_id = ANY(@scope_ids::uuid[])
ORDER BY s.name;

-- name: InsertInstruction :one
INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, status;

-- name: InsertPreference :one
INSERT INTO preference (id, scope_id, visibility, owner_id, key, body, status, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, status;

-- name: ListOpenImportConflicts :many
SELECT id, payload FROM review_item
WHERE kind = 'import_conflict' AND status = 'open';

-- name: InsertReviewItem :one
INSERT INTO review_item (id, kind, scope_id, team_id, payload, proposed_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, kind, scope_id, team_id, payload, status, proposed_by, created_at;

-- name: ListReviewItems :many
SELECT id, kind, scope_id, team_id, payload, status, proposed_by, decided_by, decided_at, reason, created_at
FROM review_item
WHERE (sqlc.narg('team_id')::uuid IS NULL OR team_id = sqlc.narg('team_id'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::review_status)
ORDER BY created_at DESC;

-- name: DecideReviewItem :one
UPDATE review_item
SET status = $2, decided_by = $3, decided_at = now(), reason = $4
WHERE id = $1 AND status = 'open'
RETURNING id, status, decided_by, decided_at, reason;

-- name: GetReviewItem :one
SELECT id, kind, scope_id, team_id, payload, status, proposed_by, decided_by, decided_at, reason, created_at
FROM review_item
WHERE id = $1;

-- name: ListInstructionsByBodies :many
SELECT i.id, i.scope_id, i.key, i.body, i.status
FROM instruction i
JOIN scope s ON s.id = i.scope_id
WHERE i.body = ANY(@bodies::text[])
  AND i.scope_id = ANY(@scope_ids::uuid[])
  AND s.team_id = @team_id;

-- name: ListPreferencesByBodies :many
SELECT p.id, p.scope_id, p.key, p.body, p.status
FROM preference p
JOIN scope s ON s.id = p.scope_id
WHERE p.body = ANY(@bodies::text[])
  AND p.scope_id = ANY(@scope_ids::uuid[])
  AND s.team_id = @team_id;

-- name: ReviewApplyInstructionStatus :one
SELECT review_apply_instruction_status(
  @body::text,
  @status::instruction_status,
  @scope_ids::uuid[],
  @team_id::uuid
) AS n;

-- name: ReviewApplyPreferenceStatus :one
SELECT review_apply_preference_status(
  @body::text,
  @status::instruction_status,
  @scope_ids::uuid[],
  @team_id::uuid
) AS n;
