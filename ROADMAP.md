# Roadmap

**Last reviewed:** 2026-09-05

These are **confidence horizons, not time buckets**. An item moves left when we are more sure
it is the right thing, not when a date arrives. This file holds no execution state — no
percentages, no checkboxes, no owners. State lives on the [project board]; a file that stores
no state cannot go stale.

Items are phrased as outcomes. If you cannot tell whether an item is done by using the system,
it is phrased wrong.

## Now — at most three

1. **A memory written on one machine is searchable from another within a minute, with no file
   copied by hand.** Schema, scope chain, `memory.write`/`search`/`supersede`, keyword retrieval.
2. **Both machines generate identical instruction files from one authoritative source, and a
   hand edit becomes a review item rather than a merge.** Instruction/preference resolution,
   deterministic rendering, `substrate-adapter` render + drift loops.
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
- Promotion, consolidation, and a human review queue with real throughput.
- Skill proposals that open a PR and link nothing until approved.
- Tasks, leases, and GitHub-triggered workers.
- A second team, OIDC, RLS leak tests, and a dashboard.

## Not doing

See [`docs/product/non-goals.md`](docs/product/non-goals.md) for the reasoning. In short: not a
harness, not a tracker, not SaaS, not event-sourced, no auto-promotion to global scope, and no
claim that prompt injection is solved.

[project board]: https://github.com/users/jacorbello/projects
