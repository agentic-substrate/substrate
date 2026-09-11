<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/hero-dark.svg">
    <img src="docs/assets/hero-light.svg" alt="Substrate is the durable context layer beneath coding harnesses on any machine. Scoped instructions, preferences, memories, and approved skills flow out to harnesses; observations and proposed changes flow back through provenance and review." width="960">
  </picture>
</p>

<h1 align="center">Substrate</h1>

<p align="center"><strong>Durable context for any coding agent, on any machine.</strong></p>

<p align="center">
  A self-hosted control plane for instructions, preferences, memories, and skills.<br>
  Keep what your agents learn. Choose where the next session runs.
</p>

<p align="center">
  <a href="docs/product/vision.md">Vision</a> ·
  <a href="ROADMAP.md">Roadmap</a> ·
  <a href="docs/ops/setup.md">Setup</a> ·
  <a href="CONTRIBUTING.md">Contribute</a>
</p>

<p align="center">
  <img alt="Status: pre-alpha" src="https://img.shields.io/badge/status-pre--alpha-orange">
  <img alt="Go 1.26.6+" src="https://img.shields.io/badge/go-1.26.6%2B-00ADD8?logo=go&logoColor=white">
  <img alt="License: Apache-2.0" src="https://img.shields.io/badge/license-Apache--2.0-blue">
  <img alt="MCP" src="https://img.shields.io/badge/protocol-MCP-black">
</p>

> **Pre-alpha.** Phase 1 establishes authority and sync. Memory tools, context compilation,
> rendering, and approved skill distribution are implemented; the broader learning and
> continuity workflows below are planned. Interfaces are not stable yet.

## Context should outlive the session

A project accumulates more than rules. It has decisions and their reasons, incidents and their
lessons, personal working preferences, and procedures worth repeating. When those live inside
one harness or on one laptop, every switch costs context.

Substrate puts that context in a durable layer beneath the tools you use. Claude Code, Codex,
Cursor, and headless workers consume the context they are allowed to see. Local `AGENTS.md`,
`CLAUDE.md`, and Cursor rules are rendered caches of that authority.

The long-term goal is that **switching agents or machines does not mean starting over**:
relevant knowledge follows the work, approved skills remain available, and checkpoints let a
new session continue the task. What a session learns can return with evidence and provenance,
without becoming a trusted fact merely because an agent said it.

## What belongs in Substrate

| Context | What it preserves | How it is treated |
|---|---|---|
| **Instructions** | Rules that must hold for a project, repo, or task | Deterministic scope resolution; never ranked or trimmed |
| **Preferences** | How a person or team prefers to work | A separate user/team/org overlay that cannot override instructions |
| **Memories** | Facts, decisions, incidents, lessons, and observations | Scope, visibility, provenance, verification, and status travel with each item |
| **Skills** | Reusable procedures and the versions approved for use | Git owns content and history; Substrate owns scope, approval, and active version |
| **Continuity** *(planned)* | Task checkpoints and handoffs | A fresh session can pick up the work across harnesses and machines |

These types share an authority and permission model, but keep their own rules. A memory is
quoted evidence, not an instruction. A skill index points to approved procedures; its bodies
are not stuffed into every context pack.

## How context flows

1. **Resolve the work's scope.** Instructions apply along
   `global → org → team → project → repo → branch → task → session`.
   Personal preferences resolve separately as `user > team > org`.
2. **Deliver effective context.** The server exposes a budgeted context pack through MCP
   `context.get`. The adapter separately renders instruction/preference files and materializes
   approved skill versions for each machine.
3. **Capture what happened.** Agents can write memories through MCP. Claude Code's current
   `PostToolUse` hook queues observations locally and the adapter drains them on reconnect.
4. **Keep learning accountable.** Agent writes enter as `unverified`. Local edits become review
   proposals. Planned verification, contradiction handling, and consolidation will help turn
   observations into reliable knowledge for future sessions.

Postgres holds context and skill state; Git holds skill content. Per-machine caches and an
outbox support offline operation. The [vision](docs/product/vision.md) explains the full
lifecycle and the boundary between today's implementation and the intended system.

## Available now and ahead

| Area | Implemented in pre-alpha | Planned |
|---|---|---|
| Shared authority | Scope resolution, instructions and preferences, deterministic rendering, import reconciliation and drift review | CI drift gate |
| Memory | Write, search, supersede, provenance and status; keyword retrieval with optional local embeddings | Trust-weighted feedback, contradictions, staleness, promotion and consolidation |
| Context | `context.get`, protected mandatory sections, budgeted memories and skill index | Ranking refinements, `context.explain`, pack caching |
| Skills | Scope/visibility-filtered manifest and linking of approved Git versions | Agent proposals through PRs and review; team pins |
| Capture and continuity | Claude Code tool observations, local outbox and memory cache | Session-start context injection, Codex hooks, summaries, checkpoints and handoffs |
| Fleet and governance | Self-hosted server, bearer identity, policy checks and audit | Task leases, GitHub-triggered workers, multi-team rollout, OIDC and review dashboard |

The [roadmap](ROADMAP.md) describes outcome-based horizons. The [PRD](docs/design/prd.md)
and [EDD](docs/design/edd.md) are the signed-off requirements and engineering design, including
future work; they are not a list of released features.

## Harness integration

| Client | Current integration | Planned continuity |
|---|---|---|
| Claude Code | `CLAUDE.md`, linked skills, `PostToolUse` observations | Session-start context, summaries and checkpoints; best-effort transcript offload |
| Codex | `AGENTS.md`, `~/.codex/AGENTS.md`, linked skills | Capture hooks and checkpoints; later thread offload |
| Cursor | `~/.cursor/rules/substrate.mdc`, linked skills; read-only integration | Checkpoint-based continuation |
| MCP clients and headless workers | `context.get`, `memory.write`, `memory.search`, `memory.supersede` | Task leases and checkpoint-based workers |

Full transcript migration is planned as a best-effort convenience for supported harness
versions. Portable structured checkpoints are the intended foundation for continuity.

## Try the current build

Requires Go 1.26.6+ to build. Store-backed operation needs Postgres with pgvector; integration
tests need Docker. See [Contributing](CONTRIBUTING.md) for the verification toolchain.

```sh
git clone https://github.com/agentic-substrate/substrate.git
cd substrate
make build
make smoke
```

The smoke check starts the real server without a database: `/healthz` returns 200 and
`/readyz` returns 503. It verifies process startup, not a configured context store.

Follow [Setup and reference](docs/ops/setup.md) for server configuration, tokens, MCP access,
import reconciliation, adapter installation, and rollback. Import is a three-step flow —
`substrate import scan` inventories a machine, `import plan` merges inventories into a
reviewable `plan.json`, and `import apply --inventory inventory.json` commits only the blocks
that inventory contains, so an edited plan cannot introduce a rule the scan never saw. The [operations runbook](docs/ops/runbook.md)
covers deployment, database roles, backups, and failure recovery. Optional Ollama embeddings
run locally; keyword-only retrieval is supported.

## Scope and ownership

Substrate is self-hosted and Apache-2.0 licensed, with no paid tier planned. It is designed for
one organization, with multi-team isolation on the roadmap. Your harness still runs the coding
session, and GitHub or Linear remains the human task tracker.

See [positioning](docs/product/positioning.md) and [non-goals](docs/product/non-goals.md) for the
product boundaries, including the limits of transcript portability and prompt-injection defenses.

## Documentation

| Start here | Purpose |
|---|---|
| [Vision](docs/product/vision.md) | The long-term context and learning lifecycle |
| [Problem](docs/product/problem.md) · [Positioning](docs/product/positioning.md) | Who this serves and why the layer exists |
| [Roadmap](ROADMAP.md) | Now, Next, and Later outcomes |
| [Setup and reference](docs/ops/setup.md) | Current commands, integrations, and configuration |
| [Operations runbook](docs/ops/runbook.md) | Deploy, migrate, monitor, back up, and restore |
| [PRD](docs/design/prd.md) · [EDD](docs/design/edd.md) | Reviewed requirements and design contracts |
| [Contributing](CONTRIBUTING.md) · [AGENTS.md](AGENTS.md) | Development workflow and repository invariants |
| [Security](SECURITY.md) | Security boundaries and reporting |

[Apache-2.0](LICENSE).
