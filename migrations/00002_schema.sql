-- +goose Up

CREATE TYPE visibility     AS ENUM ('owner','team','org','global');
CREATE TYPE principal_kind AS ENUM ('user','agent');
CREATE TYPE trust_level    AS ENUM ('human_admin','human','agent_interactive','agent_autonomous');
CREATE TYPE member_role   AS ENUM ('member','lead','admin');
CREATE TYPE scope_kind    AS ENUM ('global','org','team','project','repo','branch','task','session','user');

CREATE TABLE principal (
  id            uuid PRIMARY KEY,
  kind          principal_kind NOT NULL,
  display_name  text NOT NULL,
  trust         trust_level NOT NULL,
  minted_by     uuid REFERENCES principal(id),
  disabled_at   timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  CHECK (kind <> 'agent' OR minted_by IS NOT NULL)
);

CREATE TABLE org  (id uuid PRIMARY KEY, name text UNIQUE NOT NULL);
CREATE TABLE team (id uuid PRIMARY KEY, org_id uuid NOT NULL REFERENCES org(id), name text NOT NULL, UNIQUE(org_id,name));

CREATE TABLE membership (
  principal_id uuid NOT NULL REFERENCES principal(id),
  team_id      uuid NOT NULL REFERENCES team(id),
  role         member_role NOT NULL,
  PRIMARY KEY (principal_id, team_id)
);

CREATE TABLE scope (
  id         uuid PRIMARY KEY,
  kind       scope_kind NOT NULL,
  parent_id  uuid REFERENCES scope(id),
  key        text NOT NULL,
  team_id    uuid REFERENCES team(id),
  depth      smallint NOT NULL,
  path       ltree NOT NULL,
  UNIQUE NULLS NOT DISTINCT (parent_id, kind, key)
);
CREATE INDEX scope_path_gist ON scope USING gist (path);
CREATE UNIQUE INDEX scope_single_global ON scope ((true)) WHERE kind='global';

CREATE TABLE repo_remote (
  remote_norm   text PRIMARY KEY,
  repo_scope_id uuid NOT NULL REFERENCES scope(id),
  created_by    uuid NOT NULL REFERENCES principal(id),
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE workspace (
  id              uuid PRIMARY KEY,
  repo_scope_id   uuid NOT NULL REFERENCES scope(id),
  branch_scope_id uuid NOT NULL REFERENCES scope(id),
  principal_id    uuid NOT NULL REFERENCES principal(id),
  machine         text NOT NULL,
  worktree_path   text NOT NULL,
  git_sha         text,
  dirty           boolean NOT NULL DEFAULT false,
  last_seen_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (machine, worktree_path)
);

CREATE TABLE project_grant (
  project_scope_id uuid NOT NULL REFERENCES scope(id),
  team_id          uuid NOT NULL REFERENCES team(id),
  role             member_role NOT NULL,
  PRIMARY KEY (project_scope_id, team_id)
);

CREATE TABLE api_token (
  id           uuid PRIMARY KEY,
  principal_id uuid NOT NULL REFERENCES principal(id),
  machine      text NOT NULL,
  token_hash   bytea NOT NULL UNIQUE,
  scopes       text[] NOT NULL,
  expires_at   timestamptz,
  revoked_at   timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE FUNCTION scope_chain_enforce() RETURNS trigger AS $$
DECLARE
  parent scope%ROWTYPE;
  label  text;
  pred   scope_kind;
BEGIN
  -- Labels are UUID-derived so human keys may contain '/', '.', or spaces (EDD R23).
  label := 'k' || replace(NEW.id::text, '-', '');
  NEW.depth := CASE NEW.kind
    WHEN 'global' THEN 0
    WHEN 'org' THEN 1
    WHEN 'team' THEN 2
    WHEN 'project' THEN 3
    WHEN 'repo' THEN 4
    WHEN 'branch' THEN 5
    WHEN 'task' THEN 6
    WHEN 'session' THEN 7
    WHEN 'user' THEN 0
  END;

  IF NEW.kind = 'user' THEN
    IF NEW.parent_id IS NOT NULL THEN
      RAISE EXCEPTION 'user scope cannot have a parent';
    END IF;
    NEW.path := label::ltree;
    RETURN NEW;
  END IF;

  IF NEW.kind = 'global' THEN
    IF NEW.parent_id IS NOT NULL THEN
      RAISE EXCEPTION 'global scope cannot have a parent';
    END IF;
    NEW.path := label::ltree;
    RETURN NEW;
  END IF;

  IF NEW.parent_id IS NULL THEN
    RAISE EXCEPTION 'scope kind % requires a parent', NEW.kind;
  END IF;

  SELECT * INTO parent FROM scope WHERE id = NEW.parent_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'parent scope % not found', NEW.parent_id;
  END IF;

  IF parent.kind = 'user' THEN
    RAISE EXCEPTION 'user scope cannot have children';
  END IF;

  pred := CASE NEW.kind
    WHEN 'org' THEN 'global'::scope_kind
    WHEN 'team' THEN 'org'::scope_kind
    WHEN 'project' THEN 'team'::scope_kind
    WHEN 'repo' THEN 'project'::scope_kind
    WHEN 'branch' THEN 'repo'::scope_kind
    WHEN 'task' THEN 'branch'::scope_kind
    WHEN 'session' THEN 'task'::scope_kind
  END;

  IF parent.kind IS DISTINCT FROM pred THEN
    RAISE EXCEPTION 'parent kind % is not the immediate predecessor of %', parent.kind, NEW.kind;
  END IF;

  NEW.path := parent.path || label::ltree;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER scope_chain BEFORE INSERT OR UPDATE ON scope
  FOR EACH ROW EXECUTE FUNCTION scope_chain_enforce();

CREATE TYPE instruction_kind   AS ENUM ('rule','constraint','convention');
CREATE TYPE instruction_status AS ENUM ('active','proposed','retired');

CREATE TABLE instruction (
  id uuid PRIMARY KEY,
  scope_id uuid NOT NULL REFERENCES scope(id),
  visibility visibility NOT NULL, owner_id uuid NOT NULL REFERENCES principal(id),
  kind instruction_kind NOT NULL,
  key  text NOT NULL,
  body text NOT NULL,
  status instruction_status NOT NULL DEFAULT 'active',
  superseded_by uuid REFERENCES instruction(id),
  created_by uuid NOT NULL REFERENCES principal(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX instruction_active_key ON instruction (scope_id, key) WHERE status='active';

CREATE TABLE preference (
  id uuid PRIMARY KEY,
  scope_id uuid NOT NULL REFERENCES scope(id),
  visibility visibility NOT NULL, owner_id uuid NOT NULL REFERENCES principal(id),
  key text NOT NULL, body text NOT NULL,
  status instruction_status NOT NULL DEFAULT 'active',
  created_by uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX preference_active_key ON preference (scope_id, key) WHERE status='active';

-- +goose StatementBegin
CREATE FUNCTION preference_scope_kind() RETURNS trigger AS $$
DECLARE
  k scope_kind;
BEGIN
  SELECT kind INTO k FROM scope WHERE id = NEW.scope_id;
  IF k NOT IN ('user', 'team', 'org') THEN
    RAISE EXCEPTION 'preference scope must be user, team, or org';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER preference_scope BEFORE INSERT OR UPDATE OF scope_id ON preference
  FOR EACH ROW EXECUTE FUNCTION preference_scope_kind();

CREATE TYPE memory_tier   AS ENUM ('working','episodic','semantic');
CREATE TYPE memory_kind   AS ENUM ('fact','decision','incident','lesson','observation');
CREATE TYPE memory_status AS ENUM ('confirmed','probable','unverified','conflicted','deprecated','superseded');
CREATE TYPE verification_type AS ENUM ('code','human','agent_inference');

-- jsonb ->> and enum casts are STABLE; wrap so the generated column (EDD R4) can be STORED.
-- +goose StatementBegin
CREATE FUNCTION memory_verification_type(j jsonb) RETURNS verification_type
LANGUAGE sql IMMUTABLE STRICT AS $$
  SELECT (j->>'type')::verification_type
$$;
-- +goose StatementEnd


CREATE TABLE memory (
  id uuid PRIMARY KEY,
  scope_id uuid NOT NULL REFERENCES scope(id),
  visibility visibility NOT NULL, owner_id uuid NOT NULL REFERENCES principal(id),
  tier memory_tier NOT NULL, kind memory_kind NOT NULL,
  title text NOT NULL, body text NOT NULL,
  identifiers text[] NOT NULL DEFAULT '{}',
  embedding vector(768),
  fts tsvector GENERATED ALWAYS AS (to_tsvector('english'::regconfig, title || ' ' || body)) STORED,
  status memory_status NOT NULL DEFAULT 'unverified',
  superseded_by uuid REFERENCES memory(id),
  source jsonb NOT NULL,
  verification jsonb NOT NULL,
  verification_type verification_type GENERATED ALWAYS AS (memory_verification_type(verification)) STORED,
  valid_from_commit text, last_verified_at timestamptz, stale_reason text,
  retrieved_count int NOT NULL DEFAULT 0, useful_count int NOT NULL DEFAULT 0, incorrect_count int NOT NULL DEFAULT 0,
  created_by uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (jsonb_typeof(source)='object' AND source ? 'machine'),
  CHECK (verification ? 'type')
);
CREATE INDEX memory_embedding_hnsw ON memory USING hnsw (embedding vector_cosine_ops) WITH (m=16, ef_construction=64);
CREATE INDEX memory_fts ON memory USING gin (fts);
-- array_to_string is STABLE; wrap so the trigram GIN index (EDD §3.4) can be built.
-- +goose StatementBegin
CREATE FUNCTION memory_identifiers_text(ids text[]) RETURNS text
LANGUAGE sql IMMUTABLE AS $$ SELECT array_to_string(ids, ' ') $$;
-- +goose StatementEnd
CREATE INDEX memory_identifiers_trgm ON memory USING gin (memory_identifiers_text(identifiers) gin_trgm_ops);
CREATE INDEX memory_default_read ON memory (scope_id, tier, status) WHERE tier='semantic' AND status IN ('confirmed','probable');

CREATE TABLE ingest_receipt (
  client_id    uuid PRIMARY KEY,
  principal_id uuid NOT NULL REFERENCES principal(id),
  subject_type text NOT NULL,
  subject_id   uuid NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE edge_relation AS ENUM ('supersedes','contradicts','caused_by','relates_to','uses_skill','derived_from');
CREATE TABLE memory_edge (
  from_id uuid NOT NULL REFERENCES memory(id), to_id uuid NOT NULL REFERENCES memory(id),
  relation edge_relation NOT NULL, created_by uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (from_id, to_id, relation)
);

CREATE TABLE memory_feedback (
  id uuid PRIMARY KEY, memory_id uuid NOT NULL REFERENCES memory(id),
  principal_id uuid NOT NULL REFERENCES principal(id), trust trust_level NOT NULL,
  useful boolean, incorrect boolean, reason text, session_id text,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (useful IS NOT NULL OR incorrect IS NOT NULL)
);

-- useful_count/incorrect_count stay int; accumulate in hundredths so
-- agent_autonomous 0.25 is not rounded to 0. Weight comes from principal.trust
-- so an inserter cannot spoof NEW.trust.
-- +goose StatementBegin
CREATE FUNCTION memory_feedback_apply() RETURNS trigger AS $$
DECLARE
  lvl trust_level;
  hundredths int;
BEGIN
  SELECT trust INTO lvl FROM principal WHERE id = NEW.principal_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'principal % not found', NEW.principal_id;
  END IF;
  hundredths := CASE lvl
    WHEN 'human_admin' THEN 300
    WHEN 'human' THEN 200
    WHEN 'agent_interactive' THEN 100
    WHEN 'agent_autonomous' THEN 25
  END;
  UPDATE memory SET
    useful_count = useful_count + CASE WHEN NEW.useful THEN hundredths ELSE 0 END,
    incorrect_count = incorrect_count + CASE WHEN NEW.incorrect THEN hundredths ELSE 0 END,
    updated_at = now()
  WHERE id = NEW.memory_id;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER memory_feedback_apply AFTER INSERT ON memory_feedback
  FOR EACH ROW EXECUTE FUNCTION memory_feedback_apply();

CREATE TYPE approval AS ENUM ('proposed','approved','rejected');
CREATE TABLE skill (
  id uuid PRIMARY KEY, name text NOT NULL,
  scope_id uuid NOT NULL REFERENCES scope(id),
  visibility visibility NOT NULL, owner_id uuid NOT NULL,
  description text NOT NULL, tags text[] NOT NULL DEFAULT '{}', embedding vector(768),
  active_version_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (name)
);
CREATE TABLE skill_version (
  id uuid PRIMARY KEY, skill_id uuid NOT NULL REFERENCES skill(id),
  semver text NOT NULL, git_sha text NOT NULL, git_path text NOT NULL,
  author_id uuid NOT NULL, reason text, source_task text,
  approval approval NOT NULL DEFAULT 'proposed',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (skill_id, semver)
);
ALTER TABLE skill ADD FOREIGN KEY (active_version_id) REFERENCES skill_version(id);

-- +goose StatementBegin
CREATE FUNCTION skill_active_version_approved() RETURNS trigger AS $$
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

CREATE TRIGGER skill_active_version BEFORE INSERT OR UPDATE OF active_version_id ON skill
  FOR EACH ROW EXECUTE FUNCTION skill_active_version_approved();

-- +goose StatementBegin
CREATE FUNCTION skill_version_keep_active_approved() RETURNS trigger AS $$
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

CREATE TRIGGER skill_version_keep_active_approved
  BEFORE UPDATE OF approval ON skill_version
  FOR EACH ROW EXECUTE FUNCTION skill_version_keep_active_approved();

CREATE TYPE review_kind   AS ENUM ('promotion','instruction_change','skill_proposal','contradiction','drift_proposal','import_conflict');
CREATE TYPE review_status AS ENUM ('open','approved','rejected','withdrawn');
CREATE TABLE review_item (
  id uuid PRIMARY KEY, kind review_kind NOT NULL,
  scope_id uuid NOT NULL REFERENCES scope(id), team_id uuid REFERENCES team(id),
  payload jsonb NOT NULL,
  proposed_by uuid NOT NULL REFERENCES principal(id),
  status review_status NOT NULL DEFAULT 'open',
  decided_by uuid REFERENCES principal(id), decided_at timestamptz, reason text,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX review_open ON review_item (team_id, kind) WHERE status='open';

CREATE TABLE audit (
  id bigserial PRIMARY KEY, at timestamptz NOT NULL DEFAULT now(),
  actor_id uuid, action text NOT NULL,
  subject_type text NOT NULL, subject_id uuid NOT NULL, scope_id uuid,
  before jsonb, after jsonb, reason text, request_id text
);
CREATE INDEX audit_subject ON audit (subject_type, subject_id, at DESC);
CREATE INDEX audit_scope_at ON audit (scope_id, at DESC);

-- +goose StatementBegin
CREATE FUNCTION audit_row() RETURNS trigger AS $$
DECLARE
  rec jsonb;
  subj uuid;
  sc uuid;
  actor uuid;
  req text;
BEGIN
  rec := to_jsonb(NEW);
  subj := COALESCE(
    (rec->>'id')::uuid,
    (rec->>'client_id')::uuid,
    (rec->>'principal_id')::uuid,
    (rec->>'from_id')::uuid,
    (rec->>'project_scope_id')::uuid,
    (rec->>'repo_scope_id')::uuid
  );
  sc := COALESCE((rec->>'scope_id')::uuid, (rec->>'repo_scope_id')::uuid);
  actor := NULLIF(current_setting('substrate.actor_id', true), '')::uuid;
  req := NULLIF(current_setting('substrate.request_id', true), '');

  INSERT INTO audit (actor_id, action, subject_type, subject_id, scope_id, before, after, request_id)
  VALUES (
    actor,
    TG_TABLE_NAME || '.' || lower(TG_OP),
    TG_TABLE_NAME,
    subj,
    sc,
    CASE WHEN TG_OP = 'UPDATE' THEN to_jsonb(OLD) END,
    rec,
    req
  );
  PERFORM pg_notify('substrate_audit', json_build_object(
    'action', TG_TABLE_NAME || '.' || lower(TG_OP),
    'subject_type', TG_TABLE_NAME,
    'subject_id', subj,
    'request_id', req
  )::text);
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE
  t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'principal','org','team','membership','scope','repo_remote','workspace','project_grant','api_token',
    'instruction','preference','memory','ingest_receipt','memory_edge','memory_feedback',
    'skill','skill_version','review_item'
  ]
  LOOP
    EXECUTE format(
      'CREATE TRIGGER audit_%I AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION audit_row()',
      t, t
    );
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down

DROP TABLE IF EXISTS audit CASCADE;
DROP TABLE IF EXISTS review_item CASCADE;
DROP TABLE IF EXISTS skill_version CASCADE;
DROP TABLE IF EXISTS skill CASCADE;
DROP TABLE IF EXISTS memory_feedback CASCADE;
DROP TABLE IF EXISTS memory_edge CASCADE;
DROP TABLE IF EXISTS ingest_receipt CASCADE;
DROP TABLE IF EXISTS memory CASCADE;
DROP TABLE IF EXISTS preference CASCADE;
DROP TABLE IF EXISTS instruction CASCADE;
DROP TABLE IF EXISTS api_token CASCADE;
DROP TABLE IF EXISTS project_grant CASCADE;
DROP TABLE IF EXISTS workspace CASCADE;
DROP TABLE IF EXISTS repo_remote CASCADE;
DROP TABLE IF EXISTS membership CASCADE;
DROP TABLE IF EXISTS scope CASCADE;
DROP TABLE IF EXISTS team CASCADE;
DROP TABLE IF EXISTS org CASCADE;
DROP TABLE IF EXISTS principal CASCADE;

DROP FUNCTION IF EXISTS audit_row() CASCADE;
DROP FUNCTION IF EXISTS skill_version_keep_active_approved() CASCADE;
DROP FUNCTION IF EXISTS skill_active_version_approved() CASCADE;
DROP FUNCTION IF EXISTS memory_feedback_apply() CASCADE;
DROP FUNCTION IF EXISTS preference_scope_kind() CASCADE;
DROP FUNCTION IF EXISTS memory_identifiers_text(text[]) CASCADE;
DROP FUNCTION IF EXISTS memory_verification_type(jsonb) CASCADE;
DROP FUNCTION IF EXISTS scope_chain_enforce() CASCADE;

DROP TYPE IF EXISTS review_status;
DROP TYPE IF EXISTS review_kind;
DROP TYPE IF EXISTS approval;
DROP TYPE IF EXISTS edge_relation;
DROP TYPE IF EXISTS verification_type;
DROP TYPE IF EXISTS memory_status;
DROP TYPE IF EXISTS memory_kind;
DROP TYPE IF EXISTS memory_tier;
DROP TYPE IF EXISTS instruction_status;
DROP TYPE IF EXISTS instruction_kind;
DROP TYPE IF EXISTS scope_kind;
DROP TYPE IF EXISTS member_role;
DROP TYPE IF EXISTS trust_level;
DROP TYPE IF EXISTS principal_kind;
DROP TYPE IF EXISTS visibility;
