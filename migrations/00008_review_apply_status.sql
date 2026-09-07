-- +goose Up

-- Review decide must change instruction/preference status without SET LOCAL
-- substrate.is_admin on the whole request. content_freeze_cols and
-- content_update otherwise no-op a lead's UPDATE (RowsAffected=0) while the
-- review_item still closes. These functions are the only status write: they
-- check lead/admin of p_team_id, elevate for the UPDATE only, and return the
-- row count so a 0- or many-row match can roll back.

-- +goose StatementBegin
CREATE FUNCTION review_apply_instruction_status(
  p_body text,
  p_status instruction_status,
  p_scope_ids uuid[],
  p_team_id uuid
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  n bigint;
  saved text := 'false';
BEGIN
  IF p_body IS NULL OR p_status IS NULL OR p_team_id IS NULL THEN
    RETURN 0;
  END IF;
  IF NOT (
    COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
    OR EXISTS (
      SELECT 1 FROM membership m
      WHERE m.principal_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
        AND m.team_id = p_team_id
        AND m.role IN ('lead', 'admin')
    )
  ) THEN
    RETURN 0;
  END IF;
  saved := COALESCE(NULLIF(current_setting('substrate.is_admin', true), ''), 'false');
  PERFORM set_config('substrate.is_admin', 'true', true);
  UPDATE instruction i
  SET status = p_status, updated_at = now()
  FROM scope s
  WHERE i.scope_id = s.id
    AND i.body = p_body
    AND i.scope_id = ANY(p_scope_ids)
    AND s.team_id = p_team_id;
  GET DIAGNOSTICS n = ROW_COUNT;
  PERFORM set_config('substrate.is_admin', saved, true);
  RETURN n;
EXCEPTION WHEN OTHERS THEN
  PERFORM set_config('substrate.is_admin', saved, true);
  RAISE;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION review_apply_preference_status(
  p_body text,
  p_status instruction_status,
  p_scope_ids uuid[],
  p_team_id uuid
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  n bigint;
  saved text := 'false';
BEGIN
  IF p_body IS NULL OR p_status IS NULL OR p_team_id IS NULL THEN
    RETURN 0;
  END IF;
  IF NOT (
    COALESCE(NULLIF(current_setting('substrate.is_admin', true), '')::boolean, false)
    OR EXISTS (
      SELECT 1 FROM membership m
      WHERE m.principal_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
        AND m.team_id = p_team_id
        AND m.role IN ('lead', 'admin')
    )
  ) THEN
    RETURN 0;
  END IF;
  saved := COALESCE(NULLIF(current_setting('substrate.is_admin', true), ''), 'false');
  PERFORM set_config('substrate.is_admin', 'true', true);
  UPDATE preference p
  SET status = p_status, updated_at = now()
  FROM scope s
  WHERE p.scope_id = s.id
    AND p.body = p_body
    AND p.scope_id = ANY(p_scope_ids)
    AND s.team_id = p_team_id;
  GET DIAGNOSTICS n = ROW_COUNT;
  PERFORM set_config('substrate.is_admin', saved, true);
  RETURN n;
EXCEPTION WHEN OTHERS THEN
  PERFORM set_config('substrate.is_admin', saved, true);
  RAISE;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION review_apply_instruction_status(text, instruction_status, uuid[], uuid) OWNER TO substrate_migrate;
ALTER FUNCTION review_apply_preference_status(text, instruction_status, uuid[], uuid) OWNER TO substrate_migrate;
REVOKE ALL ON FUNCTION review_apply_instruction_status(text, instruction_status, uuid[], uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION review_apply_preference_status(text, instruction_status, uuid[], uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION review_apply_instruction_status(text, instruction_status, uuid[], uuid) TO substrate_app;
GRANT EXECUTE ON FUNCTION review_apply_preference_status(text, instruction_status, uuid[], uuid) TO substrate_app;

-- +goose Down

DROP FUNCTION IF EXISTS review_apply_preference_status(text, instruction_status, uuid[], uuid);
DROP FUNCTION IF EXISTS review_apply_instruction_status(text, instruction_status, uuid[], uuid);
