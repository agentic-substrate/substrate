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
      SELECT 1 FROM scope s WHERE s.id = p_scope_id AND s.kind = 'user'
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

CREATE POLICY content_update ON review_item FOR UPDATE
USING (
  team_id = ANY (COALESCE(NULLIF(current_setting('substrate.team_ids', true), '')::uuid[], '{}'::uuid[]))
  OR COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
)
WITH CHECK (
  team_id = ANY (COALESCE(NULLIF(current_setting('substrate.team_ids', true), '')::uuid[], '{}'::uuid[]))
  OR COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
);

-- +goose Down

DROP POLICY IF EXISTS content_update ON review_item;
DROP POLICY IF EXISTS content_write ON review_item;
DROP POLICY IF EXISTS content_read ON review_item;
ALTER TABLE review_item NO FORCE ROW LEVEL SECURITY;
ALTER TABLE review_item DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS content_update ON skill;
DROP POLICY IF EXISTS content_write ON skill;
DROP POLICY IF EXISTS content_read ON skill;
ALTER TABLE skill NO FORCE ROW LEVEL SECURITY;
ALTER TABLE skill DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS content_update ON memory;
DROP POLICY IF EXISTS content_write ON memory;
DROP POLICY IF EXISTS content_read ON memory;
ALTER TABLE memory NO FORCE ROW LEVEL SECURITY;
ALTER TABLE memory DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS content_update ON preference;
DROP POLICY IF EXISTS content_write ON preference;
DROP POLICY IF EXISTS content_read ON preference;
ALTER TABLE preference NO FORCE ROW LEVEL SECURITY;
ALTER TABLE preference DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS content_update ON instruction;
DROP POLICY IF EXISTS content_write ON instruction;
DROP POLICY IF EXISTS content_read ON instruction;
ALTER TABLE instruction NO FORCE ROW LEVEL SECURITY;
ALTER TABLE instruction DISABLE ROW LEVEL SECURITY;

DROP TRIGGER IF EXISTS scope_denorm_team ON scope;
DROP FUNCTION IF EXISTS scope_denorm_team();
DROP FUNCTION IF EXISTS scope_writable(uuid, uuid);
DROP FUNCTION IF EXISTS scope_project(uuid);
DROP FUNCTION IF EXISTS scope_org(uuid);
DROP FUNCTION IF EXISTS scope_team(uuid);
