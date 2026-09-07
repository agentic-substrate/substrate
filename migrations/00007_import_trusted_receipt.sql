-- +goose Up

-- Import-trusted receipts are per-scope, not per-principal. Tokens are per
-- (principal, machine) (EDD §4.3), so a later host's token cannot see a
-- caller-owned ingest_receipt and would otherwise report "trusted machine
-- must be imported first" — then "fix" it by passing -trusted itself.
-- subject_id is the leaf scope; any principal who can write that scope may
-- read which host already claimed trusted (SYNC-5).

CREATE POLICY import_trusted_read ON ingest_receipt FOR SELECT USING (
  subject_type LIKE 'import_trusted:%'
  AND scope_writable(
    subject_id,
    NULLIF(current_setting('substrate.actor_id', true), '')::uuid
  )
);

-- +goose Down

DROP POLICY IF EXISTS import_trusted_read ON ingest_receipt;
