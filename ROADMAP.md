# Roadmap

**Last reviewed:** 2026-09-08

The destination is a durable context layer for instructions, preferences, memories, skills,
and task continuity. A lesson learned in one session should improve a later session on another
machine; a reviewed procedure should become an available skill; a checkpoint should carry work
across harnesses. The [vision](docs/product/vision.md) describes that lifecycle.

These are **confidence horizons, not release dates**. Now identifies the current focus, not a
claim that everything listed is complete. Execution state lives on the [project board]; the
[README](README.md#available-now-and-ahead) summarizes implemented capabilities.

## Now — at most three

1. **A memory written on one machine is searchable from another within a minute, with no file
   copied by hand.** Schema, scope chain, `memory.write`/`search`/`supersede`, keyword retrieval.
2. **Both machines generate identical instruction files from one authoritative source, and a
   hand edit becomes a review item rather than a merge.** Instruction/preference resolution,
   deterministic rendering, `substrate-adapter` render + drift loops. Approved skill versions
   also reach the machines where their scope and visibility apply (SKILL-1, SKILL-2).
3. **The drifted state of the existing machines is reconciled once, with conflicts shown rather
   than resolved by "last sync wins."** `substrate import` scan → plan → apply → cutover.

## Next

- A cold session on a different machine continues a task from its checkpoint without
  re-explanation, across harness types.
- An agent can ask *why* something is in its context — and why something else is not.
- Memory that describes code gets flagged when that code changes.
- Feedback from agents is recorded and weighted by trust, and never silently changes status.

## Later

- Raw session offload: stop on the laptop, resume the actual conversation in a pod.
- Observations become useful long-term knowledge through evidence-based promotion,
  consolidation, and a human review queue with real throughput.
- Reusable lessons become skill proposals that open a PR and link nothing until approved.
  Teams can inherit org skills and pin approved versions.
- Tasks, leases, and GitHub-triggered workers.
- A second team, OIDC, RLS leak tests, and a dashboard.

## Not doing

See [`docs/product/non-goals.md`](docs/product/non-goals.md) for the reasoning: not a
harness, not a tracker, not SaaS, not event-sourced, no auto-promotion to global scope, and no
claim that prompt injection is solved.

[project board]: https://github.com/orgs/agentic-substrate/projects/1
