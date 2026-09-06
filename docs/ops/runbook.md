# Operations runbook

**Last reviewed:** 2026-09-05 · **Re-read cadence:** after every incident, and at each phase exit

Substrate is a homelab single-node deployment behind Tailscale. There is no HA and none is
planned; adapters are designed to survive a 24-hour server outage without data loss, and that
is the availability story.

## Deployment topology

| Component | Where | Notes |
|---|---|---|
| PostgreSQL 16 + pgvector, pg_trgm, ltree | CloudNativePG cluster `substrate-pg`, PVC on Longhorn | Roles: `substrate_migrate` (DDL), `substrate_app` (RLS-enforced DML, **no** `BYPASSRLS`) |
| `substrate-server` | k8s Deployment, 1 replica, image built with `ko` | Config via ConfigMap + Secret; `/readyz` probe |
| Ollama (`nomic-embed-text`) | Existing T4 node, `nodeSelector: gpu=t4` | Called directly from Go in Phase 1 |
| MinIO | Existing | Buckets `substrate-sessions` (SSE, owner-only), `substrate-backups` |
| Ingress | Traefik v3 + cert-manager, `IngressRoute` for `substrate.<tailnet>` | Per-token rate limit on `/mcp` |

## Migrations

Migrations are `goose` SQL files in `migrations/`, applied by `substrate-server` at startup
under a Postgres advisory lock so a second replica cannot double-apply.

- **Forward:** deploy the new image; the server migrates on boot and logs each version.
- **Backward:** every migration ships a `-- +goose Down`. Roll back by deploying the previous
  image *and* running `goose down-to <version>` manually — the server never auto-downgrades.
- Domain tables are never `DELETE`d from; state retires via `status`. A migration that drops a
  domain table is a design change, not a migration.

## Backups and restore

Continuous WAL archiving plus a daily 03:00 base backup via CloudNativePG barman-cloud to
`substrate-backups`, SSE-encrypted, 30-day retention, PITR enabled.

**Restore:** create a `Cluster` with `bootstrap.recovery` pointing at the object store and the
target timestamp, then repoint `substrate-server` at the new cluster.

**The restore is tested, not assumed:** a weekly CronJob restores to a scratch cluster and
asserts row counts on `memory`, `instruction`, and `audit`. A backup that has never been
restored is not a backup.

## Alerts (Phase 1 minimum)

| Alert | Threshold | What it usually means |
|---|---|---|
| Outbox depth | > 500 on any machine for > 30 min | Adapter cannot reach the server, or a write is being rejected in a loop |
| `/readyz` failing | > 5 min | Postgres is down or the pool is exhausted |
| Drift proposals | > 10/day | Someone is hand-editing generated files — usually a missing instruction key, not misbehavior |

## Failure modes

| Failure | Behavior | Recovery |
|---|---|---|
| Server down | Adapter serves already-rendered config; `context.get` falls back to the local FTS cache; writes queue in the outbox | Automatic on reconnect |
| Postgres down | `/readyz` fails, server returns 503, adapters treat it as "server down" | Restart; restore if needed |
| Ollama down | Memories store with `embedding NULL`; search degrades to FTS/trigram | Backfill job |
| Skills repo unreachable | Adapter keeps the last linked versions; `/readyz` is unaffected by design | Automatic |
| Bad instruction rendered everywhere | — | `substrate review revert <audit-id>` → broadcast → all machines re-render within 5 min |
| Token leaked | — | `substrate token revoke`; the audit log shows every use by `request_id` |
