-- +goose Up

-- STABLE lookups over scope.team_id / scope.path (EDD §3.7). Invoker, never
-- SECURITY DEFINER: definer rights would run as the table owner and skip RLS.

-- +goose StatementBegin
CREATE FUNCTION scope_team(p_scope_id uuid) RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE SET search_path = public AS $$
  SELECT s.team_id FROM scope s WHERE s.id = p_scope_id
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION scope_org(p_scope_id uuid) RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE SET search_path = public AS $$
  SELECT COALESCE(
    (SELECT t.org_id FROM scope s JOIN team t ON t.id = s.team_id WHERE s.id = p_scope_id),
    (SELECT o.id
     FROM scope s
     JOIN scope os ON os.kind = 'org' AND s.path <@ os.path
     JOIN org o ON o.name = os.key
     WHERE s.id = p_scope_id
     LIMIT 1)
  )
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION scope_project(p_scope_id uuid) RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE SET search_path = public AS $$
  SELECT ps.id
  FROM scope s
  JOIN scope ps ON ps.kind = 'project' AND s.path <@ ps.path
  WHERE s.id = p_scope_id
  LIMIT 1
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION scope_writable(p_scope_id uuid, p_actor uuid) RETURNS boolean
LANGUAGE sql STABLE PARALLEL SAFE SET search_path = public AS $$
  SELECT
    COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
    OR EXISTS (
      SELECT 1 FROM membership m
      WHERE m.principal_id = p_actor
        AND m.team_id = scope_team(p_scope_id)
    )
    OR EXISTS (
      SELECT 1 FROM project_grant g
      JOIN membership m ON m.team_id = g.team_id AND m.principal_id = p_actor
      WHERE g.project_scope_id = scope_project(p_scope_id)
        AND g.role >= 'member'
    )
    OR EXISTS (
      SELECT 1 FROM scope s
      WHERE s.id = p_scope_id AND s.kind = 'user' AND s.key = p_actor::text
    )
$$;
-- +goose StatementEnd

ALTER FUNCTION scope_team(uuid) OWNER TO substrate_migrate;
ALTER FUNCTION scope_org(uuid) OWNER TO substrate_migrate;
ALTER FUNCTION scope_project(uuid) OWNER TO substrate_migrate;
ALTER FUNCTION scope_writable(uuid, uuid) OWNER TO substrate_migrate;
GRANT EXECUTE ON FUNCTION scope_team(uuid), scope_org(uuid), scope_project(uuid), scope_writable(uuid, uuid) TO substrate_app;

-- Keep team_id denormalized so scope_team is a row lookup, not an ltree walk.
-- +goose StatementBegin
CREATE FUNCTION scope_denorm_team() RETURNS trigger
LANGUAGE plpgsql SET search_path = public AS $$
BEGIN
  IF NEW.kind IN ('global', 'org', 'user') THEN
    NEW.team_id := NULL;
    RETURN NEW;
  END IF;
  IF NEW.kind = 'team' THEN
    IF NEW.team_id IS NULL THEN
      SELECT t.id INTO NEW.team_id
      FROM team t
      JOIN org o ON o.id = t.org_id
      JOIN scope os ON os.id = NEW.parent_id AND os.kind = 'org' AND os.key = o.name
      WHERE t.name = NEW.key;
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.team_id IS NULL THEN
    SELECT s.team_id INTO NEW.team_id FROM scope s WHERE s.id = NEW.parent_id;
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION scope_denorm_team() OWNER TO substrate_migrate;
CREATE TRIGGER scope_denorm_team BEFORE INSERT OR UPDATE ON scope
  FOR EACH ROW EXECUTE FUNCTION scope_denorm_team();

-- missing_ok + NULLIF: a missing GUC must filter to zero rows, never RAISE.
-- A policy that errors instead of filtering tells the caller the row exists.

-- Owners may edit body/title; status/visibility/owner_id/scope_id stay frozen
-- except for human_admin. Return NULL cancels the row update without raising,
-- so a denied mutate is not an existence leak on a row the caller can already see.
-- +goose StatementBegin
CREATE FUNCTION content_freeze_cols() RETURNS trigger
LANGUAGE plpgsql SET search_path = public AS $$
BEGIN
  IF COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false) THEN
    RETURN NEW;
  END IF;
  IF NEW.visibility IS DISTINCT FROM OLD.visibility
     OR NEW.owner_id IS DISTINCT FROM OLD.owner_id
     OR NEW.scope_id IS DISTINCT FROM OLD.scope_id THEN
    RETURN NULL;
  END IF;
  -- Nested: plpgsql evaluates AND fully, and skill has no status column.
  IF TG_TABLE_NAME IN ('instruction', 'preference', 'memory') THEN
    IF NEW.status IS DISTINCT FROM OLD.status THEN
      RETURN NULL;
    END IF;
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION content_freeze_cols() OWNER TO substrate_migrate;
GRANT EXECUTE ON FUNCTION content_freeze_cols() TO substrate_app;

-- +goose StatementBegin
DO $$
DECLARE
  t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['instruction', 'preference', 'memory', 'skill']
  LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format(
      $p$
      CREATE POLICY content_read ON %I FOR SELECT USING (
           visibility = 'global'
        OR (visibility = 'org' AND scope_org(scope_id) = NULLIF(current_setting('substrate.org_id', true), '')::uuid)
        OR (visibility = 'team' AND (
              scope_team(scope_id) = ANY (COALESCE(NULLIF(current_setting('substrate.team_ids', true), '')::uuid[], '{}'::uuid[]))
           OR scope_project(scope_id) = ANY (COALESCE(NULLIF(current_setting('substrate.granted_project_ids', true), '')::uuid[], '{}'::uuid[]))))
        OR (visibility = 'owner' AND owner_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid)
        OR COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
      )
      $p$, t);
    EXECUTE format(
      $p$
      CREATE POLICY content_write ON %I FOR INSERT WITH CHECK (
        owner_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
        AND scope_writable(scope_id, NULLIF(current_setting('substrate.actor_id', true), '')::uuid)
      )
      $p$, t);
    EXECUTE format(
      $p$
      CREATE POLICY content_update ON %I FOR UPDATE
      USING (
        COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
        OR (
          owner_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
          AND scope_writable(scope_id, NULLIF(current_setting('substrate.actor_id', true), '')::uuid)
        )
      )
      WITH CHECK (
        COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
        OR (
          owner_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
          AND scope_writable(scope_id, NULLIF(current_setting('substrate.actor_id', true), '')::uuid)
        )
      )
      $p$, t);
    EXECUTE format(
      'CREATE TRIGGER content_freeze_%I BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION content_freeze_cols()',
      t, t);
  END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE review_item ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_item FORCE ROW LEVEL SECURITY;

CREATE POLICY content_read ON review_item FOR SELECT USING (
  team_id = ANY (COALESCE(NULLIF(current_setting('substrate.team_ids', true), '')::uuid[], '{}'::uuid[]))
  OR COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
);

CREATE POLICY content_write ON review_item FOR INSERT WITH CHECK (
  proposed_by = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
  AND (
    team_id = ANY (COALESCE(NULLIF(current_setting('substrate.team_ids', true), '')::uuid[], '{}'::uuid[]))
    OR COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
  )
);

-- Proposer may withdraw an open row. Deciding is human_admin or team lead/admin.
-- proposed_by is immutable (WITH CHECK repeats it; a trigger backs that up).
CREATE POLICY review_withdraw ON review_item FOR UPDATE
USING (
  proposed_by = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
  AND status = 'open'
)
WITH CHECK (
  proposed_by = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
  AND status = 'withdrawn'
  AND decided_by IS NULL
);

CREATE POLICY review_decide ON review_item FOR UPDATE
USING (
  COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
  OR EXISTS (
    SELECT 1 FROM membership m
    WHERE m.principal_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
      AND m.team_id = review_item.team_id
      AND m.role IN ('lead', 'admin')
  )
)
WITH CHECK (
  -- Repeat USING here: permissive policies OR their WITH CHECKs, so a
  -- proposer who passes review_withdraw.USING must not satisfy this arm.
  status IN ('approved', 'rejected')
  AND decided_by = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
  AND (
    COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
    OR EXISTS (
      SELECT 1 FROM membership m
      WHERE m.principal_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
        AND m.team_id = review_item.team_id
        AND m.role IN ('lead', 'admin')
    )
  )
);

-- +goose StatementBegin
CREATE FUNCTION review_item_immutable_proposer() RETURNS trigger
LANGUAGE plpgsql SET search_path = public AS $$
BEGIN
  IF NEW.proposed_by IS DISTINCT FROM OLD.proposed_by THEN
    RETURN NULL;
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION review_item_immutable_proposer() OWNER TO substrate_migrate;
GRANT EXECUTE ON FUNCTION review_item_immutable_proposer() TO substrate_app;
CREATE TRIGGER review_item_immutable_proposer BEFORE UPDATE ON review_item
  FOR EACH ROW EXECUTE FUNCTION review_item_immutable_proposer();

-- Child tables inherit visibility from the parent row. BEFORE INSERT on
-- memory_edge / memory_feedback raises the same 42501 for a hidden parent as
-- for a missing one, so the FK cannot be used as an existence oracle.
ALTER TABLE skill_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE skill_version FORCE ROW LEVEL SECURITY;
CREATE POLICY content_read ON skill_version FOR SELECT USING (
  EXISTS (SELECT 1 FROM skill s WHERE s.id = skill_id)
);
CREATE POLICY content_write ON skill_version FOR INSERT WITH CHECK (
  author_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
  AND EXISTS (SELECT 1 FROM skill s WHERE s.id = skill_id
    AND (
      COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
      OR (s.owner_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
          AND scope_writable(s.scope_id, NULLIF(current_setting('substrate.actor_id', true), '')::uuid))
    ))
);
CREATE POLICY content_update ON skill_version FOR UPDATE
USING (EXISTS (SELECT 1 FROM skill s WHERE s.id = skill_id
  AND (
    COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
    OR (s.owner_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
        AND scope_writable(s.scope_id, NULLIF(current_setting('substrate.actor_id', true), '')::uuid))
  )))
WITH CHECK (EXISTS (SELECT 1 FROM skill s WHERE s.id = skill_id
  AND (
    COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
    OR (s.owner_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
        AND scope_writable(s.scope_id, NULLIF(current_setting('substrate.actor_id', true), '')::uuid))
  )));

ALTER TABLE memory_edge ENABLE ROW LEVEL SECURITY;
ALTER TABLE memory_edge FORCE ROW LEVEL SECURITY;
CREATE POLICY content_read ON memory_edge FOR SELECT USING (
  EXISTS (SELECT 1 FROM memory m WHERE m.id = from_id)
  AND EXISTS (SELECT 1 FROM memory m WHERE m.id = to_id)
);
CREATE POLICY content_write ON memory_edge FOR INSERT WITH CHECK (
  created_by = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
  AND EXISTS (SELECT 1 FROM memory m WHERE m.id = from_id)
  AND EXISTS (SELECT 1 FROM memory m WHERE m.id = to_id)
);

ALTER TABLE memory_feedback ENABLE ROW LEVEL SECURITY;
ALTER TABLE memory_feedback FORCE ROW LEVEL SECURITY;
CREATE POLICY content_read ON memory_feedback FOR SELECT USING (
  EXISTS (SELECT 1 FROM memory m WHERE m.id = memory_id)
);
CREATE POLICY content_write ON memory_feedback FOR INSERT WITH CHECK (
  principal_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
  AND EXISTS (SELECT 1 FROM memory m WHERE m.id = memory_id)
);

ALTER TABLE ingest_receipt ENABLE ROW LEVEL SECURITY;
ALTER TABLE ingest_receipt FORCE ROW LEVEL SECURITY;
CREATE POLICY content_read ON ingest_receipt FOR SELECT USING (
  principal_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
  OR COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
);
CREATE POLICY content_write ON ingest_receipt FOR INSERT WITH CHECK (
  principal_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
);

-- +goose StatementBegin
CREATE FUNCTION require_visible_memory() RETURNS trigger
LANGUAGE plpgsql SET search_path = public AS $$
DECLARE
  hidden boolean := false;
BEGIN
  IF TG_TABLE_NAME = 'memory_edge' THEN
    hidden := NOT EXISTS (SELECT 1 FROM memory WHERE id = NEW.from_id)
           OR NOT EXISTS (SELECT 1 FROM memory WHERE id = NEW.to_id);
  ELSE
    hidden := NOT EXISTS (SELECT 1 FROM memory WHERE id = NEW.memory_id);
  END IF;
  IF hidden THEN
    RAISE EXCEPTION 'not visible' USING ERRCODE = '42501';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION require_visible_memory() OWNER TO substrate_migrate;
GRANT EXECUTE ON FUNCTION require_visible_memory() TO substrate_app;
CREATE TRIGGER memory_edge_visible BEFORE INSERT ON memory_edge
  FOR EACH ROW EXECUTE FUNCTION require_visible_memory();
CREATE TRIGGER memory_feedback_visible BEFORE INSERT ON memory_feedback
  FOR EACH ROW EXECUTE FUNCTION require_visible_memory();

-- +goose Down

DROP TRIGGER IF EXISTS memory_feedback_visible ON memory_feedback;
DROP TRIGGER IF EXISTS memory_edge_visible ON memory_edge;
DROP FUNCTION IF EXISTS require_visible_memory();

DROP POLICY IF EXISTS content_write ON ingest_receipt;
DROP POLICY IF EXISTS content_read ON ingest_receipt;
ALTER TABLE ingest_receipt NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ingest_receipt DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS content_write ON memory_feedback;
DROP POLICY IF EXISTS content_read ON memory_feedback;
ALTER TABLE memory_feedback NO FORCE ROW LEVEL SECURITY;
ALTER TABLE memory_feedback DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS content_write ON memory_edge;
DROP POLICY IF EXISTS content_read ON memory_edge;
ALTER TABLE memory_edge NO FORCE ROW LEVEL SECURITY;
ALTER TABLE memory_edge DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS content_update ON skill_version;
DROP POLICY IF EXISTS content_write ON skill_version;
DROP POLICY IF EXISTS content_read ON skill_version;
ALTER TABLE skill_version NO FORCE ROW LEVEL SECURITY;
ALTER TABLE skill_version DISABLE ROW LEVEL SECURITY;

DROP TRIGGER IF EXISTS review_item_immutable_proposer ON review_item;
DROP FUNCTION IF EXISTS review_item_immutable_proposer();
DROP POLICY IF EXISTS review_decide ON review_item;
DROP POLICY IF EXISTS review_withdraw ON review_item;
DROP POLICY IF EXISTS content_write ON review_item;
DROP POLICY IF EXISTS content_read ON review_item;
ALTER TABLE review_item NO FORCE ROW LEVEL SECURITY;
ALTER TABLE review_item DISABLE ROW LEVEL SECURITY;

DROP TRIGGER IF EXISTS content_freeze_skill ON skill;
DROP POLICY IF EXISTS content_update ON skill;
DROP POLICY IF EXISTS content_write ON skill;
DROP POLICY IF EXISTS content_read ON skill;
ALTER TABLE skill NO FORCE ROW LEVEL SECURITY;
ALTER TABLE skill DISABLE ROW LEVEL SECURITY;

DROP TRIGGER IF EXISTS content_freeze_memory ON memory;
DROP POLICY IF EXISTS content_update ON memory;
DROP POLICY IF EXISTS content_write ON memory;
DROP POLICY IF EXISTS content_read ON memory;
ALTER TABLE memory NO FORCE ROW LEVEL SECURITY;
ALTER TABLE memory DISABLE ROW LEVEL SECURITY;

DROP TRIGGER IF EXISTS content_freeze_preference ON preference;
DROP POLICY IF EXISTS content_update ON preference;
DROP POLICY IF EXISTS content_write ON preference;
DROP POLICY IF EXISTS content_read ON preference;
ALTER TABLE preference NO FORCE ROW LEVEL SECURITY;
ALTER TABLE preference DISABLE ROW LEVEL SECURITY;

DROP TRIGGER IF EXISTS content_freeze_instruction ON instruction;
DROP POLICY IF EXISTS content_update ON instruction;
DROP POLICY IF EXISTS content_write ON instruction;
DROP POLICY IF EXISTS content_read ON instruction;
ALTER TABLE instruction NO FORCE ROW LEVEL SECURITY;
ALTER TABLE instruction DISABLE ROW LEVEL SECURITY;

DROP FUNCTION IF EXISTS content_freeze_cols();
DROP TRIGGER IF EXISTS scope_denorm_team ON scope;
DROP FUNCTION IF EXISTS scope_denorm_team();
DROP FUNCTION IF EXISTS scope_writable(uuid, uuid);
DROP FUNCTION IF EXISTS scope_project(uuid);
DROP FUNCTION IF EXISTS scope_org(uuid);
DROP FUNCTION IF EXISTS scope_team(uuid);
