-- +goose Up

-- R6: active_version_id may only point at an approved version. Sequential
-- updates already raise; without row locks a concurrent
-- UPDATE skill SET active_version_id = v and
-- UPDATE skill_version SET approval = 'rejected' WHERE id = v can both
-- commit under READ COMMITTED, leaving a rejected active version.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION skill_active_version_approved() RETURNS trigger AS $$
DECLARE
  ap approval;
BEGIN
  IF NEW.active_version_id IS NULL THEN
    RETURN NEW;
  END IF;
  SELECT approval INTO ap FROM skill_version WHERE id = NEW.active_version_id AND skill_id = NEW.id FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'active_version_id must belong to this skill';
  END IF;
  IF ap <> 'approved' THEN
    RAISE EXCEPTION 'active_version_id must point at an approved version';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION skill_version_keep_active_approved() RETURNS trigger AS $$
BEGIN
  PERFORM 1 FROM skill WHERE id = NEW.skill_id FOR UPDATE;
  IF EXISTS (
    SELECT 1 FROM skill
    WHERE active_version_id = NEW.id AND id = NEW.skill_id
  ) AND NEW.approval IS DISTINCT FROM 'approved' THEN
    RAISE EXCEPTION 'cannot change approval of an active skill version away from approved';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

ALTER FUNCTION skill_active_version_approved() OWNER TO substrate_migrate;
ALTER FUNCTION skill_version_keep_active_approved() OWNER TO substrate_migrate;
GRANT EXECUTE ON FUNCTION skill_active_version_approved() TO substrate_app;
GRANT EXECUTE ON FUNCTION skill_version_keep_active_approved() TO substrate_app;

-- +goose Down

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION skill_active_version_approved() RETURNS trigger AS $$
DECLARE
  ap approval;
BEGIN
  IF NEW.active_version_id IS NULL THEN
    RETURN NEW;
  END IF;
  SELECT approval INTO ap FROM skill_version WHERE id = NEW.active_version_id AND skill_id = NEW.id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'active_version_id must belong to this skill';
  END IF;
  IF ap <> 'approved' THEN
    RAISE EXCEPTION 'active_version_id must point at an approved version';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION skill_version_keep_active_approved() RETURNS trigger AS $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM skill
    WHERE active_version_id = NEW.id AND id = NEW.skill_id
  ) AND NEW.approval IS DISTINCT FROM 'approved' THEN
    RAISE EXCEPTION 'cannot change approval of an active skill version away from approved';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

ALTER FUNCTION skill_active_version_approved() OWNER TO substrate_migrate;
ALTER FUNCTION skill_version_keep_active_approved() OWNER TO substrate_migrate;
GRANT EXECUTE ON FUNCTION skill_active_version_approved() TO substrate_app;
GRANT EXECUTE ON FUNCTION skill_version_keep_active_approved() TO substrate_app;
