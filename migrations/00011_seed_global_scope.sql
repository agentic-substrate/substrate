-- +goose Up

-- The global scope is the root of every chain, and every read path resolves it
-- (internal/rest/resolve.go globalPath). Nothing created it outside the test
-- fixtures, so a fresh install answered 500 on /v1/render -- the probe
-- `substrate auth login` makes -- while CI stayed green. The id is generated and
-- the path is filled by scope_chain_enforce; scope_single_global guarantees the
-- NOT EXISTS guard cannot race into a second row.
INSERT INTO scope (id, kind, parent_id, key, depth, path)
SELECT gen_random_uuid(), 'global', NULL, '', 0, 'placeholder'
WHERE NOT EXISTS (SELECT 1 FROM scope WHERE kind = 'global');

-- +goose Down

-- Deliberately empty. Deleting the root would orphan every org scope and every
-- row attached to the global chain (Gotcha 6); 00002's Down drops the table.
SELECT 1;
