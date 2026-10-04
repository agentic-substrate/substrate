# Non-goals

**Last reviewed:** 2026-10-03 · **Re-read cadence:** before any new epic is filed

This is the highest-value file in `docs/product/` for an agent: unbounded scope is the default
failure of agent-built features. If a change requires one of these, it is out of scope until
this file changes first.

| Non-goal | Rationale |
|---|---|
| A new coding harness, IDE, or agent loop | Substrate sits *beneath* harnesses and makes them interchangeable. It never generates code or drives a conversation. |
| Replacing GitHub / Linear as the human tracker | The tracker stays the human UI. Substrate mirrors tasks; it does not become the board. |
| Operating a hosted multi-customer SaaS | Substrate is self-hosted for personal, team, and organization use. Nodes may participate in separate Work and Personal context spaces; hosted customer tenancy and billing are outside scope. |
| Cursor session migration | IDE-bound. Cursor gets Tier-1 (checkpoint) continuity only, forever. |
| Transcript portability as a foundational dependency | Harness storage formats are private and version-gated. Tier 2 is best-effort **by design**; anything that must work depends on Tier 1. |
| Event-sourced state | An append-only audit log answers the history questions. Full event sourcing adds projection and replay for no payoff at this scale. |
| Auto-promotion to `global` scope | Always human review. No exceptions, no thresholds. |
| A general-purpose vector database or knowledge-graph product | Substrate focuses on durable agent context. Initial storage uses SQLite and retrieval uses ranked text plus explicit semantic associations. Neural embeddings are an optional candidate; scoring/index details and supported scale remain open. |
| Prompt injection treated as a solved boundary | Layered mitigation only. Status gating and the instruction/memory split are the real control; the rendering fence is the weakest layer and is never claimed otherwise. |
