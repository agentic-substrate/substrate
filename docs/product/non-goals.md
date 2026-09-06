# Non-goals

**Last reviewed:** 2026-09-05 · **Re-read cadence:** before any new epic is filed

This is the highest-value file in `docs/product/` for an agent: unbounded scope is the default
failure of agent-built features. If a change requires one of these, it is out of scope until
this file changes first.

| Non-goal | Rationale |
|---|---|
| A new coding harness, IDE, or agent loop | Substrate sits *beneath* harnesses and makes them interchangeable. It never generates code or drives a conversation. |
| Replacing GitHub / Linear as the human tracker | The tracker stays the human UI. Substrate mirrors tasks; it does not become the board. |
| Cloud or multi-tenant SaaS | Single org, self-hosted, Tailscale-only. Multi-**team** is in scope; multi-**tenant** is not. |
| Cursor session migration | IDE-bound. Cursor gets Tier-1 (checkpoint) continuity only, forever. |
| Transcript portability as a foundational dependency | Harness storage formats are private and version-gated. Tier 2 is best-effort **by design**; anything that must work depends on Tier 1. |
| Event-sourced state | An append-only audit log answers the history questions. Full event sourcing adds projection and replay for no payoff at this scale. |
| Auto-promotion to `global` scope | Always human review. No exceptions, no thresholds. |
| A general-purpose vector database or knowledge-graph product | Postgres + pgvector until the documented scale ceiling (~50 agents, ~1M memories). |
| Prompt injection treated as a solved boundary | Layered mitigation only. Status gating and the instruction/memory split are the real control; the rendering fence is the weakest layer and is never claimed otherwise. |
