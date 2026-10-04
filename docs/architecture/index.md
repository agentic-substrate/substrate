# Architecture decisions

**Last reviewed:** 2026-10-04. Re-read before changing an accepted boundary or filing its implementation.

These decisions describe the agreed direction. Trusted local bindings and scoped artifact
persistence are implemented; broader policy, retrieval, MCP, and synchronization remain future
work. The browser exposes status/context inspection only. Implementation acceptance lives in
the [first release contract](../product/first-release.md).

The [vision](../product/vision.md) explains the purpose and intended experience. These pages
record how selected boundaries support it and where implementation choices remain open.

- [Deployment](deployment.md) starts locally and advances to selectively synchronized nodes.
- [Delivery](delivery.md) defines the CLI, MCP, and embedded administrative interface.
- [Packaging](packaging.md) limits initial platform support to Linux and WSL 2.
- [Artifacts](artifacts.md) defines eligibility, precedence, and effective versions.
- [Sharing](sharing.md) separates placement, audience, and reviewed publication.
- [Repositories](repositories.md) binds Git identities and checkouts to trusted spaces.
- [Onboarding](onboarding.md) explains enrollment and administrative authority.
- [Offline behavior](offline.md) sets the boundaries for disconnected use and grants.
- [Reconciliation](reconciliation.md) preserves conflicting candidates without silent overwrite.
- [Transfer and recovery](security.md) records hub authority, scoped enterprise recovery,
  proposed key custody, telemetry limits, and evidence required before customer-data transfer.
- [Retrieval](retrieval.md) combines text, context, and explicit associations.
- [Embeddings](embeddings.md) keeps model inference optional and subject to evaluation.

The [security roadmap](../plans/security.md) pairs those boundaries with unresolved decisions,
release gates, retained security tests, and factual enterprise assurance requirements.
