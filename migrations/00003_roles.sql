-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'substrate_migrate') THEN
    CREATE ROLE substrate_migrate NOLOGIN NOSUPERUSER NOBYPASSRLS;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'substrate_app') THEN
    CREATE ROLE substrate_app NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE;
  ELSE
    ALTER ROLE substrate_app NOBYPASSRLS;
  END IF;
END $$;
-- +goose StatementEnd

GRANT substrate_migrate TO CURRENT_USER;
GRANT substrate_app TO CURRENT_USER;

-- +goose StatementBegin
DO $$
DECLARE
  t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'principal','org','team','membership','scope','repo_remote','workspace','project_grant','api_token',
    'instruction','preference','memory','ingest_receipt','memory_edge','memory_feedback',
    'skill','skill_version','review_item','audit'
  ]
  LOOP
    EXECUTE format('ALTER TABLE %I OWNER TO substrate_migrate', t);
  END LOOP;
END $$;
-- +goose StatementEnd

ALTER FUNCTION scope_chain_enforce() OWNER TO substrate_migrate;
ALTER FUNCTION preference_scope_kind() OWNER TO substrate_migrate;
ALTER FUNCTION memory_feedback_apply() OWNER TO substrate_migrate;
ALTER FUNCTION skill_active_version_approved() OWNER TO substrate_migrate;
ALTER FUNCTION skill_version_keep_active_approved() OWNER TO substrate_migrate;
ALTER FUNCTION memory_identifiers_text(text[]) OWNER TO substrate_migrate;
ALTER FUNCTION memory_verification_type(jsonb) OWNER TO substrate_migrate;
ALTER FUNCTION audit_row() OWNER TO substrate_migrate;
ALTER SEQUENCE audit_id_seq OWNER TO substrate_migrate;

GRANT USAGE ON SCHEMA public TO substrate_app;
GRANT SELECT, INSERT, UPDATE ON
  principal, org, team, membership, scope, repo_remote, workspace, project_grant, api_token,
  instruction, preference, memory, ingest_receipt, memory_edge, memory_feedback,
  skill, skill_version, review_item
  TO substrate_app;
REVOKE DELETE ON
  principal, org, team, membership, scope, repo_remote, workspace, project_grant, api_token,
  instruction, preference, memory, ingest_receipt, memory_edge, memory_feedback,
  skill, skill_version, review_item
  FROM substrate_app;
GRANT SELECT, INSERT ON audit TO substrate_app;
REVOKE UPDATE, DELETE ON audit FROM substrate_app;
GRANT USAGE, SELECT ON SEQUENCE audit_id_seq TO substrate_app;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO substrate_app;

-- +goose StatementBegin
DO $$
DECLARE
  t text;
BEGIN
  FOR t IN SELECT typname FROM pg_type WHERE typnamespace = 'public'::regnamespace AND typtype = 'e'
  LOOP
    EXECUTE format('GRANT USAGE ON TYPE %I TO substrate_app', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down

DROP OWNED BY substrate_app;
REASSIGN OWNED BY substrate_migrate TO CURRENT_USER;
DROP ROLE IF EXISTS substrate_app;
DROP ROLE IF EXISTS substrate_migrate;
