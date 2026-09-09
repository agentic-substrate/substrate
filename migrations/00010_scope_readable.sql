-- +goose Up

-- The read side of scope_writable. /v1/render, /v1/skills/manifest and
-- /v1/memory/cache resolve a repo key to its chain and never write, so the
-- RLS 42501 that refuses a foreign repo on a write never fires: the key
-- resolved for anyone and the status code alone enumerated which repo keys
-- this control plane binds (#109; AGENTS.md Gotcha 13).
--
-- scope_writable is the wrong predicate to reuse by name on a read path -- a
-- principal that may read but not write must not be refused -- but it is the
-- right predicate by extension today: member_role is ('member','lead','admin')
-- and scope_writable already admits every project_grant role, so no grant
-- confers read without write. Delegating keeps the two exactly in step now and
-- gives a read-only role, if one is ever added, one function to widen instead
-- of a read path that silently stayed at write strength.
--
-- Delegation also inherits the substrate.is_admin short-circuit, so a
-- human_admin resolves every repo key exactly as it does today.
-- +goose StatementBegin
CREATE FUNCTION scope_readable(p_scope_id uuid, p_actor uuid) RETURNS boolean
LANGUAGE sql STABLE PARALLEL SAFE SET search_path = public AS $$
  SELECT scope_writable(p_scope_id, p_actor)
$$;
-- +goose StatementEnd

ALTER FUNCTION scope_readable(uuid, uuid) OWNER TO substrate_migrate;
GRANT EXECUTE ON FUNCTION scope_readable(uuid, uuid) TO substrate_app;

-- +goose Down

DROP FUNCTION scope_readable(uuid, uuid);
