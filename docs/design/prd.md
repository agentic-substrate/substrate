# Product Requirements Document — Agent Control Plane (v2)

| | |
|---|---|
| **Status** | v2.1 (post EDD round-2 review) |
| **Date** | 2026-09-05 |
| **Owner** | Jeremy |
| **Companion doc** | `agent-control-plane-design.md` v2 |
| **Working name** | Agent Control Plane (ACP) |

**Changes from v1:** canonical scope chain with `repo` and `session`; instructions and preferences are first-class records, not memory kinds; Git holds skill content, Postgres holds skill state; audit log replaces event sourcing; trust-weighted contradiction policy; symbol-level staleness as the target; new requirements for context introspection, memory feedback, and verification type; Redis deferred; two-tier continuity; phases re-cut with a tighter Phase 1.

---

## 1. Problem statement

Every AI coding harness in use — Claude Code, Codex, Cursor, and any headless worker — keeps its own memory, instruction files, skills, and session state on whichever machine it ran. The agent is treated as durable and the knowledge as disposable, when it should be the reverse. Switching harnesses, switching machines (WSL laptop ↔ Mac Mini ↔ Kubernetes pod), or bringing a teammate onto a project means re-explaining context, re-discovering past failures, and hand-syncing instruction files that have already drifted.

The cost recurs every session and grows multiplicatively with agents, machines, and people: repeated context reconstruction, agents acting confidently on stale or contradictory facts, duplicated skills that diverge, and no way to move in-progress work to more capable hardware.

## 2. Vision

One authoritative, self-hosted control plane where **agents are disposable and accumulated intelligence is persistent**. The product is the function

```
(principal + team + project + repo + branch + task + current code) → effective context
```

delivered to any harness on any machine, with what the harness learns flowing back under provenance and trust.

## 3. Goals

| # | Goal | Measure |
|---|---|---|
| G1 | Eliminate context reconstruction when switching harness or machine | A session on machine B receives the same effective instructions, preferences, and relevant facts as machine A with no manual file copy |
| G2 | One source of truth for instructions, preferences, and skills | Zero hand-edited `CLAUDE.md` / `AGENTS.md` / `.cursor/rules` on managed machines; drift check passes on 100% |
| G3 | Memory improves without curation and does not become confidently wrong | ≥ 90% of served memories are `confirmed`/`probable`; `code`-verified memories flagged stale within 24h of a referenced symbol changing |
| G4 | Work continues across machines by default, and full sessions migrate when possible | Tier-1 (checkpoint) continuity succeeds 100% at a turn boundary; Tier-2 (transcript) offload succeeds ≥ 95% under pinned versions, in ≤ 2 min |
| G5 | Second team onboarded with isolated context and inherited org rules | RLS leak tests green; team sees own rules + org rules and not the other team's private memory |

## 4. Non-goals

| Non-goal | Rationale |
|---|---|
| A new coding harness or IDE | ACP sits beneath harnesses and makes them interchangeable |
| Replacing GitHub/Linear as the human tracker | Tracker stays the human UI (Symphony pattern); ACP mirrors tasks |
| Cloud/multi-tenant SaaS | Single org, self-hosted. Multi-team ≠ multi-tenant |
| Cursor IDE session migration | IDE-bound; Tier-1 continuity only |
| Transcript portability as a foundational dependency | Harness storage formats are private and version-gated; Tier 2 is best-effort by design |
| Event-sourced state | An audit log gives the needed history; full event sourcing adds projection/replay complexity without payoff at this scale |
| Auto-promotion to `global` scope | Always human review |

## 5. Personas

| Persona | Primary needs |
|---|---|
| **Solo operator (Jeremy)** | Sync across WSL / Mac Mini / pods; continuity; memory that survives |
| **Team member** | Correct rules and own preferences on day one; private team memory |
| **Team lead** | Team-scoped review queue; audit of changes |
| **Org admin** | Global/org rules, OIDC, permissions without bottlenecking |
| **Autonomous worker** | Minted identity, context pack, lease, checkpointing |

## 6. User stories (priority order)

**Solo operator**
- I want every machine to generate its instruction files from one source so I never hand-edit `CLAUDE.md` twice.
- I want a memory written on one machine searchable from another within a minute so switching laptops costs nothing.
- I want a session I stop on WSL to continue from its checkpoint in a pod, and — when versions match — to resume the actual conversation.
- I want a one-time import of drifted memory, rules, and skills from each machine, with conflicts shown to me rather than resolved by "last sync wins."
- I want to ask "why is this in my context?" and "why isn't memory X?" so I can debug agent behavior.
- I want memories flagged stale when the code they describe changes.

**Team member**
- I want my preferences applied on any project, and to be told when a team rule overrides one.
- I want my team's incidents and failed approaches visible to my agents but hidden from other teams.
- I want to propose a skill from a repeated workflow without editing a shared repo by hand.

**Team lead / org admin**
- I want a review queue of proposed team memories, instructions, and skills.
- I want an audit trail answering "who changed rule X" and "why do agents believe Y."
- I want agent permissions derived from the launching user so no agent has standalone authority.

**Autonomous worker**
- I want to claim a task with a lease and checkpoint so another worker resumes if I crash.
- I want a context pack sized to my budget.

## 7. Prior art and research

Surveyed 2026-09-05. Three layers are solved or maturing; the fourth is the gap.

- **Shared memory across harnesses (mature, crowded):** Memorix (local-first SQLite, MCP, hooks, Git-derived memory, review-gated long-term memory, opt-in shared HTTP mode; per-machine store is its limit), Mori (hook capture + session distillation), AgentMemory (shared server), Graphiti/Cognee (graph backends).
- **Task orchestration (maturing):** OpenAI Symphony (board-as-control-plane pattern), NEEDLE (SQLite bead queue with atomic claims), Project Supervisor (leases, evidence manifests, typed handoff contracts).
- **Config and skill sync (solved):** Agent Skills / `SKILL.md` standard (agentskills.io; adopted by Claude Code, Codex, Cursor, Copilot, Gemini CLI and 20+ others), agentsync-vcs, agent-rules-sync, `AGENTS.md` as the cross-tool instruction file.
- **Session migration (workable, undocumented):** Claude Code transcripts at `~/.claude/projects/<cwd-slug>/<uuid>.jsonl`, resumable on any machine under a matching slug; community tools confirm it and confirm it is version-gated.
- **The gap:** no surveyed project combines, on one store, a normalized scope chain with deterministic instruction resolution, provenance and verification state, trust-weighted conflict handling, symbol-linked staleness, budgeted and explainable context compilation, a permissioned review queue, and adapters that treat local files as caches. That is ACP's build scope; everything else is borrowed.

## 8. Requirements

### 8.1 Scopes and identity (SCOPE)

| ID | Pri | Requirement | Acceptance criteria |
|---|---|---|---|
| SCOPE-1 | P0 | Canonical scope chain `global → org → team → project → repo → branch/worktree → task → session`, with `user` as an orthogonal overlay root. Used identically by every table and by the compiler | Given a memory at `repo:plotlens/api`, when compiling for `branch:feature-x` under that repo, then it is a candidate; when compiling for `repo:plotlens/web`, then it is not |
| SCOPE-2 | P0 | Projects belong to one team; cross-team access via `project_grant` | — |
| SCOPE-3 | P0 | Applicability (`scope_id`) and visibility (`owner`/`team`/`org`/`global`) are independent fields on instructions, preferences, memories, and skills; visibility enforced by Postgres RLS | Given team A's `team`-visible memory, when a team-B member queries, then it is absent even if the service omits its filter |
| SCOPE-4 | P0 | Principals carry `trust_level` ∈ {human_admin, human, agent_interactive, agent_autonomous}; agent principals are minted by a user and inherit a subset of that user's memberships and permissions | Given a worker minted by U on team T, when it writes to org scope, then the write is rejected |
| SCOPE-5 | P0 | Phase 1 auth: per-machine bearer tokens bound to one user. Phase 7: OIDC JWTs with subject and team claims | — |
| SCOPE-6 | P1 | Write policy: user scope free; team scope open to members, `confirmed` requires `lead`; org/global always via review | — |

### 8.2 Instructions and preferences (INST)

| ID | Pri | Requirement | Acceptance criteria |
|---|---|---|---|
| INST-1 | P0 | Instructions (`rule`/`constraint`/`convention`) are first-class records with `scope_id`, `key`, `body`, `status`, separate from memory. They are never subject to retrieval scoring | Given an active instruction at `project:plotlens`, when any pack is compiled for that project, then the instruction is present regardless of task text or budget pressure |
| INST-2 | P0 | Resolution along the org chain by `key`: most specific active instruction wins; distinct keys accumulate; users cannot override | Given `python.version=3.13` at global and `python.version=3.12` at project, then the pack contains 3.12 only |
| INST-3 | P0 | Preferences resolve `user > team > org` and are emitted only where no effective instruction shares the `key`; suppression is reported in one line | Given user preference `indent=tabs` and project instruction `indent=spaces`, then the pack contains `spaces` and a one-line note that the preference was suppressed |
| INST-4 | P0 | Generated `AGENTS.md`/`CLAUDE.md`/Cursor rules are renderings of the effective instruction + preference set for (user, machine, repo) | Byte-identical output for identical inputs |
| INST-5 | P1 | `instruction.propose` enters the review queue; instruction changes at org/global require `admin` | — |

### 8.3 Memory (MEM)

| ID | Pri | Requirement | Acceptance criteria |
|---|---|---|---|
| MEM-1 | P0 | Every memory carries `scope_id`, `visibility`, `tier`, `kind` ∈ {fact, decision, incident, lesson, observation}, `status` ∈ {confirmed, probable, unverified, conflicted, deprecated, superseded}, `source` provenance, and `verification` ∈ {code, human, agent_inference} with referents | Given a write missing scope, source, or verification, then it is rejected |
| MEM-2 | P0 | Supersession never deletes; superseded rows are excluded from default reads and retained with an edge | — |
| MEM-3 | P0 | Hybrid retrieval: pgvector + FTS/trigram over title, body, and extracted identifiers; exact identifier hits outrank semantic hits | Given a memory containing `validation.py`, when queried by filename, then it is in the top 3 |
| MEM-4 | P0 | Default reads return `semantic` tier with `confirmed`/`probable` only; episodic/unverified on explicit request | — |
| MEM-5 | P0 | Local embeddings (Ollama on the T4); keyword-only degradation when unavailable | Given Ollama down, then `memory.search` returns within 2s |
| MEM-6 | P0 | `memory.feedback(id, useful?, incorrect?, reason?)` records trust-weighted evidence; feedback alone never changes `status` | Given 50 `incorrect` votes from `agent_autonomous` principals, then status is unchanged and a review item exists |
| MEM-7 | P1 | Trust-weighted contradiction policy: a `confirmed` memory is never downgraded by an `unverified` agent observation; conflicts create a `contradicts` edge and a review item; two `probable` memories in conflict both become `conflicted`; a `code`-verified memory validated at HEAD may flag the existing one stale | Given confirmed A and an autonomous agent writes contradicting B, then A remains `confirmed`, B is `unverified`, an edge and review item exist |
| MEM-8 | P1 | Staleness: Phase 2 file-level heuristic explicitly labeled approximate; Phase 4 symbol-level (tree-sitter) against `verification.symbols`/config keys/schemas. Stale memories drop a ranking tier and show the flag; they are not auto-deprecated | Given a `code`-verified memory referencing `VALIDATION_ORDER`, when that symbol's value changes in one line, then the memory is flagged within 24h; when only formatting changes, then it is not |
| MEM-9 | P1 | Promotion: nightly `unverified → probable` on ≥ 2 independent corroborations or code re-validation; `→ confirmed` by human per scope policy | — |
| MEM-10 | P1 | Consolidation: session-end summarization to episodic; nightly dedupe; weekly roll-up of episodic > 30 days | — |
| MEM-11 | P2 | Additional graph edges (`caused_by`, `relates_to`, `uses_skill`) queryable for structured reasoning | — |

### 8.4 Context compilation (CTX)

| ID | Pri | Requirement | Acceptance criteria |
|---|---|---|---|
| CTX-1 | P0 | `context.get` emits, in order: effective instructions, effective preferences, mandatory items (open handoff, broadcasts, unresolved review items for the scope), then retrieved memories and skill index; every item carries a footer (id, status, source, verification type, last verified) | Phase 1 may implement retrieval as keyword-only |
| CTX-2 | P0 | Per-section token budgets with priority packing; instructions, preferences, and mandatory items are never cut; skill bodies are never inlined | Given budget 12k, then the pack's size under the documented conservative estimator (see EDD §5.2) is ≤ 12k, and all instructions are present. The estimator, not the harness tokenizer, is the contract |
| CTX-3 | P1 | Ranking = relevance × recency × status weight × scope specificity × trust-weighted feedback ratio (floored) | — |
| CTX-4 | P1 | `context.explain(pack_id, item_id?)` returns include/exclude reasons with score components for every candidate, including items not included | Given memory X excluded as superseded, when explained, then the response names the superseding id |
| CTX-5 | P1 | Pack caching keyed by (scope, task, git sha), invalidated by audit events | — |

### 8.5 Skills (SKILL)

| ID | Pri | Requirement | Acceptance criteria |
|---|---|---|---|
| SKILL-1 | P0 | Git is authoritative for skill content and history (Agent Skills `SKILL.md` format); Postgres is authoritative for skill state (scope, visibility, active version, approval, retrieval metadata); `skill_version.git_sha` links them | Given `active_version_id` moved to an older version, when the adapter pulls, then the linked directory matches that version's `git_sha` |
| SKILL-2 | P0 | Adapter links only skills whose scope and visibility match the machine's user/teams/repos | — |
| SKILL-3 | P1 | `skill.propose` creates a branch + PR and a review item; nothing links until approved | — |
| SKILL-4 | P1 | Per-team namespaces; org skills inheritable with per-team pin | — |

### 8.6 Sync and reconciliation (SYNC)

| ID | Pri | Requirement | Acceptance criteria |
|---|---|---|---|
| SYNC-1 | P0 | `cp-adapter` per machine renders instruction/preference files and links skills, with a "generated — do not edit" header; pulls on start, every 5 min, and on broadcast | Given a server change, then all managed files match the server rendering within 5 min |
| SYNC-2 | P0 | Local outbox for hook-captured observations; drains on reconnect; zero loss | Given 1h offline and 20 observations, then all 20 arrive within 2 min of reconnection |
| SYNC-3 | P0 | Offline read cache of `confirmed`/`probable` memory for present repos; degraded keyword `context.get` | — |
| SYNC-4 | P0 | Drift protection: hand-edited generated file → diff → `drift_proposal` review item → regenerate | Given a manual edit, then a review item contains the diff and the file is restored |
| SYNC-5 | P0 | `cp import`: inventory, classify blocks as instruction/preference at implied scope, dedupe by hash, route divergent blocks/skills to `import_conflict` review with hostname, import memory stores as `episodic/unverified` | Given two machines with differing `~/.claude/CLAUDE.md`, then identical blocks appear once and each differing block is a review pair tagged with hostname |
| SYNC-6 | P0 | Conflict rule: server wins for `confirmed`/`probable`; local wins for `working`; else review. Never last-sync-wins | — |
| SYNC-7 | P1 | CI drift gate for repos | — |

### 8.7 Continuity and offload (CONT)

| ID | Pri | Requirement | Acceptance criteria |
|---|---|---|---|
| CONT-1 | P0 | **Tier 1 (guaranteed):** every `Stop`/`PreCompact` writes a structured checkpoint; a harness started anywhere with `context.get(task)` receives it at top priority | Given a checkpoint from WSL, when a fresh session starts in a pod, then its first turn continues the task without re-explanation |
| CONT-2 | P0 | Tier 1 works across harness versions and across harness types (Claude Code → Codex) | — |
| CONT-3 | P1 | **Tier 2 (best effort):** `cp offload`/`cp reclaim` commit to `wip/<session>`, upload transcript + sidecars to MinIO (`owner`, encrypted), write checkpoint, provision pinned pod, restore transcript to matching cwd slug, `--resume` in tmux | ≤ 2 min; next assistant turn references earlier conversation facts unprompted |
| CONT-4 | P1 | Identical absolute repo paths on all machines and images (`/work/<project>/<repo>`) | — |
| CONT-5 | P1 | Refuse offload mid-tool-call; on any Tier-2 failure fall back to Tier 1 automatically and log it | Given a version mismatch, then the target opens a fresh session from the checkpoint |
| CONT-6 | P1 | Harness versions pinned; `cleanupPeriodDays` raised; `--fork-session` option; Cursor is Tier 1 only | — |
| CONT-7 | P2 | Codex thread offload under the same contract | — |

### 8.8 Capture and review (CAP)

| ID | Pri | Requirement |
|---|---|---|
| CAP-1a-capture | P0 | Claude Code `PostToolUse` → outbox observation, installed by merging into `~/.claude/settings.json`; scope derived from the checkout's git remote and resolved server-side. **Phase 1** |
| CAP-1a-context | P0 | Claude Code `SessionStart` → `context.get` with an offline instruction-pack fallback. **Deferred to Phase 2**: no client path to `context.get` outside MCP and no cached instruction pack exists, and Claude Code already auto-loads the rendered `CLAUDE.md`, so the marginal value is retrieved memories and mandatory items. PRD amended |
| CAP-1b | P0 | Claude Code hooks, Phase 2: `Stop`/`PreCompact` → episodic summary + checkpoint |
| CAP-2 | P1 | Codex hooks with the same contract; Cursor read-only |
| CAP-3 | P1 | Session record (task, agent, machine, repo, branch, files, tools, errors, decisions, outcome) summarized, not retained verbatim |
| CAP-4 | P1 | Review queue kinds: promotion, instruction_change, skill_proposal, contradiction, drift_proposal, import_conflict; per-team scoping; org/global require admin |
| CAP-5 | P2 | Agent-generated skill proposals with session evidence |

### 8.9 Tasks and fleet (TASK)

| ID | Pri | Requirement |
|---|---|---|
| TASK-1 | P1 | Task records with objective, project, repo, priority, status, dependencies, assignee, completion criteria, external ref |
| TASK-2 | P1 | Leases in Postgres (`FOR UPDATE SKIP LOCKED`, `lease_expires_at`, sweeper); expiry returns task to `ready` with last checkpoint as resume point |
| TASK-3 | P1 | Typed handoffs; receiver's pack includes the open handoff as a mandatory item |
| TASK-4 | P1 | GitHub-triggered k8s Job workers with minted identity, context pack, worktree, GPU node selection |
| TASK-5 | P2 | Presence, advisory locks, messaging, broadcast, capability routing, machine registry; Redis evaluated here if Postgres coordination becomes the bottleneck |
| TASK-6 | P2 | Dependency-aware pipelines |
| TASK-7 | P2 | Dashboard |

### 8.10 Audit and governance (GOV)

| ID | Pri | Requirement |
|---|---|---|
| GOV-1 | P0 | Every mutation writes transactional state and one append-only audit row (`actor, action, subject, before, after, reason`) in the same transaction. State is never reconstructed from the log |
| GOV-2 | P0 | Answerable: "who changed instruction X and when"; "why do agents believe Y" via `source` + `supersedes` chain |
| GOV-3 | P0 | Instruction and skill changes are reversible from the audit `before` image |
| GOV-4 | P0 | Transcripts and blobs encrypted at rest, `owner`-visible, never indexed into semantic memory |

## 9. Architecture summary

See the design doc. Phase 1 footprint: PostgreSQL (+pgvector, FTS, RLS), Git (skills), MinIO, Ollama on the T4. One core service (Go, or Python FastMCP for Phase 1) exposing ~12 MCP tools over streamable HTTP behind NGINX on Tailscale; `cp-adapter` per machine; `cp` CLI. Kubernetes for pods and workers. Redis deferred to fleet phase.

## 10. Success metrics

**Leading (weeks 1–4):** 100% of managed machines pass drift check; cross-machine memory visibility ≤ 1 min; Tier-1 continuity 100% at turn boundary; Tier-2 offload ≥ 95% and ≤ 2 min p95; pack budget compliance 100%; zero outbox loss across ≥ 10 disconnects.

**Lagging (months 1–3):** ≥ 90% of served memories `confirmed`/`probable`; symbol-level staleness latency ≤ 24h; < 5 review items older than 7 days; feedback ratio used in ranking with measurable lift on `explain` audits; second team live with RLS tests green; self-reported "re-explained the project" incidents trending to zero.

## 11. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Harness transcript format changes | Tier 1 is the dependency; Tier 2 is optional; version pinning; failure auto-falls back (CONT-5) |
| Low-trust agents destabilize trusted knowledge | Trust-weighted contradiction (MEM-7); feedback never changes status (MEM-6); human-only promotion at org/global |
| Mandatory rules dropped by retrieval or budget | Instructions are a separate deterministic table, never cut (INST-1, CTX-2) |
| Stale memory acted on | Verification referents + staleness flags (MEM-8); status gating (MEM-4) |
| Over-building | Phase 1 is authority + sync only; compiler ranking and hooks in Phase 2; tasks in Phase 6 |
| Cross-team leak | RLS in Postgres independent of service code; leak tests in CI |
| Homelab SPOF | Offline cache + outbox; Postgres backups to MinIO |
| Third-party tools close the gap | Import-friendly store keeps switching cost low; re-evaluate after Phase 2 |

## 12. Open questions

| Question | Owner | Blocks |
|---|---|---|
| Go from day one vs Python FastMCP for Phase 1 | Jeremy | Phase 1 |
| Embedding model and dimension on the T4 | Jeremy | Phase 1 (re-indexable) |
| Canonical mount path `/work/<project>/<repo>` | Jeremy | Phase 3 |
| Skills repo layout: one org repo with `team/<name>/` vs per-team repos | Team leads | Phase 5 |
| Human tracker for the Symphony loop: GitHub Issues vs Linear | Team | Phase 6 |
| OIDC provider | Jeremy / team | Phase 7 |
| Corroboration threshold for `unverified → probable` | Jeremy | No — tune in Phase 4 |

## 13. Phasing

| Phase | Scope | Requirement IDs | Exit criterion |
|---|---|---|---|
| **1 — Authority + sync** | Schema; `context.get` (instructions + preferences + keyword memory); `memory.search/write/supersede`; `cp-adapter` pull-config/pull-skills/push-memory; Ollama embeddings (direct from Go); `PostToolUse` → outbox hook; reconciliation on WSL + Mac Mini | SCOPE-1..5, INST-1..4, MEM-1..5, CTX-1..2, SKILL-1..2, SYNC-1..6, CAP-1a-capture, GOV-1..4 | Identical generated config on both machines; import conflicts resolved; memory written on one machine searchable on the other ≤ 1 min |
| **2 — Compiler + capture + Tier-1 continuity** | Ranking, `explain`, `feedback`, `SessionStart` → `context.get`, Claude Code + Codex hooks, checkpoints, file-level staleness | CTX-3..5, MEM-6, MEM-8 (file-level), CAP-1a-context, CAP-1b, CAP-2..3, CONT-1..2 | Cold start on machine B from checkpoint continues the task |
| **3 — Raw session offload (Tier 2)** | Pinned pod image, `cp offload`/`reclaim`, MinIO transcripts, fallback | CONT-3..6 | 10 consecutive WSL ↔ pod round-trips |
| **4 — Lifecycle + review** | Promotion, trust-weighted contradictions, symbol-level staleness, review queue UI | MEM-7, MEM-8 (symbol), MEM-9..10, CAP-4, INST-5, SCOPE-6 | ≥ 90% served memories confirmed/probable |
| **5 — Skills governance** | Proposals, versioning UI, namespaces | SKILL-3..4, CAP-5 | First agent-proposed skill approved |
| **6 — Tasks + workers** | Postgres leases, handoffs, GitHub-triggered Jobs | TASK-1..4 | First issue → PR with zero manual context |
| **7 — Teams + fleet + dashboard** | OIDC, memberships, RLS tests, presence/messaging/routing, dashboard, Redis if needed | SCOPE-5 (OIDC), TASK-5..7, MEM-11, CONT-7, SYNC-7 | Second team live; RLS leak tests green |

## 14. Appendix A — Capability-to-requirement map

| Cap. | Title | Req. | Phase |
|---|---|---|---|
| 1 | Shared global memory | MEM-1, SCOPE-1 | 1 |
| 2 | Project-scoped memory | SCOPE-1, SCOPE-3 | 1 |
| 3 | Hierarchical context scopes | SCOPE-1, INST-2, INST-3 | 1 |
| 4 | Intelligent context compilation | CTX-1..5 | 1/2 |
| 5 | Persistent session history | CAP-3 | 2 |
| 6 | Cross-agent handoffs | TASK-3, CONT-2 | 2/6 |
| 7 | Cross-machine continuity | SYNC-1..3, CONT-1..6 | 1–3 |
| 8 | Shared skill registry | SKILL-1..2 | 1 |
| 9 | Dynamic skill discovery | CTX-1, MEM-3 | 1 |
| 10 | Skill versioning | SKILL-1 | 1 |
| 11 | Agent-generated skill proposals | SKILL-3, CAP-5 | 5 |
| 12 | Memory lifecycle | MEM-1 (tiers), MEM-10 | 1/4 |
| 13 | Promotion and consolidation | MEM-9..10 | 4 |
| 14 | Memory supersession | MEM-2 | 1 |
| 15 | Provenance | MEM-1, GOV-2 | 1 |
| 16 | Confidence / verification state | MEM-1 (status + verification) | 1 |
| 17 | Shared task system | TASK-1 | 6 |
| 18 | Task claiming and leases | TASK-2 | 6 |
| 19 | Resource / file coordination | TASK-5 | 7 |
| 20 | Agent presence | TASK-5 | 7 |
| 21 | Agent identity | SCOPE-4 | 1 |
| 22 | Capability-aware assignment | TASK-5 | 7 |
| 23 | Inter-agent messaging | TASK-5 | 7 |
| 24 | Broadcast messaging | TASK-5, CTX-1 | 7 |
| 25 | Event-driven agents | TASK-4 | 6 |
| 26 | Disposable workers | TASK-4 | 6 |
| 27 | Branch / worktree awareness | SCOPE-1 | 1 |
| 28 | Repository awareness | SCOPE-1 (`repo`) | 1 |
| 29 | Git integration | MEM-1 (`verification.code`) | 1 |
| 30 | Knowledge freshness detection | MEM-8 | 2/4 |
| 31 | Human review queues | CAP-4 | 4 |
| 32 | Permissions and trust levels | SCOPE-4, SCOPE-6 | 1/4 |
| 33 | Audit history | GOV-1..3 | 1 |
| 34 | Centralized configuration | INST-4, SYNC-1 | 1 |
| 35 | Client adapters | SYNC-1, SKILL-2 | 1 |
| 36 | Offline / local caching | SYNC-2..3 | 1 |
| 37 | Conflict detection | MEM-7, SYNC-5..6 | 1/4 |
| 38 | Knowledge graph relationships | MEM-2, MEM-11 | 1/7 |
| 39 | Semantic retrieval | MEM-3 | 1 |
| 40 | Exact / full-text retrieval | MEM-3 | 1 |
| 41 | Context budgets | CTX-2 | 1 |
| 42 | Automatic task checkpointing | CONT-1, TASK-2 | 2/6 |
| 43 | Context-window rollover | CAP-1 (`PreCompact`), CONT-1 | 2 |
| 44 | Agent specialization | SCOPE-4 (roles as permission sets) | 6 |
| 45 | Multi-agent workflows | TASK-6 | 7 |
| 46 | Dependency-aware workflows | TASK-1, TASK-6 | 6/7 |
| 47 | Scheduled agents | MEM-9..10, TASK-4 | 4/6 |
| 48 | Shared infrastructure knowledge | MEM-1 at org scope | 1 |
| 49 | Machine capability registry | TASK-5 | 7 |
| 50 | Central dashboard | TASK-7 | 7 |
| — | Instructions as first-class records (added v2) | INST-1..5 | 1 |
| — | Context introspection (added v2) | CTX-4 | 2 |
| — | Memory feedback (added v2) | MEM-6 | 2 |
| — | Verification type (added v2) | MEM-1 | 1 |
| — | Trust-weighted contradiction (added v2) | MEM-7 | 4 |
| — | Multi-team scopes and user overlay | SCOPE-1..6 | 1/7 |
| — | Session offload WSL ↔ pod | CONT-3..7 | 3 |
| — | Drifted-machine reconciliation | SYNC-4..6 | 1 |

## 15. Appendix B — Sources consulted

Memorix (github.com/AVIDS2/memorix); Mori, AgentMemory, Graphiti, Cognee via awesome-ai-agents-2026 and XDA coverage; OpenAI Symphony (openai.com), InfoQ and MindStudio analyses; NEEDLE and Project Supervisor via awesome-agent-orchestrators / PyPI; Agent Skills specification (agentskills.io) and adoption coverage; agentsync-vcs, mujin-agentsync, agent-rules-sync, skills-sync; Claude Code session storage and migration — anthropics/claude-code issues #58591, #58725, #69585; claude-code-migrate; claude-codex-bridge. External architectural review incorporated in v2.
