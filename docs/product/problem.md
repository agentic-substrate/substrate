# Problem: project knowledge is scattered across tools

**Last reviewed:** 2026-10-03 · **Re-read cadence:** at each phase exit

## The job

When I move work between coding harnesses or machines, I want the next session to receive
the applicable instructions, working preferences, relevant facts, and approved procedures,
so I can continue without repeating the project briefing. Eventually, a checkpoint should
carry the state of unfinished work. The choice of tool or hardware should not depend on
which one remembers the project.

When I add a person, device, or repository to that context, I want joining and administration
to be straightforward, so maintaining the system does not become another source of repeated
configuration work. Ease of joining and administration is a major product goal for both a
person managing their own machines and a team or organization administrator.

## Where context gets lost

| What happens today | What the next session loses |
|---|---|
| Rules are copied between `AGENTS.md`, `CLAUDE.md`, and Cursor rules | A clear answer about which version is authoritative |
| Preferences are repeated in each harness | Consistent working choices across tools |
| Decisions and lessons stay in chats or local memory stores | The reasoning and evidence behind earlier work |
| Skill folders are copied or updated independently | A known, approved version of a reusable procedure |
| A task stays inside one conversation | Progress and next steps when work moves elsewhere |
| Old memories remain available after the code changes | A reliable distinction between current facts and stale claims |
| Each machine, checkout, and harness needs separate setup | Predictable onboarding and an understandable view of who receives which context |

The cost repeats across sessions and grows with harnesses, machines, and people. File syncing
addresses only part of it: matching bytes cannot establish whether a memory is verified or a
skill is approved for a particular team.

## The product response

Substrate is intended to make context durable beneath interchangeable harnesses. The proposed
design resolves applicable instructions and preferences, retrieves relevant memories, and
distributes approved skills from shared authority. Observations would return with scope and
provenance. The long-term lifecycle adds verification, knowledge maintenance, skill proposals,
and portable task continuity. The architecture remains under discussion.

## What must hold

1. Required instructions resolve deterministically and survive context-budget pressure.
2. Memories carry evidence and status; agent observations cannot silently overwrite confirmed facts.
3. Skill content stays in Git, with approval and active-version state in Substrate.
4. Rendered shared harness files are caches; drift becomes a reviewable proposal. Authored
   repository source files remain distinct and are not silently overwritten.
5. Context is permissioned. Sharing authority does not imply sharing every private memory.
6. Planned continuity works from structured checkpoints even when transcript migration fails.
7. User joins and administration are central product outcomes. Portable repository discovery,
   reusable approved bindings, and inspectable access should reduce routine setup work.
8. Artifacts have clear precedence, search eligibility, segmentation, and approval behavior.
   An organization's prohibition on outside-project access cannot be relaxed by a repository
   instruction, artifact specialization, or personal publication approval.
9. Retrieval respects laptop battery and interactive work. Neural inference is not presumed
   mandatory; expensive enrichment needs resource limits and deliberate scheduling.

See the [vision](vision.md) for the full lifecycle and the [getting started guide](../getting-started.md) for what
is implemented today.
