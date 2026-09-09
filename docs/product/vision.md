# Vision: context that compounds across sessions

Substrate's long-term goal is one authoritative, self-hosted context layer beneath any coding
harness on any machine. Instructions, preferences, memories, skills, and task continuity should
survive the session that used or discovered them. Choosing another tool should not mean losing
what the project has learned.

File rendering is one delivery mechanism. The broader product is a lifecycle: deliver the
right context, capture useful evidence from work, review and verify changes, and make the
result available to the next session.

This page explains the intended system. The [README](../../README.md#available-now-and-ahead)
identifies current capabilities; the [roadmap](../../ROADMAP.md) groups future outcomes.
The reviewed [PRD](../design/prd.md) and [EDD](../design/edd.md) remain the requirements and
contract records.

## Five kinds of durable context

**Instructions establish constraints.** Resolve the most specific active instruction for each
key along `global → org → team → project → repo → branch → task → session`. Instructions
remain deterministic and are never scored or removed to make room for memories (INST, CTX-2).

**Preferences preserve working choices.** A person's preferences can follow them between
projects and machines. The `user > team > org` overlay is separate from instruction scope;
a personal preference cannot override a project instruction (INST-3).

**Memories preserve knowledge and its evidence.** Facts, decisions, incidents, lessons, and
observations carry scope, visibility, source, verification, and status. Agents write
`unverified`. Superseding an item preserves its history. Planned feedback, contradiction
handling, code staleness checks, and consolidation help maintain useful knowledge as the
project changes; feedback alone never changes status (MEM-1 through MEM-10).

**Skills preserve repeatable practice.** A useful procedure can be shared without copying a
folder from laptop to laptop. Git is authoritative for `SKILL.md` content and history;
Substrate stores scope, visibility, approval, and the active version. Adapters link only the
approved versions applicable to the machine. The planned proposal workflow lets agents submit
improvements through a PR and review before a new version is linked (SKILL-1 through SKILL-4).

**Continuity preserves the state of work.** Planned structured checkpoints and typed handoffs
carry the objective, progress, and next steps into a fresh session. This should work across
harnesses. Full transcript offload is a separate, best-effort path tied to supported versions;
it must fall back to a checkpoint when restoration fails (CONT, TASK-3).

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

This is the destination, not a claim that the entire loop ships today. Memory write/search,
context compilation, tool observation capture, and approved skill linking establish the
foundation. Automated knowledge maintenance, skill proposals, and portable checkpoints are
future work.

## Shared authority, selective delivery

One source of truth does not mean every agent sees everything. Identity, scope, and visibility
control what applies. The compiler serves a budgeted context pack: effective instructions and
preferences, mandatory items, relevant memories, and a skill index. Protected sections are
never trimmed; skill bodies remain outside the pack (SCOPE, CTX-1, CTX-2).

The server exposes context through MCP. The per-machine adapter renders local harness files,
links approved skills, keeps a memory cache, and drains an outbox after reconnecting. Local
files are caches of server authority; a hand edit becomes a reviewable proposal rather than
silently replacing shared instructions (SYNC).

Memory is rendered as quoted data, separate from directives. That separation, status gating,
and permission checks are parts of a layered defense; they do not make prompt injection a
solved problem.

## What success looks like

- A harness switch retains the applicable rules, working preferences, and relevant knowledge.
- A verified lesson can help a later session on another machine, with its evidence attached.
- An approved skill reaches the machines where it applies at the intended Git version.
- A fresh session can continue from a checkpoint without a repeated project briefing.
- A human can trace why an agent received a fact and review changes to shared knowledge.

Later task leases and GitHub-triggered workers extend this same context foundation to a fleet.
Substrate remains the layer beneath the harness; the existing tracker remains the human work
board. These goals stay within the [non-goals](non-goals.md): one self-hosted organization,
no new IDE or agent loop, and no dependence on private transcript formats for basic continuity.
