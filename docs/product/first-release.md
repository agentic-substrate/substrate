# First usable release

**Last reviewed:** 2026-10-05 · **Re-read cadence:** before filing or closing a foundational epic

**Status:** agreed release boundary. The architecture and
artifact policies referenced below are agreed. Trusted local bindings, repository-scoped artifact
persistence, explicit local Git registration/approval, and scoped snapshot reads are implemented.
Scoped lexical retrieval, durable bounded maintenance with scoped browser status, and local
stdio MCP are implemented. Foreground Linux/WSL offline restart and synthetic resource evidence
are recorded in the [development guide](../development.md#foreground-maintenance-evidence);
services and continuous WSL availability remain unsupported.
Client-specific evidence records actual tested versions. Local browser artifact inspection and
exact human-reviewed publication of derived memories and owner offline plaintext
whole-installation backup/restore are implemented. Native activation remains unfinished. The browser is a local interface to the
foreground node, not evidence of complete release or remote deployment readiness.

## Job and scope

When I switch coding harnesses on one machine, I want the next session to retrieve the
permitted project artifacts and record useful observations, so knowledge survives the switch
without carrying Work material into Personal projects.

Use one owner, one node, and multiple harness sessions in one Linux environment or WSL 2
distro. Include memories, skills, and agent definitions from the first usable release. Start
with scoped discovery and content delivery, then add synchronization across environments.

This is the first step toward the [vision](vision.md), rather than the whole learning loop.
It must demonstrate that knowledge survives a harness switch, that reviewed versions and
permissions remain understandable, and that captured work is durable. Cross-machine continuity,
automatic knowledge maintenance, and native harness delivery build on that evidence later;
they are not prerequisites for this release.

| Boundary | Benefit | Tradeoff |
|---|---|---|
| Memories only | Smallest capture/recall implementation. | Does not validate source ownership or effective versions for skills and definitions. |
| All three artifact types on one node — selected | Tests the actual artifact model and harness reuse before distribution. | Requires explicit registration, revisions, and approved/effective states. |
| Multiple nodes immediately | Demonstrates device portability early. | Adds enrollment, transfer, shared grants, and replica conflicts before local usability is established. |

## Three initial outcomes

1. **Reuse the same permitted artifacts across harness sessions.** Set up the local owner once;
   bind repository identities and checkouts to explicit spaces. Save memory observations with
   provenance and status. Register skill and agent-definition sources, inspect their candidate
   and approved versions, and retrieve the selected content. Use ranked text, exact context,
   and explicit associations. Missing or ambiguous context requires a trusted binding.
2. **Understand and control Work/Personal boundaries.** Search, list, read, revise, and deliver
   artifacts according to the same session restrictions. Inspect content, source, applicability,
   effective version, permitted actions, and conflict state. Include local human review for
   cross-space publication, bound to exact content and destination. Before that review is
   implemented, prototypes keep publication blocked.
3. **Keep artifacts durable and maintenance observable.** Acknowledged saves survive supported
   shutdown/restart; backup and restore recover complete application state. Incremental indexing
   and bounded background work expose readiness, deferred work, and pause controls. Capturing
   authorized work remains durable when indexing is paused. Optional services preserve the same
   data directory, identity, access checks, and resource policy.

The browser initially shows spaces/repository bindings, artifact inspection and review, and
node/retrieval status. Local backup and fresh restore are trusted offline owner CLI commands.
Pairing, team administration, and remote coordinator controls
appear with those implemented capabilities. Empty, pending, blocked, and error states must
remain distinguishable from successful completion.

## Acceptance evidence

- Install and start the packaged application in the declared Linux and WSL 2 environments,
  without requiring a model runtime. Record the tested platform and client versions.
- Establish Work and Personal spaces with separate repositories; bind a sibling worktree to
  the correct logical repository through trusted registration. A manifest alone grants no access.
- From harness A, save an authorized Work observation and inspect its destination/status.
  From harness B in the Work worktree, retrieve that observation and approved skill/definition
  content. Demonstrate actual tool calls rather than treating process exit as integration proof.
- In a fresh Personal session, Work artifacts remain absent from search, listing, direct reads,
  aliases, related results, and error metadata. Tool arguments cannot enlarge session authority.
- Submit two edits against one artifact version. Preserve the stale candidate and accepted
  content, expose an authorized conflict, and resolve against the current version. Retrying the
  same contribution does not create duplicates; old edits cannot undo accepted retirement.
- Demonstrate exact-content local publication review for the first usable release. Changed merged content
  requires new review, and mandatory denial remains denial. Agent approval arguments do not
  satisfy human review. Unsupported publication is explicitly blocked across all surfaces.
- Disconnect networking, pause discretionary indexing, capture another observation, restart,
  resume indexing, and retrieve it. Evaluate representative queries, relevance, latency, and
  resource use; no performance or battery target is claimed before measurement.
- Back up and restore artifacts, bindings, revisions, pending work, and known retirement state;
  include all three kinds, declared bundles, selection/aliases, conflict history, publication
  policy/proposals/private completed audits, retry receipts, and durable pause/bulk/failure
  state with validated postings/checkpoints. Clear all scoped sessions and active/consumed
  review grants before fresh-destination promotion. Identify external Git sources needing their
  own backup and deny changed checkout identities. A snapshot cannot recover changes made
  after it. Later shared restores revalidate current authority before replay or delivery.

[SQLite backup](https://www.sqlite.org/backup.html) provides a snapshot mechanism; complete
application recovery remains a product responsibility. [SQLite FTS5](https://www.sqlite.org/fts5.html)
provides lexical primitives; useful recall requires actual query evaluation.

## Harness promise

Target basic MCP compatibility with Codex, Cursor Agent, Claude Code, and OpenCode, and state
tested versions. Each advertised client needs discovery, capture, search, reads, explicit
denials, concurrent sessions, and unavailable-node checks. A cross-harness journey complements
those per-client checks. Standard protocol support alone is not proof of compatibility.

Discovering and reading an approved skill or agent definition does not install a native harness
feature or run an agent. Native rendering/installation is supported only for implemented,
validated adapters and compatible governance requirements. Unsupported native activation is
explicit, with a permitted preview or next step. Compare [MCP tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools)
with [native skills](https://code.claude.com/docs/en/skills) and
[agent definitions](https://code.claude.com/docs/en/sub-agents).

Scope checks cover Substrate-mediated delivery. Independently loaded files and previously
delivered conversation content need the already agreed runtime/isolation treatment; switching
a session binding cannot retract content that the harness has already received.

## Next and later horizons

Next, enroll a second WSL/Linux node, selectively synchronize authorized artifacts, and test
disconnection/reconnection under the agreed [offline](../architecture/offline.md) and
[reconciliation](../architecture/reconciliation.md) contracts. Human-friendly pairing and replica conflict
review arrive with that capability. Teams and organizations follow with their actual authority,
membership, and administration controls. macOS, native Windows, automatic coordinator failover,
optional embeddings, and native desktop convenience remain future scope.

Before those network paths carry customer/internal data, meet the
[security roadmap's transfer gates](../plans/security.md#capability-gates). Enterprise scoped
content recovery and optional hub management/metrics are confirmed later direction; key
custody, recovery granularity, and offline enforcement remain open. They do not expand this
single-node release into a transfer system or an agent execution engine.

See [delivery](../architecture/delivery.md), [packaging](../architecture/packaging.md), [artifacts](../architecture/artifacts.md),
[repository bindings](../architecture/repositories.md), and [deployment](../architecture/deployment.md).
