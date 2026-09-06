# Problem statement

**Last reviewed:** 2026-09-05 · **Re-read cadence:** at each phase exit

## Job story

When I move a piece of work between AI coding harnesses (Claude Code, Codex, Cursor) or
between machines (WSL laptop, Mac Mini, a Kubernetes pod), I want the agent on the far side to
already hold the same instructions, preferences, and hard-won project facts, so I can pick the
right tool and the right hardware for the task instead of the one that happens to remember.

## What people do today instead

| Today | Cost |
|---|---|
| Hand-copy `CLAUDE.md` / `AGENTS.md` / `.cursor/rules` between machines | Silent drift; two machines disagree and neither is authoritative |
| Re-explain the project at the start of each session | Minutes per session, every session, per agent |
| Per-machine memory stores (Memorix and friends) | Knowledge is stranded on whichever laptop learned it |
| Finish the task on the machine you started it on | Can't move a long job to the GPU box; can't hand off to a worker |
| Trust whatever the agent remembers | Agents act confidently on facts the code stopped honoring months ago |

The cost recurs every session and grows multiplicatively with agents × machines × people.

## The inversion

Today the agent is treated as durable and its knowledge as disposable. Substrate flips it:
**agents are disposable, accumulated intelligence is persistent.** The product is one function —

```
(principal + team + project + repo + branch + task + current code) → effective context
```

— served to any harness on any machine, with what the harness learns flowing back under
provenance and trust.

## What must be true for this to be worth building

1. Instructions resolve deterministically and are never dropped by retrieval or budget pressure.
   A rule that is sometimes present is worse than no rule.
2. Memory carries provenance and verification state, so a low-trust agent observation cannot
   quietly overwrite a human-confirmed fact.
3. Local files are a **cache**, not a source. Anything hand-edited is drift, and drift is a
   reviewable event, not a merge.
