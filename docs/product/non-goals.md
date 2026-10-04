# Non-goals

**Last reviewed:** 2026-10-04 · **Re-read cadence:** before any new epic is filed

These boundaries protect the [vision](vision.md). A proposal that needs to cross one must
revisit the product decision explicitly before implementation.

| Non-goal | Rationale |
|---|---|
| A new coding harness, IDE, or general-purpose agent loop | Substrate supplies durable context and artifacts; the existing harness generates code and runs agents. |
| Replacing GitHub or Linear as the human tracker | Knowledge and checkpoints can travel without moving the team's issue workflow into another board. |
| Operating a hosted multi-customer SaaS | Self-hosted personal, team, and organization use is in scope. Hosted customer tenancy and billing are not. |
| Transcript portability as a foundational dependency | Basic continuity must use structured checkpoints. Full restoration is optional and depends on validated client formats and behavior. |
| Full Cursor IDE-session migration | Cursor continuity uses structured checkpoints. Migrating its private IDE session state remains outside scope. |
| Automatic promotion to global applicability | Human review is required before a project lesson becomes a global default. Global applicability grants no additional audience or cross-space access. |
| Implicit widening of artifact access | A copied manifest, narrower instruction, generic-sounding lesson, or agent proposal cannot authorize new recipients. Cross-space publication requires policy permission and human review of the new version. |
| Unrestricted synchronization | Local placement, authorized recipients, processing destinations, and offline access are independent decisions. Membership in Work and Personal does not merge their context. |
| Event-sourced application state | Retained revisions and an audit trail serve the history requirements without making every operation depend on full projection and replay. |
| A general-purpose vector database or knowledge-graph product | Retrieval serves useful, permitted context. Embeddings and alternate stores need measured benefit; model inference is not required for the first release. |
| A guarantee that revocation erases every copy | Disconnected or hostile devices and existing conversations cannot be treated as instantly erasable. Offline authority and deployment boundaries need explicit limits. |
| Prompt injection treated as a solved boundary | Evidence, instructions, approval, and permissions remain distinct. No rendering fence alone establishes a complete security boundary. |
