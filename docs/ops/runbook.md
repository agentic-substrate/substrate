# Operations runbook

**Last reviewed:** 2026-09-06 · **Re-read cadence:** after every incident, and at each phase exit

Substrate is a homelab single-node deployment behind Tailscale. There is no HA and none is
planned; adapters are designed to survive a 24-hour server outage without data loss, and that
is the availability story.

## Deployment topology

| Component | Where | Notes |
|---|---|---|
| PostgreSQL 16 + pgvector, pg_trgm, ltree | CloudNativePG cluster `substrate-pg`, PVC on Longhorn | Roles: `substrate_migrate` (DDL), `substrate_app` (RLS-enforced DML, **no** `BYPASSRLS`) |
| `substrate-server` | k8s Deployment, 1 replica, image built with `ko` | Config via ConfigMap + Secret; `/readyz` probe; `-otlp` / `SUBSTRATE_OTLP_ENDPOINT` |
| Ollama (`nomic-embed-text`) | Existing T4 node, `nodeSelector: gpu=t4` | Called directly from Go in Phase 1 |
| OTel collector | Existing; endpoint still open (EDD §19 item 5) | Empty `-otlp` disables export. Phase 1 rules: `deploy/alerts.yaml` |
| MinIO | Existing | Buckets `substrate-sessions` (SSE, owner-only), `substrate-backups` |
| Ingress | Traefik v3 + cert-manager, `IngressRoute` for `substrate.<tailnet>` | Per-token rate limit on `/mcp` |

## Migrations

Migrations are `goose` SQL files in `migrations/`, applied by `substrate-server` at startup
under a Postgres advisory lock (`pg_try_advisory_lock` via goose's session locker) so a second
replica cannot double-apply.

What has shipped:

| Version | File | What it does |
|---|---|---|
| 1 | `00001_extensions.sql` | `vector`, `pg_trgm`, `ltree` |
| 2 | `00002_schema.sql` | EDD §3 identity, scopes (ltree `path`, chain trigger), instructions, memory, skills, review, audit (INSERT-only trigger + `pg_notify('substrate_audit')`) |
| 3 | `00003_roles.sql` | `substrate_migrate` owns the tables; `substrate_app` is DML-only, **no** `BYPASSRLS`, no `DELETE` on domain tables, `REVOKE UPDATE, DELETE` on `audit`. The request pool assumes `substrate_app` on acquire |
| 4 | `00004_rls.sql` | `ENABLE` + `FORCE ROW LEVEL SECURITY` on `instruction`, `preference`, `memory`, `skill`, `review_item`, `skill_version`, `memory_edge`, `memory_feedback`, `ingest_receipt`. Child-table policies follow the parent row (`skill` / `memory` / `principal_id`). `scope_writable` treats `kind='user'` as the caller's own scope (`scope.key = actor_id`). `review_item` UPDATE is withdraw (proposer, `open`→`withdrawn`) or decide (`human_admin` / team lead). Frozen columns (`status`, `visibility`, `owner_id`, `scope_id`) are immutable except for `human_admin`. Session GUCs are `substrate.actor_id`, `substrate.team_ids`, `substrate.org_id`, `substrate.granted_project_ids`, `substrate.is_admin`. |
| 5 | `00005_memory_supersede_status.sql` | Replaces `content_freeze_cols` so a non-admin `memory` UPDATE may set `status='superseded'` when `superseded_by` is set in the same statement (system supersession, not a user edit). Visibility, owner_id, scope_id, and every other status change stay frozen. Without this, superseded rows keep `status='confirmed'` and still match `memory_default_read`. |
| 6 | `00006_skill_active_version_lock.sql` | `skill_active_version_approved` `SELECT ... FOR UPDATE`s the target `skill_version` row; `skill_version_keep_active_approved` locks the parent `skill` row. Without those locks a concurrent activate and reject can both commit under READ COMMITTED and leave `active_version_id` pointing at a rejected version (R6). |

The bootstrap DSN must be able to `CREATE EXTENSION` and `CREATE ROLE`. Migrations run on
that connection; the request pool then `SET SESSION AUTHORIZATION` / `SET ROLE` to
`substrate_app` so REVOKEs on `DELETE` and on `audit` actually bind (GOV-1, gotcha 6).
A Postgres outage must not kill the process: `/healthz` stays 200, `/readyz` is 503, and
the server retries `store.Open` in the background until the pool pings.

Frozen columns (`visibility`, `owner_id`, `scope_id`, and `status`) stay immutable for
non-admins. The one exception, from version 5, is a `memory` UPDATE that sets
`status='superseded'` in the same statement as `superseded_by` — a system transition,
not a user edit.

- **Forward:** deploy the new image; the server migrates on boot (`-dsn` / `SUBSTRATE_DSN`).
- **Backward:** every migration ships a `-- +goose Down`. Roll back by deploying the previous
  image *and* running `goose down-to <version>` manually — the server never auto-downgrades.
- Domain tables are never `DELETE`d from; state retires via `status`. A migration that drops a
  domain table is a design change, not a migration.
- **`goose down-to` is for an empty CI database only.** The `Down` of an initial-schema migration
  drops the domain tables, so running it against the homelab database destroys every memory,
  instruction, and audit row — and the audit table is the one record that cannot be rebuilt. Once
  a migration has been applied here, the only backward path is a PITR restore (below) or a new
  forward migration.

## Backups and restore

Continuous WAL archiving plus a daily 03:00 base backup via CloudNativePG barman-cloud to
`substrate-backups`, SSE-encrypted, 30-day retention, PITR enabled.

**Restore:** create a `Cluster` with `bootstrap.recovery` pointing at the object store and the
target timestamp, then repoint `substrate-server` at the new cluster.

**The restore is tested, not assumed:** a weekly CronJob restores to a scratch cluster and
asserts row counts on `memory`, `instruction`, and `audit`. A backup that has never been
restored is not a backup.

## Alerts (Phase 1 minimum)

Rules live in [`deploy/alerts.yaml`](../../deploy/alerts.yaml). Metric names are `substrate_*`.
`substrate_outbox_depth` is reported **by the adapter**, per `machine` label, including zero.
A machine that stopped reporting drops the series; do not default missing to 0.

The collector endpoint is `-otlp` / `SUBSTRATE_OTLP_ENDPOINT`. Empty disables export (supported,
not degraded). A collector outage must not fail a request; export is best-effort and a failure
is logged once.

| Alert | Threshold | What it usually means |
|---|---|---|
| Outbox depth (`SubstrateOutboxDepth`) | > 500 on any machine for > 30 min | Adapter cannot reach the server, or a write is being rejected in a loop |
| `/readyz` failing (`SubstrateReadyz`) | > 5 min | Postgres is down or the pool is exhausted |
| Drift proposals (`SubstrateDriftProposals`) | > 10/day | Someone is bypassing the adapter or an instruction key is missing — usually a missing key, not misbehavior. Act on the review queue (`drift_proposal`), not a `scope_path` label. |

## Failure modes

| Failure | Behavior | Recovery |
|---|---|---|
| Server down | Adapter serves already-rendered config; `context.get` falls back to the local FTS cache; writes queue in the outbox | Automatic on reconnect |
| Postgres down | `/healthz` stays 200; `/readyz` is 503; the process retries the store in the background | Automatic when Postgres returns |
| Ollama down | Memories store with `embedding NULL`; search degrades to FTS/trigram | Backfill job |
| Skills repo unreachable | Adapter keeps the last linked versions; `/readyz` is unaffected by design | Automatic |
| Bad instruction rendered everywhere | — | `substrate review revert <audit-id>` → broadcast → all machines re-render within 5 min |
| Token leaked | — | `substrate token revoke`; the audit log shows every use by `request_id` |
