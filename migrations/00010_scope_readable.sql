-- +goose Up

-- The read side of scope_writable. /v1/render, /v1/skills/manifest and
-- /v1/memory/cache resolve a repo key to its chain and never write, so the
-- RLS 42501 that refuses a foreign repo on a write never fires: the key
-- resolved for anyone and the status code alone enumerated which repo keys
-- this control plane binds (#109; AGENTS.md Gotcha 13).
--
-- The read paths do not refuse on a false result. resolveReadableRepoPaths
-- returns no chain, and the handler falls back to the global chain and answers
-- 200 with the byte-identical body that request would produce with no ?repos=
-- at all. A repo bound to another team and a repo bound nowhere therefore give
-- the same answer, which is what kills the oracle -- not that both are denied.
--
-- That fallback only preserves content the global chain itself reaches. On
-- /v1/memory/cache, whose no-?repos= path applies no scope filter and relies
-- on RLS alone, that is every visibility='global' row regardless of where it
-- was authored. On /v1/render and /v1/skills/manifest, whose no-?repos= path
-- still resolves a scoped (global-only) chain, a visibility='global' row
-- authored below global -- reachable only through the named repo's chain --
-- is withheld from a caller who cannot read that chain, exactly as a 403
-- would have withheld it; the fallback changes the status code there, not the
-- content. Serving that below-global content to such a caller would require
-- querying the chain to find it, which reopens the oracle -- so the remaining
-- withholding on those two endpoints is inherent to the design, not a
-- shortfall of it. The write paths still refuse: h.repoScope keeps its
-- scope_writable check and its masked 403.
--
-- scope_writable is the wrong predicate to reuse by name on a read path -- a
-- principal that may read but not write must not be dropped to the global
-- chain -- but it is the right predicate by extension today: member_role is
-- ('member','lead','admin') and scope_writable already admits every
-- project_grant role, so no grant confers read without write. Delegating keeps
-- the two exactly in step now and gives a read-only role, if one is ever added,
-- one function to widen instead of a read path that silently stayed at write
-- strength.
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
