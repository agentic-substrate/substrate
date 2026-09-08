-- +goose Up

-- review_item's INSERT policy checked proposed_by and team_id only, so a row
-- could be filed at ANY scope_id -- including another team's repo -- as long
-- as it was stamped with the proposer's own team. It then became readable and
-- decidable by the proposer's team lead, payload and all. Every content table
-- already gates its INSERT on scope_writable; review_item is brought in line.
--
-- This is the backstop, not the gate: POST /v1/review derives the chain from
-- the repo key server-side. The policy is what survives the next handler bug.

DROP POLICY content_write ON review_item;

CREATE POLICY content_write ON review_item FOR INSERT WITH CHECK (
  proposed_by = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
  AND (
    team_id = ANY (COALESCE(NULLIF(current_setting('substrate.team_ids', true), '')::uuid[], '{}'::uuid[]))
    OR COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
  )
  AND scope_writable(scope_id, NULLIF(current_setting('substrate.actor_id', true), '')::uuid)
);

-- +goose Down

DROP POLICY content_write ON review_item;

CREATE POLICY content_write ON review_item FOR INSERT WITH CHECK (
  proposed_by = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
  AND (
    team_id = ANY (COALESCE(NULLIF(current_setting('substrate.team_ids', true), '')::uuid[], '{}'::uuid[]))
    OR COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
  )
);
