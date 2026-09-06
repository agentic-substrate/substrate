# Substrate

**A self-hosted control plane that makes AI coding agents disposable by making their context
durable** — one source of truth for instructions, memory, and skills, served to any harness on
any machine.

> Status: **pre-alpha.** Phase 1 (authority + sync) is under construction. Nothing here is
> stable yet; see [ROADMAP.md](ROADMAP.md) for what "done" means at each horizon.

## The problem

Every AI coding harness — Claude Code, Codex, Cursor, any headless worker — keeps its own
memory, instruction files, and session state on whichever machine it happened to run on. The
agent is treated as durable and its knowledge as disposable, when it should be the reverse.

Switching harnesses, switching machines, or bringing a teammate onto a project means
re-explaining context, re-discovering past failures, and hand-syncing instruction files that
have already drifted. The cost recurs every session and grows with agents × machines × people.

Substrate inverts it. It is one function —

```
(principal + team + project + repo + branch + task + current code) → effective context
```

— served to any harness on any machine, with what the harness learns flowing back under
provenance and trust. Full framing: [`docs/product/problem.md`](docs/product/problem.md).

## What makes it different

Shared agent memory is a crowded space, and three of the four layers here are borrowed
deliberately (Agent Skills for the skill format, board-as-control-plane for orchestration,
transcript-slug resumption for migration). The part being built is the combination no surveyed
project offers on one store:

- A normalized **scope chain** — `global → org → team → project → repo → branch → task → session`
  — with `user` as an orthogonal overlay, used identically by every table and by the compiler.
- **Deterministic instruction resolution.** Instructions are a first-class table, never scored,
  never trimmed by budget pressure. A rule that is sometimes present is worse than no rule.
- **Provenance and verification state** on every memory, with trust-weighted conflict handling:
  a low-trust agent observation can never quietly demote a human-confirmed fact.
- **Explainable, budgeted context compilation** — ask why an item is in the pack, and why
  another is not.
- **Adapters that treat local files as caches.** A hand-edited `CLAUDE.md` is drift: it becomes
  a reviewable diff, not a silent merge.

Reasoning and what we're *not* building: [`docs/product/positioning.md`](docs/product/positioning.md),
[`docs/product/non-goals.md`](docs/product/non-goals.md).

## Quickstart (developers)

Requires Go 1.26.6+ (the floor in `go.mod`; earlier 1.26 patches carry stdlib CVEs that `govulncheck` fails CI on).

```sh
git clone https://github.com/agentic-substrate/substrate.git
cd substrate

make build     # static binaries into ./bin
make smoke     # boots the real server against empty state and hits it
make check     # everything CI runs: fmt, vet, lint, race tests, govulncheck
```

Run the server directly:

```sh
./bin/substrate-server -addr :8080 -dsn 'postgres://…'
curl localhost:8080/healthz
curl localhost:8080/readyz
```

`-dsn` (or `SUBSTRATE_DSN`) is the Postgres connection string. The server listens first.
If a DSN is set, it applies goose migrations under an advisory lock and retries in the
background rather than exiting. `/healthz` is **200** whenever the process is alive.
`/readyz` returns **200** only after migrations have applied and the pool can ping Postgres;
otherwise **503**. Without a DSN, `/readyz` is 503 `store not configured`. `/readyz` does
not check Git or the skills repo (EDD R27).

`-ollama` (or `SUBSTRATE_OLLAMA_URL`) points at Ollama for embeddings — `nomic-embed-text`,
768 dimensions, called directly from Go with no Python in the request path (EDD R21). Leave it
empty and retrieval is keyword-only, which is a supported configuration rather than a degraded
one: `memory.search` must never fail because embeddings are unavailable (MEM-5). When it is set,
a write whose embed call fails still succeeds with `embedding NULL` and is picked up by the
backfill pass. Semantic similarity is capped below the score of an exact identifier hit, so a
vector neighbour can never outrank a typed filename or symbol (MEM-3).

`POST /mcp` is the streamable-HTTP MCP endpoint (EDD §4.1). Bearer auth is required;
unauthenticated requests are rejected before the MCP handler runs. Rate limiting is
Traefik's job, not the process. Domain packages register tools on the server
`internal/mcpx` provides. Phase 1 exposes `context.get`, `memory.write`, `memory.search`,
and `memory.supersede`. `context.get` compiles instructions, preferences, mandatory
items, keyword-retrieved memories, and a skill index under the documented conservative
token estimator; instructions, preferences, and mandatory items are never trimmed.
Agents always write `unverified`; supersede never deletes.

`/v1` is the adapter and CLI REST surface (EDD §4.2), behind the same bearer
auth as `/mcp` and not exposed to harnesses. It serves `GET /v1/render`,
`GET /v1/skills/manifest`, `POST /v1/memory/batch`, `GET /v1/memory/cache`,
review create/list/decide, `GET /v1/events` (SSE of `substrate_audit`
notifications), and `GET /v1/health/git`. `/v1/memory/batch` is the
idempotency boundary: `ingest_receipt` and the memory row are created in one
transaction, and a replay with a known `client_id` returns the original id
with `duplicate: true`. Render `sha256` values are `render.DriftHash` of the
content (footer excluded). `/readyz` stays Postgres-only; skills-repo
reachability is `/v1/health/git` plus the `substrate_git_health` expvar
(EDD R27). `POST /v1/import` is not served here.

The operator CLI mints and revokes bearer tokens. The token is printed once
and stored only as a SHA-256 hash; agent tokens expire in 24 hours (EDD R3).

```sh
./bin/substrate token mint --for agent --parent <user-uuid> --machine wsl --scopes memory:write --dsn "$SUBSTRATE_DSN"
./bin/substrate token revoke <token> --dsn "$SUBSTRATE_DSN"
```

`./bin/substrate` with no arguments still reports the version. The adapter
binary is the per-machine daemon (render, skills, outbox, cache loops).

## Configuration

`-addr` (listen address), `-dsn` / `SUBSTRATE_DSN` (Postgres), `-ollama` /
`SUBSTRATE_OLLAMA_URL` (embeddings; empty means keyword-only), and
`-skills-repo` / `SUBSTRATE_SKILLS_REPO` (skills git remote; probed by
`GET /v1/health/git`, never by `/readyz`). The rest of the intended
surface — repo roots, the OTel endpoint — is specified in
[`docs/design/edd.md`](docs/design/edd.md) §7 and §12 and will be documented here as it lands.

## Deployment

Single node, homelab, Tailscale-only, no public exposure: CloudNativePG on Longhorn, Traefik v3
with cert-manager, MinIO for transcripts and backups, Ollama on a T4 for embeddings. Topology,
backup/restore, and failure modes: [`docs/ops/runbook.md`](docs/ops/runbook.md).

## Documentation

| Doc | What it is |
|---|---|
| [AGENTS.md](AGENTS.md) | The contract every AI agent working in this repo reads first |
| [ROADMAP.md](ROADMAP.md) | Now / Next / Later as confidence horizons |
| [docs/product/](docs/product/) | Problem statement, non-goals, positioning |
| [docs/design/prd.md](docs/design/prd.md) | Requirements, with the IDs (`MEM-7`, `CTX-2`) used everywhere |
| [docs/design/edd.md](docs/design/edd.md) | Engineering design, reviewed twice and signed off |
| [docs/ops/runbook.md](docs/ops/runbook.md) | Deploy, migrate, back up, restore, alert |
| [CONTRIBUTING.md](CONTRIBUTING.md) | How to get a change merged |
| [SECURITY.md](SECURITY.md) | Reporting, and what is deliberately not a vulnerability |

## Licence

[Apache-2.0](LICENSE). Permissive on purpose: the adapter contract, the MCP tool surface, and
the rendered-file targets are all interfaces a third harness might implement, and nobody writes
an adapter against a contract they cannot read.
