# Roadmap

**Last reviewed:** 2026-10-03. Re-read before accepting a foundational epic or changing a horizon.

Horizons express confidence in the outcome, not delivery dates. Execution state belongs in the
GitHub Project once created. The [first release contract](docs/product/first-release.md)
defines the evidence for the three Now outcomes.

## Now

- [Feature #2](https://github.com/agentic-substrate/substrate/issues/2): Reuse permitted memories, skills, and agent definitions across multiple harness sessions on
  one Linux or WSL node through a CLI and MCP, with explicit repository bindings and versions.
- [Feature #3](https://github.com/agentic-substrate/substrate/issues/3): Keep Work and Personal boundaries understandable and enforceable across every artifact
  operation, including exact-content human review for permitted cross-space publication.
- [Feature #4](https://github.com/agentic-substrate/substrate/issues/4): Trust acknowledged saves, restart, backup, and restore, while observing and controlling
  incremental indexing and bounded background work.

## Next

- Keep independently usable nodes in multiple WSL instances selectively synchronized while
  they join, disconnect, and reconcile preserved revisions under a single coordinator.
- Extend the same contracts to multiple laptops and servers with secure enrollment, finite
  offline grants, and explicit lifecycle and recovery evidence.

## Later

- Administer team and organization spaces, membership, policy, and auditing without granting
  other projects access to restricted artifacts.
- Add native macOS and Windows support when lifecycle and packaging can be tested there.
- Improve retrieval only when evaluated queries justify the added resource cost. Optional
  embeddings, alternate stores, and coordinator availability need measured revisit triggers.

## Not doing

- Replacing coding harnesses or managing general-purpose agent execution.
- Sharing live SQLite files across systems, committing captured memory databases to Git, or
  allowing repository manifests to grant access by themselves.
- Requiring model inference, Tailscale, PostgreSQL, or pgvector to use the first release.
- Promising erasure from hostile devices, filesystem administrators, or existing conversations.
