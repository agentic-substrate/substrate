# Problem: project knowledge is scattered across tools

**Last reviewed:** 2026-09-08 · **Re-read cadence:** at each phase exit

## The job

When work moves between coding harnesses or machines, the next session should receive the
applicable instructions, working preferences, relevant facts, and approved procedures. It
should eventually be able to continue from a checkpoint. The choice of tool or hardware should
not depend on which one remembers the project.

## Where context gets lost

| What happens today | What the next session loses |
|---|---|
| Rules are copied between `AGENTS.md`, `CLAUDE.md`, and Cursor rules | A clear answer about which version is authoritative |
| Preferences are repeated in each harness | Consistent working choices across tools |
| Decisions and lessons stay in chats or local memory stores | The reasoning and evidence behind earlier work |
| Skill folders are copied or updated independently | A known, approved version of a reusable procedure |
| A task stays inside one conversation | Progress and next steps when work moves elsewhere |
| Old memories remain available after the code changes | A reliable distinction between current facts and stale claims |

The cost repeats across sessions and grows with harnesses, machines, and people. File syncing
addresses only part of it: matching bytes cannot establish whether a memory is verified or a
skill is approved for a particular team.

## The product response

Substrate makes context durable beneath interchangeable harnesses. It resolves applicable
instructions and preferences, retrieves relevant memories, and distributes approved skills
from shared authority. Observations return with scope and provenance. The long-term lifecycle
adds verification, knowledge maintenance, skill proposals, and portable task continuity.

## What must hold

1. Required instructions resolve deterministically and survive context-budget pressure.
2. Memories carry evidence and status; agent observations cannot silently overwrite confirmed facts.
3. Skill content stays in Git, with approval and active-version state in Substrate.
4. Local harness files are caches; drift becomes a reviewable proposal.
5. Context is permissioned. Sharing authority does not imply sharing every private memory.
6. Planned continuity works from structured checkpoints even when transcript migration fails.

See the [vision](vision.md) for the full lifecycle and the [README](../../README.md) for what
is implemented today.
