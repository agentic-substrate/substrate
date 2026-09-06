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
./bin/substrate-server -addr :8080
curl localhost:8080/healthz
```

`/readyz` returns **503 `store not configured`** today, which is correct — the Postgres store
lands with Phase 1's schema work.

The other two binaries report their version and little else so far:

```sh
./bin/substrate-adapter    # per-machine daemon: render, skills, outbox, cache loops
./bin/substrate            # operator CLI: import, review, token, offload, doctor
```

## Configuration

Nothing to configure yet beyond `-addr`. The intended surface — bearer tokens per
(principal, machine), Ollama endpoint, Postgres DSN, repo roots — is specified in
[`docs/design/edd.md`](docs/design/edd.md) §4.3 and §7 and will be documented here as it lands.

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
