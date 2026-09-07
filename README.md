<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/hero-dark.svg">
    <img src="docs/assets/hero-light.svg" alt="Substrate — disposable harnesses on top, one durable control plane beneath, every machine in sync" width="820">
  </picture>
</p>

<h1 align="center">Substrate</h1>

<p align="center"><strong>A self-hosted control plane that makes AI coding agents disposable by making their context durable.</strong></p>

<p align="center">
  One source of truth for instructions, memory, and skills —<br>
  served to Claude Code, Codex, Cursor, or any headless worker, on any machine.
</p>

<p align="center">
  <a href="ROADMAP.md">Roadmap</a> ·
  <a href="docs/product/problem.md">Why</a> ·
  <a href="docs/design/edd.md">Design</a> ·
  <a href="CONTRIBUTING.md">Contribute</a>
</p>

<p align="center">
  <img alt="Status: pre-alpha" src="https://img.shields.io/badge/status-pre--alpha-orange">
  <img alt="Go 1.26.6+" src="https://img.shields.io/badge/go-1.26.6%2B-00ADD8?logo=go&logoColor=white">
  <img alt="License: Apache-2.0" src="https://img.shields.io/badge/license-Apache--2.0-blue">
  <img alt="MCP" src="https://img.shields.io/badge/protocol-MCP-black">
</p>

> Status: **pre-alpha.** Phase 1 (authority + sync) is under construction. Nothing here is
> stable yet; [ROADMAP.md](ROADMAP.md) says what "done" means at each horizon.

---

Every coding harness treats the **agent as durable and its knowledge as disposable**. Substrate is built on the opposite premise: the agent is the throwaway part. What it learned, what it was told, and what it's allowed to do should outlive it — and follow you to the next harness, the next laptop, the next teammate.

## Why

You already pay this tax. It just doesn't show up on one line item.

| What you do today | What it costs |
|---|---|
| Copy `CLAUDE.md` / `AGENTS.md` / `.cursor/rules` between machines | Two machines disagree. Neither is right. |
| Re-explain the project every session | Minutes, per session, per harness |
| Let each harness keep its own memory | Knowledge stranded on whichever box learned it |
| Finish the job where you started it | Can't hand a long run to the GPU box |
| Trust what the agent "remembers" | Confident action on facts the code no longer honors |

The cost recurs, and it multiplies: **harnesses × machines × people**.

Substrate collapses it to one function —

```
(global → org → team → project → repo → branch → task → session) + user overlay
                     → effective context
```

— compiled once, served everywhere, with what agents learn flowing *back* under provenance and trust. Full framing: [`docs/product/problem.md`](docs/product/problem.md).

## What makes it different

Shared agent memory is a crowded space, and three of the four layers here are borrowed deliberately — Agent Skills for the skill format, board-as-control-plane for orchestration, transcript-slug resumption for migration. What no surveyed project offers is this combination on **one store**:

- **A scope chain.** `global → org → team → project → repo → branch → task → session`, with `user` as an orthogonal overlay. Every table uses it. So does the compiler.
- **Deterministic instructions.** Rules are a first-class table, never scored, never budget-trimmed. *A rule that is sometimes present is worse than no rule.*
- **Provenance and trust on memory.** Every fact carries who wrote it, how it was verified, and its status: `unverified → probable → confirmed`. A low-trust agent can never quietly demote a human-confirmed fact.
- **Budgeted compilation, explainable by design.** The pack is compiled under a token budget with instructions exempt; `context.explain` — why an item is in the pack, and why another is not — is a Next-horizon item.
- **Local files as caches.** `CLAUDE.md` is a *rendering* of the store, emitted byte-deterministically. The adapter that turns a hand-edit into a reviewable diff rather than a silent merge is Phase 1 work in progress.

Reasoning, and what we're deliberately *not* building: [`docs/product/positioning.md`](docs/product/positioning.md), [`docs/product/non-goals.md`](docs/product/non-goals.md).

## Works with

| Harness | Substrate renders | Captures via | Continuity |
|---|---|---|---|
| Claude Code | `CLAUDE.md` (`@AGENTS.md` + hooks block) | `SessionStart` / `PostToolUse` hooks *(Phase 2)* | checkpoint *(Phase 2)* |
| Codex | `AGENTS.md`, `~/.codex/AGENTS.md` | hooks *(Phase 2)* | checkpoint *(Phase 2)* |
| Cursor | `~/.cursor/rules/substrate.mdc` | read-only | checkpoint *(Phase 2)* |
| Headless workers | context pack over MCP (`context.get`) | `memory.*` MCP tools | lease + checkpoint *(Phase 6)* |

Rendering and skill linking ship today; hooks and continuity are designed and phased, not shipped. Skills use the [Agent Skills](https://agentskills.io) `SKILL.md` format — Git holds skill content, Substrate holds skill state (`skill_version.git_sha` is the only link). The adapter materializes the active approved `git_sha` into `~/.agents/skills/<name>` and refreshes harness symlinks; a skill whose scope or visibility does not match this machine is omitted from `GET /v1/skills/manifest` and is not linked. `active_version_id` can only point at an approved version (Postgres trigger, R6).

## Quickstart

Requires Go 1.26.6+ (the floor in `go.mod`; earlier 1.26 patches carry stdlib CVEs that `govulncheck` fails CI on).

```sh
git clone https://github.com/agentic-substrate/substrate.git && cd substrate

make build     # binaries into ./bin
make smoke     # boots the real server against empty state and hits it
make check     # everything CI runs: fmt, vet, lint, race tests, govulncheck
```

Run the server and mint a token:

```sh
./bin/substrate-server -addr :8080 -dsn 'postgres://…'
curl localhost:8080/healthz
curl localhost:8080/readyz

./bin/substrate token mint --for agent --parent <user-uuid> --machine wsl \
    --scopes memory:write --dsn "$SUBSTRATE_DSN"

./bin/substrate import scan -root /abs/machine-root -hostname wsl \
    -out /abs/path/inventory.json [-memorix-json /abs/path/memorix.json] \
    [-exclude 'some/fixtures/**']
./bin/substrate import plan -out /abs/path/plan.json /abs/path/inventory.json
./bin/substrate import apply -machine mac -trusted mac \
    -server "$SUBSTRATE_URL" -token "$TOKEN" \
    -scope 'global:/org:acme/team:core/project:plotlens' \
    /abs/path/plan.json
./bin/substrate import apply -machine mac -trusted mac -commit \
    -server "$SUBSTRATE_URL" -token "$TOKEN" \
    -scope 'global:/org:acme/team:core/project:plotlens' \
    /abs/path/plan.json
```

Point any MCP client at `POST /mcp` with that bearer token. Phase 1 exposes `context.get`, `memory.write`, `memory.search`, and `memory.supersede`. Agents always write `unverified`; supersede never deletes.

<details>
<summary><strong>Operational detail</strong> — health, embeddings, the <code>/v1</code> surface, configuration</summary>

`-dsn` (or `SUBSTRATE_DSN`) is the Postgres connection string. The server listens first. If a
DSN is set, it applies goose migrations under an advisory lock and retries in the background
rather than exiting. `/healthz` is **200** whenever the process is alive. `/readyz` returns
**200** only after migrations have applied and the pool can ping Postgres; otherwise **503**.
Without a DSN, `/readyz` is 503 `store not configured`. `/readyz` does not check Git or the
skills repo (EDD R27).

`-ollama` (or `SUBSTRATE_OLLAMA_URL`) points at Ollama for embeddings — `nomic-embed-text`, 768
dimensions, called directly from Go with no Python in the request path (EDD R21). Leave it empty
and retrieval is keyword-only, which is a supported configuration rather than a degraded one:
`memory.search` must never fail because embeddings are unavailable (MEM-5). When it is set, a
write whose embed call fails still succeeds with `embedding NULL` and is picked up by the
backfill pass. Semantic similarity is capped below the score of an exact identifier hit, so a
vector neighbour can never outrank a typed filename or symbol (MEM-3).

`POST /mcp` is the streamable-HTTP MCP endpoint (EDD §4.1). Bearer auth is required;
unauthenticated requests are rejected before the MCP handler runs. Rate limiting is Traefik's
job, not the process. `context.get` compiles instructions, preferences, mandatory items,
keyword-retrieved memories, and a skill index under the documented conservative token estimator;
instructions, preferences, and mandatory items are never trimmed.

`/v1` is the adapter and CLI REST surface (EDD §4.2), behind the same bearer auth as `/mcp` and
not exposed to harnesses. It serves `GET /v1/render`, `GET /v1/skills/manifest`,
`POST /v1/memory/batch`, `GET /v1/memory/cache`, review create/list/decide, `POST /v1/import`,
`GET /v1/events`
(SSE wake-ups when `substrate_audit` fires; the payload carries no audit metadata), and
`GET /v1/health/git`. `/v1/memory/batch` is the idempotency boundary: `ingest_receipt` and the
memory row are created in one transaction, and a replay with a known `client_id` returns the
original id with `duplicate: true`. Render `sha256` values are `render.DriftHash` of the content
(footer excluded). `POST /v1/import` consumes a `plan.json` (SYNC-5, EDD §9). The most-trusted
machine is applied first; only its non-conflict blocks that are not already byte-identical to
an active row become `active`. The first successful trusted commit stores a per-scope
marker; a later `-trusted` that names a different host is rejected, including when the
later host uses a different token. Every later machine can only add `proposed` rows and
`import_conflict` review items — it cannot promote anything to `active` or modify a row the
trusted machine established. Each conflict payload carries the applying `hostname` and the
pair sides' hostnames. Imported memory is always `episodic`/`unverified` even when the source
claimed `confirmed` (Gotcha 4). A replay of the same machine+plan is idempotent via
`ingest_receipt` keyed on `(machine, scope, plan-hash)`; an optional `client_id` is an
alias of that key, never a second write. `dry_run` is the default: the server classifies and returns the planned set
grouped by hostname without opening a write transaction. Writes require `"commit": true`.

Tokens are printed once and stored only as a SHA-256 hash; agent tokens expire in 24 hours
(EDD R3). `--for agent` is the only accepted kind today. Revoke with
`./bin/substrate token revoke <token> --dsn "$SUBSTRATE_DSN"`. `./bin/substrate` with no
arguments reports the version.

`substrate import scan` and `substrate import plan` are read-only (SYNC-5, EDD §9). Scan
requires `-root` (repeatable), `-hostname`, and `-out`; plan requires `-out` and one or
more inventory files. `-root` and `-out` must be absolute paths — there is no `$HOME`
default, and the command will not guess one. `-out` is rejected if it falls inside any
`-root` (scan) or equals any inventoried `Path` (plan), and the written file is always
mode `0600`, even when it already existed. Scan never writes, moves, or modifies
anything under the scanned roots; it only writes the file named by `-out`. It inventories
`.claude/CLAUDE.md`, `.codex/AGENTS.md`, `.cursor/rules/`, and any `AGENTS.md` /
`CLAUDE.md` found under those roots. Symlinks are not followed. Memorix is read only from
an operator-exported JSON file passed as `-memorix-json`; scan never execs `memorix`.
If that flag is omitted the source is skipped with a logged reason and the rest of the
scan still succeeds. An unreadable directory under a root is recorded in `skipped` and the
walk continues, so one permission error cannot abort a scan of a real home. Dependency and
plugin caches are never inventoried — a `CLAUDE.md` in the Go module cache or an `AGENTS.md`
in a vendored crate belongs to its upstream author, not to this machine. `-exclude <glob>`
(repeatable) drops anything matching a glob against the root-relative path, where `**` spans
separators; a pattern that is not a valid glob is an error rather than a filter that
silently matches nothing. Plan splits markdown into blocks, **dedupes by content hash**, then
classifies each unique block as `instruction` or `preference`. Classification is heuristic
and will misfile (R15); confidence is never certainty. Identical blocks from two machines
appear once in `plan.json`; differing blocks at the same heading from distinct hostnames
or paths are a conflict pair tagged with hostname. Extra hashes from a single file stay
in `blocks`. `substrate import apply` POSTs that plan to `/v1/import`. It
requires `-machine`, `-trusted` (the most-trusted hostname; apply it first),
`-server` (or `SUBSTRATE_URL`), `-token` (or `SUBSTRATE_TOKEN`), `-scope`, and
one `plan.json`. Without `-commit` the command is a dry-run: it performs every
read and decision, prints counts and identities of rows that would become
active, proposed, conflict, and memory grouped by hostname, and writes nothing.
`-dry-run` is the same path. `-commit` performs the writes. The dry-run and the
real run share one code path. Cutover is a separate command and is not served
here.

`substrate-adapter` is the per-machine daemon (EDD §5). With no `-server` it reports its
version. With `-server` it pulls `GET /v1/render` on start, every 5 minutes, and on a
`GET /v1/events` SSE wake-up; writes managed files atomically; and posts a `drift_proposal`
(unified diff) *before* restoring a hand-edited file. Comparisons use `render.DriftHash`
(footer excluded). Repo discovery scans `-roots` two levels deep (`<root>/*/*`); unknown
remotes are logged, never auto-created as scopes, and their checkouts are left untouched.
Render targets are confined to the paths `render.Specs()` declares, under `-home` or a
discovered checkout, with symlinks resolved. `-home` is required with `-server` so the binary
cannot guess `$HOME` and overwrite pre-cutover harness files. State lives in
`<home>/.substrate/adapter.sqlite` (override with `-state`). The outbox drains
hook-captured observations to `POST /v1/memory/batch` in batches of at most 50, with a
UUIDv7 `client_id` assigned at enqueue and never regenerated on retry; a replayed id is
success (`duplicate: true`). The offline cache is an FTS5 delta of confirmed/probable
memory for remotes present on the machine, pulled from `GET /v1/memory/cache`. Hook-path
calls use a 1.5s server timeout and fall back to the outbox (writes) or the cache (reads)
so they return in under 2s. The skills loop runs on the same tick as render: `GET
/v1/skills/manifest`, `git fetch` of `-skills-repo` into `<home>/.substrate/skills.git`,
export of each approved `git_sha` into `<home>/.agents/skills/<name>`, and symlinks into
`.claude/skills`, `.codex/skills`, and `.cursor/skills`. A skills-repo outage keeps the last
linked versions and does not affect `/readyz`.

```sh
./bin/substrate-adapter -server https://cp.example -token "$TOKEN" \
  -home "$HOME" -roots /work -machine wsl -skills-repo git@github.com:org/skills.git
```

**Configuration:** `-addr` (listen address), `-dsn` / `SUBSTRATE_DSN`, `-ollama` /
`SUBSTRATE_OLLAMA_URL` (empty means keyword-only), `-skills-repo` / `SUBSTRATE_SKILLS_REPO`
(skills git remote; probed by `GET /v1/health/git`, never by `/readyz`). Adapter flags:
`-server`, `-token` / `SUBSTRATE_TOKEN`, `-machine`, `-home`, `-state`, `-roots`, `-interval`
(default 5m), `-scope` (review items), `-skills-repo` / `SUBSTRATE_SKILLS_REPO` (bare mirror
under `<home>/.substrate/skills.git`; empty skips linking). The rest of the
intended surface is specified in [`docs/design/edd.md`](docs/design/edd.md) §7 and §12 and gets
documented here as it lands.

</details>

## Where to run it

| | Try it | Homelab | Team |
|---|---|---|---|
| **Setup** | `make build`, local Postgres | k8s: CloudNativePG on Longhorn, Traefik v3 | Later horizon |
| **Store** | one Postgres | CloudNativePG + MinIO backups | same, RLS-isolated per team |
| **Embeddings** | keyword-only (fine) | Ollama on your GPU | " |
| **Network** | localhost | Tailscale, no public exposure | OIDC + Tailscale |

Keyword-only retrieval is a supported mode, not a degraded one. `memory.search` never fails
because embeddings are unavailable. Topology, backup/restore, and failure modes:
[`docs/ops/runbook.md`](docs/ops/runbook.md).

## How it fits together

Harnesses are clients. Substrate is the plane underneath. Writes hit the store; the compiler reads the store and emits a **context pack** (`context.get`, shipped); the per-machine adapter renders that pack into the files your harness already reads, drains the hook outbox, and serves an offline FTS cache when the server is unreachable. The full engineering diagram is in [`docs/design/edd.md`](docs/design/edd.md).

## What's here, what isn't

**Now — building.** Postgres store · scope chain · deterministic instruction resolution · `context.get` + `memory.*` MCP tools · per-machine adapter (render, link skills, outbox, drift review) · import reconcile of existing rule files.

**Designed, not shipped.** Ranked and explainable context packs (`context.explain`) · trust-weighted contradiction handling · symbol-level staleness · transcript offload between machines · task leases · multi-team RLS · review UI.

**Not building.** A new IDE or harness. A hosted multi-tenant SaaS. "Prompt injection, solved." Details in [`docs/product/non-goals.md`](docs/product/non-goals.md).

Confidence horizons, not dates: [ROADMAP.md](ROADMAP.md).

## Documentation

| | |
|---|---|
| [AGENTS.md](AGENTS.md) | The contract every agent working in this repo reads first |
| [docs/product/](docs/product/) | Problem, positioning, non-goals |
| [docs/design/prd.md](docs/design/prd.md) · [edd.md](docs/design/edd.md) | Requirements (with the `MEM-7` / `CTX-2` IDs used everywhere) and the signed-off engineering design |
| [docs/ops/runbook.md](docs/ops/runbook.md) | Deploy, migrate, back up, restore, alert |
| [CONTRIBUTING.md](CONTRIBUTING.md) | How to get a change merged |
| [SECURITY.md](SECURITY.md) | Reporting, and what is deliberately not a vulnerability |

## Licence

[Apache-2.0](LICENSE). Permissive on purpose: the adapter contract, the MCP tool surface, and the rendered-file targets are all interfaces a third harness might implement. Nobody writes an adapter against a contract they can't read.
