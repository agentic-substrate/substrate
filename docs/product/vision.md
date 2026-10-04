# Vision: context that compounds across sessions

**Last reviewed:** 2026-10-03 · **Re-read cadence:** at each phase exit

**Design status:** Go is selected for the backend, CLI, and MCP implementation. CLI, MCP, and
a Vite + React browser administration interface are agreed for the first usable release.
The browser's static build will be embedded in the Go executable; exact screens remain open.
Initial platform coverage is Linux and WSL 2, with macOS and native Windows later.
Locally runnable nodes, selective synchronization, and shared authority for team and
organization policy are the agreed architecture direction. Initial node synchronization uses
authenticated HTTPS to configured,
reachable endpoints. Hybrid retrieval, work/personal separation, group restrictions,
repository-level artifacts, and per-item cross-space publication review are required.
Administrator-controlled finite offline grants for ordinary shared artifacts, online-only
sensitive artifacts, and independent wholly owned Personal operation are also agreed.
SQLite is selected for local nodes and the initial single coordinator. The Go driver, vector
backend, embedding runtime/model, credential protocols and identity-provider integrations,
synchronization mechanisms, and concrete interface transports remain open decisions.
Initial retrieval combines ranked text, exact identifiers/scope, and explicit semantic
associations without neural inference. Embeddings are an optional candidate that must justify
their quality and resource costs; battery use during interactive work constrains the design.

Easy user joins and administration are major product goals. A checked-in repository manifest
supports portable discovery; trusted registration and current grants control access. Captured
memory belongs in Substrate records, with optional reviewed Git knowledge and versioned skill
and agent-definition sources. Enrollment starts with local ownership and pairing, then
administrator-verified early-team invitations and later optional organization OIDC. Manifest
format, credential protocols, and implementation of the enrollment experience remain open.

The [deployment requirements](../architecture/deployment.md) cover standalone use, WSL distributions,
multiple machines, teams, and organizations, including disconnected operation.
The [retrieval research](../architecture/retrieval.md) evaluates how locally available memories can be found.
The [embedding research](../architecture/embeddings.md) describes a conditional model-based retrieval path,
including local/remote processing and encoder compatibility. Its need and deployment default
must follow retrieval-quality and resource evaluation.
The [sharing requirements](../architecture/sharing.md) cover group access, storage placement, replication,
and controlled reuse across work and personal projects.
The [repository research](../architecture/repositories.md) covers repository identity, worktrees, nested
scopes, existing harness artifacts, and source-storage options.
The [onboarding contract](../architecture/onboarding.md) covers the agreed enrollment sequence, personal setup,
teammate invitations, device pairing, recovery requirements, and later optional organization SSO.
The [delivery research](../architecture/delivery.md) records agreed CLI/MCP access and an embedded Vite + React
browser administration interface. Exact screens, node lifecycle, and concrete platform
packaging remain pending. The [installation contract](../architecture/packaging.md) records the initial
platform scope and recommendations for foreground operation, optional user services, and
update/recovery requirements.
The agreed [reconciliation contract](../architecture/reconciliation.md) defines conflict handling by artifact
type, with independent contributions, preserved revision candidates, and current authority
checks. Wire formats, retention, and implementation remain open.
The agreed [first-release contract](first-release.md) scopes one owner and one Linux/WSL node across
all three artifact types, with multi-environment synchronization following. Its acceptance criteria guide the foundational roadmap.
The [artifact contract](../architecture/artifacts.md) defines the agreed mapping for precedence, search eligibility,
segmentation, approval, and mandatory organization restrictions. Artifacts means memories,
skills, and agent definitions throughout this design.

Substrate's long-term goal is a durable context layer beneath any coding harness on any
machine, usable locally and shared across installations. Instructions, preferences, memories,
skills, and task continuity should survive the session that used or discovered them.
Choosing another tool should not mean losing what the project has learned.

File rendering is one delivery mechanism. The broader product is a lifecycle: deliver the
right context, capture useful evidence from work, review and verify changes, and make the
result available to the next session.

This page explains the intended system. The [getting started guide](../getting-started.md)
identifies current capabilities; the [roadmap](https://github.com/agentic-substrate/substrate/blob/main/ROADMAP.md) groups future outcomes.
Implementation requirements and interface contracts must be reviewed before their roadmap
items are built. This vision is a direction, not a claim that those contracts exist today.

## Durable context

**Instructions establish constraints.** Resolve the most specific active instruction for each
key along `global → org → team → project → repo → branch → task → session` within the
session's authorized context spaces. A Work-wide instruction does not apply to Personal
merely because its applicability scope is global. Instructions remain deterministic and
are never scored or removed to make room for memories. Instruction specificity cannot relax
mandatory access, placement, or processing policies.

**Preferences preserve working choices.** A person's preferences can follow them between
permitted projects and machines. Work-specific preferences remain within their authorized
spaces until explicitly shared. The `user > team > org` overlay is separate from instruction
scope; a personal preference cannot override a project instruction.

**Memories preserve knowledge and its evidence.** Facts, decisions, incidents, lessons, and
observations carry scope, visibility, source, verification, and status. Agents write
`unverified`. Superseding an item preserves its history. Planned feedback, contradiction
handling, code staleness checks, and consolidation help maintain useful knowledge as the
project changes; feedback alone never changes status.
Repository knowledge must work across authorized checkouts without conflating it with
branch observations or checkout-specific task state.

**Skills preserve repeatable practice.** A useful procedure can be shared without copying a
folder from laptop to laptop. Git is authoritative for `SKILL.md` content and history;
Substrate stores scope, visibility, approval, and the active version. The source can reside in
the applicable code repository or a separate repository with matching access boundaries;
repository applicability does not require colocated source. Adapters deliver only the
approved versions applicable to the machine. The planned proposal workflow lets agents submit
improvements through a PR and review before a new version is linked.

**Agent definitions preserve scoped roles.** Versioned definitions can describe how a harness
uses approved procedures and context. They carry group access and placement rules alongside
skills and memories. Receiving a definition cannot grant permissions beyond the session's
credentials. Substrate distributes definitions; the coding harness runs the agent.

**Continuity preserves the state of work.** Planned structured checkpoints and typed handoffs
carry the objective, progress, and next steps into a fresh session. This should work across
harnesses. Full transcript offload is a separate, best-effort path tied to supported versions;
it must fall back to a checkpoint when restoration fails.

## The intended learning loop

Imagine diagnosing a deployment failure on a laptop, then continuing the fix with another
harness on a server:

1. The first session receives the applicable instructions, preferences, relevant memories,
   and an index of approved skills.
2. Its observations return with their source and scope. An agent's proposed explanation is
   stored as unverified evidence.
3. Verification and review can establish a reliable lesson. A conflicting observation does
   not silently replace a human-confirmed fact.
4. If the procedure is reusable, a skill proposal can turn the lesson into a reviewed Git
   version. Approval controls when adapters distribute it.
5. A checkpoint lets the next session continue. It receives the relevant lesson and approved
   procedure without reconstructing the investigation from scratch.

This is the destination. The repository currently contains a runnable bootstrap shell;
artifact runtime capabilities remain unimplemented. Memory write/search, context compilation, tool observation capture, and
approved skill linking are proposed foundations. Automated knowledge maintenance, skill
proposals, and portable checkpoints could build on those foundations later.

## Shared authority, selective delivery

Authority is assigned per context space. Identity, scope, and visibility control what applies
and what may be synchronized. The compiler serves a budgeted context pack: effective
instructions and preferences, mandatory items, relevant memories, and a skill index.
Protected sections are never trimmed; skill bodies remain outside the pack.

Work and Personal remain separate even when the same node stores both. Sessions receive
only authorized spaces and group content. Storage placement, replication recipients, and
processing destinations need explicit policies for memories, skills, and agent definitions.
Portable practices require an authorized cross-space sharing mechanism.

A node retains the context needed for local operation and records personal changes and new
observations while disconnected. Shared-policy edits remain proposals until the relevant
shared authority accepts them; disconnected operation uses the last accepted shared policy
subject to valid grants under the agreed [offline policy](../architecture/offline.md). Reconnection checks current access
before exchanging content or accepting submissions.

MCP is an agreed delivery interface; native adapters that render harness files and link
approved skills remain proposed capabilities. Hand edits to rendered shared instructions become reviewable proposals.
Transport, adapter lifecycle, and synchronization contracts remain to be designed.

Memory is rendered as quoted data, separate from directives. That separation, status gating,
and permission checks are parts of a layered defense; they do not make prompt injection a
solved problem.

## What success looks like

- A person can join another device or repository without recreating every harness's setup;
  an administrator can inspect and manage people, devices, repository associations, and access.
- A harness switch retains the applicable rules, working preferences, and relevant knowledge.
- A verified lesson can help a later session on another machine, with its evidence attached.
- An approved skill reaches the machines where it applies at the intended Git version.
- A fresh session can continue from a checkpoint without a repeated project briefing.
- A human can trace why an agent received a fact and review changes to shared knowledge.

Later task leases and GitHub-triggered workers extend this same context foundation to a fleet.
Substrate remains the layer beneath the harness; the existing tracker remains the human work
board. These goals stay within the [non-goals](non-goals.md): self-hosted context infrastructure,
no new IDE or agent loop, and no dependence on private transcript formats for basic continuity.
