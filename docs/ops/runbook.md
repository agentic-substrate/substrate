# Operations runbook

**Last reviewed:** 2026-09-07 · **Re-read cadence:** after every incident, and at each phase exit

Substrate is a homelab single-node deployment behind Tailscale. There is no HA and none is
planned; adapters are designed to survive a 24-hour server outage without data loss, and that
is the availability story.

## Deployment topology

| Component | Where | Notes |
|---|---|---|
| PostgreSQL 16 + pgvector, pg_trgm, ltree | CloudNativePG cluster `substrate-pg`, PVC on Longhorn | Roles: `substrate_migrate` (DDL), `substrate_app` (RLS-enforced DML, **no** `BYPASSRLS`) |
| `substrate-server` | k8s Deployment, 1 replica, image built with `ko` | Config via ConfigMap + Secret; `/readyz` probe; `-otlp` / `SUBSTRATE_OTLP_ENDPOINT` |
| Ollama (`nomic-embed-text`) | Existing T4 node, `nodeSelector: gpu=t4` | Called directly from Go in Phase 1 |
| OTel collector | In-namespace, [`deploy/otel/collector.yaml`](../../deploy/otel/collector.yaml) | OTLP/HTTP on 4318, Prometheus scrape on 8889. `SUBSTRATE_OTLP_ENDPOINT` points at it; empty still disables export |
| MinIO | Existing | Buckets `substrate-sessions` (SSE, owner-only), `substrate-backups` |
| Ingress | Traefik v3, `IngressRoute` for `substrate.<tailnet>` | Rate limit on `/mcp` (none on `/v1`, deliberately); Tailscale-only entrypoint. TLS is **not** cert-manager — see [TLS for the tailnet host](#tls-for-the-tailnet-host) |
| Manifests | [`deploy/`](../../deploy/) | Kustomize overlay. Secrets are templates with empty values. Postgres PVC is **20Gi** on Longhorn, set explicitly — never omit it |
| Network | [`deploy/networkpolicy.yaml`](../../deploy/networkpolicy.yaml) | Default-deny ingress for the namespace. Only Traefik reaches `substrate-server:8080`; only the server and CNPG reach Postgres |

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
| 7 | `00007_import_trusted_receipt.sql` | Adds `import_trusted_read` on `ingest_receipt`: rows with `subject_type` `import_trusted:%` and `subject_id` equal to a writable leaf scope are visible to any principal who can write that scope, not only the inserting actor. Tokens are per (principal, machine); without this a later host cannot see the trusted-host marker and "fixes" it by passing `-trusted` itself. |
| 8 | `00008_review_apply_status.sql` | `review_apply_instruction_status` and `review_apply_preference_status` (`SECURITY DEFINER`, owned by `substrate_migrate`). A team lead/admin of `p_team_id` (or `human_admin`) may set `status` on rows whose `scope.team_id` matches and whose `scope_id` is in the supplied list. Elevation to `substrate.is_admin` lasts only for that UPDATE so `content_freeze_cols` does not no-op it; the function returns `ROW_COUNT` so a 0- or many-row match fails the decide instead of closing the review item. |

The bootstrap DSN must be able to `CREATE EXTENSION` and `CREATE ROLE`. Migrations run on
that connection; the request pool then `SET SESSION AUTHORIZATION` / `SET ROLE` to
`substrate_app` so REVOKEs on `DELETE` and on `audit` actually bind (GOV-1, gotcha 6).
A Postgres outage must not kill the process: `/healthz` stays 200, `/readyz` is 503, and
the server retries `store.Open` in the background until the pool pings.

Frozen columns (`visibility`, `owner_id`, `scope_id`, and `status`) stay immutable for
non-admins. Exceptions: version 5, a `memory` UPDATE that sets `status='superseded'` in the
same statement as `superseded_by`; version 8, the `review_apply_*_status` functions, which
are the review-decide status write (team lead of that item, one row, no request-wide
`is_admin`).

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
`substrate-backups`, SSE-encrypted, 30-day retention, PITR enabled. Manifests live in
[`deploy/cnpg/`](../../deploy/cnpg/).

**Restore (PITR):** apply [`deploy/cnpg/scratch-cluster.yaml`](../../deploy/cnpg/scratch-cluster.yaml)
(or a copy with `bootstrap.recovery.recoveryTarget.targetTime` set), wait until the Cluster
is Ready, then repoint `substrate-server` at the restored service. For a production failover
the restored cluster is renamed/repointed; the weekly job uses a scratch name
(`substrate-pg-scratch`) and deletes it afterwards so the extra 20Gi PVC does not linger.

**The restore is tested, not assumed:** CronJob `substrate-restore-test` (Sunday 04:00) restores
into that scratch cluster and runs [`scripts/restore-assert.sh`](../../scripts/restore-assert.sh)
on `memory`, `instruction`, and `audit`. A restore that produces **zero rows fails the job** —
that is not a silent success. A backup that has never been restored is not a backup.

**The CronJob ships `suspend: true` and that is committed state.** `kubectl apply -k deploy/`
(step 8) must not arm an unattended weekly delete-and-restore before a human has ever run one.
Step 11 unsuspends it, and only after step 10 recorded non-zero counts. If you ever re-apply
`deploy/`, kustomize sets `suspend` back to `true` — re-run the step 11 patch.

**A failed weekly run leaves the scratch cluster behind on purpose.** `restore-test.sh` cleans
up on success only; the week the job actually matters is the week it fails, and deleting the
scratch cluster in an `EXIT` trap would destroy the only evidence. The Job log names the
cluster and the delete command. The next run deletes it before restoring, so the cost of
forgetting is one lingering 20Gi Longhorn volume, not a broken schedule.

The restore container needs kubectl and `/bin/sh` only: both scripts are POSIX sh, so the
busybox-based kubectl images (`rancher/kubectl`, `alpine/k8s`) work. Pin it by digest.

## TLS for the tailnet host

**cert-manager cannot issue a certificate for `substrate.<tailnet>`.** A `*.ts.net` name is not
publicly reachable, so the HTTP-01 challenge can never be answered, and Tailscale DNS is not
one of cert-manager's DNS-01 providers. Point
[`deploy/ingress/certificate.yaml`](../../deploy/ingress/certificate.yaml) at an ordinary
Let's Encrypt `ClusterIssuer` and the `Certificate` stays `Ready=False` forever, the
`substrate-tls` Secret is never created, and **Traefik silently serves its self-signed default**
— which looks like a browser warning, not an outage, and nothing alerts on it.

`certificate.yaml` is therefore **not** in `deploy/kustomization.yaml` resources. Pick one:

1. **`tailscale cert`** (the default; this is what a tailnet host is for). Tailscale obtains a
   real Let's Encrypt certificate for the MagicDNS name over its own challenge path. Run on a
   node in the tailnet, then load it into the Secret the IngressRoute already references:

   ```sh
   tailscale cert --cert-file tls.crt --key-file tls.key substrate.<tailnet>
   kubectl create secret tls substrate-tls -n substrate \
     --cert=tls.crt --key=tls.key --dry-run=client -o yaml | kubectl apply -f -
   ```

   Certificates are 90-day and this renewal is **manual** unless the operator scripts it.
   Alert on days-to-expiry (< 14 days) or it becomes a 00:00 UTC outage.

2. **An internal CA.** Create a cert-manager `Issuer`/`ClusterIssuer` of kind `CA` (or Vault /
   step-ca) whose root is distributed to every client that calls `/mcp` and `/v1`, substitute
   `<internal-ca-cluster-issuer>`, and add `ingress/certificate.yaml` back to
   `kustomization.yaml` resources.

Verify with `openssl s_client -connect substrate.<tailnet>:443 -showcerts` and confirm the
chain is the one you intended — not Traefik's default. Checking only the leaf hides a broken
chain.

## Which role goes in `SUBSTRATE_DSN`

**Use the CNPG-generated `substrate-pg-app` secret. Never `postgres`.**

`bootstrap.initdb.owner: app` makes CNPG create the role `app`, generate its password, and
store it in a secret named `substrate-pg-app`. Read it; do not invent one:

```sh
kubectl get secret substrate-pg-app -n substrate -o jsonpath='{.data.uri}' | base64 -d
```

The two `managed.roles` (`substrate_migrate`, `substrate_app`) are `login: false` and have no
`passwordSecret`. They are `SET ROLE` / `SET SESSION AUTHORIZATION` targets, not connection
identities, and **cannot** appear in a DSN. The server connects as `app`, migrates, then
switches the request pool to `substrate_app` so the RLS policies and the `audit` REVOKEs bind.

`enableSuperuserAccess` is **off** (CNPG's default, and `cluster.yaml` no longer overrides it):
nothing at runtime connects as `postgres`, and a superuser password that exists is a superuser
password that eventually ends up in a DSN. Local `psql -U postgres` through `kubectl exec` still
works — that is peer auth on the unix socket, which is all the restore job needs.

## Operator checklist

**Requires the operator.** Nothing in this list is performed by an automated worker against
the homelab. Substitute every `<placeholder>` on the operator machine; never commit filled
values. `kubectl kustomize` is offline. `kubectl apply --dry-run=client` still performs
API discovery and must not use the homelab kubeconfig. `--dry-run=server` talks to
the API and is a human step.

| # | Step | Blast radius |
|---|---|---|
| 1 | Fill [`deploy/server/secret.yaml`](../../deploy/server/secret.yaml) (`SUBSTRATE_DSN` only) and [`deploy/cnpg/backups-secret.yaml`](../../deploy/cnpg/backups-secret.yaml) (MinIO keys only). The DSN uses the CNPG-generated `substrate-pg-app` role — see [Which role goes in `SUBSTRATE_DSN`](#which-role-goes-in-substrate_dsn). **There is no superuser secret to fill and no postgres password to hold.** Keep the filled copies off git. | None until apply. A filled file committed to the repo is a credential leak. The two secrets are separate so an RCE in the server cannot read the backup bucket keys. |
| 2 | Substitute every placeholder, then **verify**: `scripts/check-deploy-placeholders.sh --substituted deploy/`. The full list is `<minio-service>`, `<minio-namespace>`, `<ollama-namespace>`, `<tailnet>`, `<tailscale-entrypoint>`, `<traefik-namespace>`, `<cnpg-namespace>`, `<prometheus-namespace>`, `<bucket-owner>`, `<minio-endpoint>`, and four images: `<substrate-pg-image>`, `<substrate-server-image>`, `<kubectl-image-digest>`, `<otel-collector-image-digest>`. `<internal-ca-cluster-issuer>` only if you chose the internal-CA TLS path. **All four images must be pinned by digest** (`repo/name@sha256:…`); the checker rejects a tag. | None until apply. A public Traefik entrypoint here would expose `/mcp` and `/v1` off-tailnet. A missed placeholder applies as a literal — `kubectl` accepts `<tailnet>` as a hostname. |
| 3 | Create MinIO buckets using [`deploy/minio/buckets.sh`](../../deploy/minio/buckets.sh) (printed `mc` commands, not a script to pipe blindly). Confirm `mc ls` shows no collision first. The `mc alias` line takes `<access-key>`, `<secret-key>`, `<minio-endpoint>`, and `<bucket-owner>` — these are MinIO-side, not Kubernetes placeholders, and never go into a manifest. | New buckets `substrate-backups` and `substrate-sessions` only. A colliding name can hide or encrypt someone else's prefix. |
| 4 | Build the CNPG image: `docker build -t substrate-pg:16-pgvector deploy/cnpg/` and load it where the cluster can pull. | Image store only. |
| 5 | `make ko-build` (`ko build --local ./cmd/substrate-server`). Do not push unless the operator's registry is the intended destination. Tag/load so the Deployment image matches. | Image store only. `ko` without `--local` would push. |
| 6 | Offline check: `kubectl kustomize deploy/`, `scripts/check-deploy-secrets.sh`, and `scripts/check-deploy-placeholders.sh --substituted deploy/`. `kubectl apply --dry-run=client` still performs API discovery; do not point it at the homelab kubeconfig. | None. |
| 7 | `kubectl apply --dry-run=server -k deploy/` against the real API. | None (no objects persist) but it **does** contact the cluster. |
| 8 | `kubectl apply -k deploy/` — Namespace, NetworkPolicies, `substrate-pg` (20Gi Longhorn), server, PDBs, OTel collector, IngressRoute, CronJob RBAC. The CronJob lands **suspended**; that is intended. **Do not** apply `scratch-cluster.yaml` at this step, and `certificate.yaml` is not applied at all (see [TLS for the tailnet host](#tls-for-the-tailnet-host)). | One 20Gi Longhorn volume. A default-sized PVC or a colliding hostname can take a neighbouring workload down. Traefik route is inert unless the entrypoint is already public (see step 2). The default-deny NetworkPolicy takes effect immediately — if the CNI does not enforce NetworkPolicy this is a no-op, and if it does, a wrong `<traefik-namespace>` blackholes all ingress. |
| 9 | Wait for `substrate-pg` Ready. Confirm extensions `vector`, `pg_trgm`, `ltree` and roles `substrate_migrate` / `substrate_app` with `rolbypassrls = false` on `substrate_app`. Let the server migrate on boot. | Process of record: first goose Up against this database. There is no `goose down` after this (see Migrations). |
| 10 | **First restore, by hand, before the CronJob is trusted.** Apply [`deploy/cnpg/scratch-cluster.yaml`](../../deploy/cnpg/scratch-cluster.yaml), wait Ready, exec `SELECT count(*) FROM memory`, `instruction`, `audit`, run `scripts/restore-assert.sh` with those three numbers, record the counts, then `kubectl delete cluster substrate-pg-scratch`. Zero rows is a failed restore, not an all-clear. | A second 20Gi Longhorn volume until deleted. Deletes only the scratch Cluster, never `substrate-pg`. |
| 11 | **Arm the CronJob**, only after step 10 recorded non-zero counts: `kubectl patch cronjob substrate-restore-test -n substrate -p '{"spec":{"suspend":false}}'`. Confirm with `kubectl get cronjob substrate-restore-test -n substrate -o jsonpath='{.spec.suspend}'` → `false`. Sunday 04:00 then repeats the scratch restore and fails the Job on zero rows. | First point at which an unattended job deletes and recreates a Cluster once a week. A failed run **leaves** the scratch cluster for inspection — check Longhorn for the extra 20Gi and delete it after investigating. Re-applying `deploy/` re-suspends the job; re-run this patch. |

`kubectl apply --dry-run=server`, executing the restore on the scratch cluster, and anything
that needs the real tailnet name or credentials are **Requires the operator**. They are not
done in CI and they are not performed against the live cluster except by the operator.

## Alerts (Phase 1 minimum)

Rules live in [`deploy/alerts.yaml`](../../deploy/alerts.yaml), loaded by the homelab
Prometheus. The `substrate_*` series they alert on come from the in-namespace OTel collector
([`deploy/otel/collector.yaml`](../../deploy/otel/collector.yaml)): `substrate-server` exports
OTLP/HTTP to it on 4318, and it re-exposes the metrics for scrape on 8889. Without that
collector these rules reference metrics nothing exports — they evaluate to no data and stay
green forever, which is the failure mode of an alert that has never fired. Metric names are `substrate_*`.
`substrate_outbox_depth` is reported **by the adapter**, per `machine` label, including zero.
A machine that stopped reporting drops the series; do not default missing to 0.

The collector endpoint is `-otlp` / `SUBSTRATE_OTLP_ENDPOINT`, set in
[`deploy/server/configmap.yaml`](../../deploy/server/configmap.yaml) to
`http://substrate-otel-collector.substrate.svc.cluster.local:4318`. Empty disables export
(supported, not degraded) — but then the alerts above go quiet, not green-because-healthy. A collector outage must not fail a request; export is best-effort and a failure
is logged once. On SIGTERM, `Shutdown` waits up to 5s for the collector; a hanging collector
delays process exit by that much and the timeout is not returned as a process-exit error.

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

## Import cutover and rollback

Cutover replaces a machine's harness files with rendered ones and installs the
adapter unit. The files it displaces are the only copy of that machine's
accumulated configuration. There is no backup beyond the `*.pre-substrate`
renames this command makes. If a `*.pre-substrate` path already exists, stop
and resolve it by hand — overwriting it destroys an earlier cutover's state.

Always dry-run first. `-root` is repeatable and has no `$HOME` default; passing
the real home is a deliberate operator choice. When `-root` is omitted, cutover
defaults to `/work` so CONT-4 checkouts are displaced to `*.pre-substrate`
instead of being left for the adapter to treat as drift and overwrite. Passing
`-root "$HOME"` does **not** add `/work`; include both if harness files live
under home and checkouts live under the mount root.

```sh
# Preview. Writes nothing. The printed list must match what --restore would invert.
./bin/substrate import cutover -root "$HOME" -root /work \
  -server "$SUBSTRATE_URL" -token "$TOKEN" -machine wsl

# Commit. systemd --user on Linux/WSL, launchd on macOS. Unit pins -roots /work.
./bin/substrate import cutover -root "$HOME" -root /work \
  -server "$SUBSTRATE_URL" -token "$TOKEN" -machine wsl -commit
```

Nothing in this flow deletes. Live files and the `.memorix` store are renamed
to `*.pre-substrate`, then rendered files are written. The adapter unit's
`-home` is the first `-root`; `-roots` is the set this cutover displaced, so
the daemon only ever renders where a `*.pre-substrate` backup exists. With
`-root` omitted that set is `/work`, keeping Phase 3 cwd slugs matching
later (CONT-4). If you cut over `$HOME` only, the unit scans `$HOME` only —
checkouts under `/work` stay unmanaged rather than being overwritten
unrecoverably. Cutover walks exactly the `-root` list it was given
(or `/work` when none was given). Restore is the inverse of that list and
does not discover extra trees.

**Rollback** — run this before anything else if cutover went wrong. Server rows
are additive and can stay.

```sh
./bin/substrate adapter uninstall -restore -root "$HOME" -root /work
./bin/substrate adapter uninstall -restore -root "$HOME" -root /work -commit
```

`-restore` puts planned `*.pre-substrate` paths back over the live path (verified
by hash in tests), removes generated files only when they still match the
written `DriftHash`, and uninstalls the unit. If a live file was edited after
cutover, restore refuses and prints `DISCARD live edits`; re-run with `-force`
to overwrite those edits. After a successful restore there must be no
`*.pre-substrate` leftovers. If restore reports a file/directory mismatch, do
not delete: rename the unexpected side out of the way and re-run.

```sh
./bin/substrate adapter uninstall -restore -root "$HOME" -root /work -force
./bin/substrate adapter uninstall -restore -root "$HOME" -root /work -force -commit
```
