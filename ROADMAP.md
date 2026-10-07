# Roadmap

**Last reviewed:** 2026-10-07. Re-read before accepting a foundational epic or changing a horizon.

The [vision](docs/product/vision.md) is durable context that compounds across sessions:
permitted knowledge and reviewed practice follow the work, with understandable boundaries
and eventual task continuity. Horizons express confidence in the outcome, not delivery dates.
Execution state belongs in the [GitHub Project](https://github.com/orgs/agentic-substrate/projects/2).
The [first release contract](docs/product/first-release.md) defines evidence for the three first-release outcomes.

## Done

The three first-release outcomes are implemented and verified on one Linux or WSL 2 node.
The [development guide](docs/development.md#actual-client-verification) records the tested
client versions and the synthetic workload; native activation, remote MCP, other platforms,
and real-data retrieval measurements remain outside that evidence.

- [Feature #2](https://github.com/agentic-substrate/substrate/issues/2): Reuse permitted memories, skills, and agent definitions across multiple harness sessions on
  one Linux or WSL node through a CLI and MCP, with explicit repository bindings and versions.
- [Feature #3](https://github.com/agentic-substrate/substrate/issues/3): Keep Work and Personal boundaries understandable and enforceable across every artifact
  operation, including exact-content human review for permitted cross-space publication.
- [Feature #4](https://github.com/agentic-substrate/substrate/issues/4): Trust acknowledged saves, restart, backup, and restore, while observing and controlling
  incremental indexing and bounded background work.

## Now

- [Feature #25](https://github.com/agentic-substrate/substrate/issues/25): Decide the transfer gate (security plan Gate C): key custody, recovery scope, offline
  enforcement, enrollment, deletion, and telemetry. Synchronization implementation waits on it.
- [Task #33](https://github.com/agentic-substrate/substrate/issues/33): Measure lexical retrieval and node energy use on representative real
  permitted data.
- [Task #34](https://github.com/agentic-substrate/substrate/issues/34): Manually verify screen-reader and 400% zoom behavior for the browser page.

## Next

- [Feature #32](https://github.com/agentic-substrate/substrate/issues/32): Keep independently usable nodes in multiple WSL instances selectively synchronized while
  they join, disconnect, and reconcile preserved revisions under a single coordinator.
- Extend the same contracts to multiple laptops and servers with secure enrollment, finite
  offline grants, and explicit lifecycle and recovery evidence.

Before network transfer carries customer or internal data, meet the
[security gates](docs/plans/security.md#capability-gates): decide key custody, recovery scope,
offline enforcement, deletion, and telemetry, then verify hostile peers/hubs and scoped recovery.
Future peer-to-peer and hub-and-spoke paths enforce the same transfer policy; initial HTTPS
synchronization with one coordinator remains the selected first distribution step.

## Later

- Keep knowledge useful as projects change through verification, explicit contradiction and
  staleness handling, and reviewed improvements to reusable procedures.
- Resume unfinished work across harnesses and machines through structured checkpoints and
  handoffs. Transcript restoration remains an optional, validated convenience.
- Deliver effective instructions, preferences, and approved native skills and agent definitions
  through adapters that preserve the session's permissions and selected versions.
- Administer team and organization spaces, membership, policy, and auditing without granting
  other projects access to restricted artifacts.
- Provide optional authoritative hub management and approved metrics, with enterprise content
  recovery limited to assigned scope. Management and analytics confer no implicit decryption
  authority; custody and independently enforced recovery remain design decisions.
- Add native macOS and Windows support when lifecycle and packaging can be tested there.
- Improve retrieval only when evaluated queries justify the added resource cost. Optional
  embeddings, alternate stores, and coordinator availability need measured revisit triggers.

## Not doing

- Replacing coding harnesses or managing general-purpose agent execution.
- Sharing live SQLite files across systems, committing captured memory databases to Git, or
  allowing repository manifests to grant access by themselves.
- Requiring model inference, Tailscale, PostgreSQL, or pgvector to use the first release.
- Promising erasure from hostile devices, filesystem administrators, or existing conversations.
