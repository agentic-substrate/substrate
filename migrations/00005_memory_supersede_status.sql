-- +goose Up

-- Owners still cannot edit status, except the system transition that marks a
-- row superseded in the same statement that sets superseded_by. Nested IFs:
-- plpgsql evaluates AND fully, and instruction/preference status enums have
-- no 'superseded' value.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION content_freeze_cols() RETURNS trigger
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
  IF TG_TABLE_NAME IN ('instruction', 'preference', 'memory') THEN
    IF NEW.status IS DISTINCT FROM OLD.status THEN
      IF TG_TABLE_NAME = 'memory' THEN
        IF NEW.status = 'superseded'
           AND NEW.superseded_by IS NOT NULL
           AND OLD.superseded_by IS NULL THEN
          RETURN NEW;
        END IF;
      END IF;
      RETURN NULL;
    END IF;
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION content_freeze_cols() OWNER TO substrate_migrate;
GRANT EXECUTE ON FUNCTION content_freeze_cols() TO substrate_app;

-- +goose Down

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION content_freeze_cols() RETURNS trigger
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
