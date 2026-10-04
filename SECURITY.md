# Security policy

**Last reviewed:** 2026-10-04. Re-read before each release or trust-boundary change.

The project is in bootstrap development and has no supported production release. Report
suspected vulnerabilities privately through the repository's GitHub security reporting page
when that facility is enabled. Until then, contact an organization maintainer to arrange a
private reporting channel. Do not publish exploit details or private artifacts in an issue.

Include the affected revision, environment, expected boundary, reproduction steps, and impact.
Use synthetic data where possible. Maintainers will investigate and coordinate disclosure;
there is no guaranteed response deadline at this stage.

The current server is intended for a trusted Linux or WSL user and accepts loopback listeners.
It serves an anonymous status page and a credentialed read-only session context endpoint.
Trusted local CLI setup provides owner, space, checkout, and scoped session records. Local
artifact capture/inspection/retirement authenticate each operation and filter owner, space,
and repository before disclosure. Network enrollment, pairing, publication, and MCP remain
unavailable. SQLite commits revisions, retry receipts, and pending work atomically; failed
commits return no success receipt, and stale edits cannot undo retirement. Git source imports
remain immutable candidates rather than approved or executable content. Plaintext owner-only
state files do not protect against the owner’s OS account, root, or endpoint compromise.
Future artifact authorization must apply to every read, search, list, mutation, delivery, and
publication surface. Repository contents and model-supplied arguments cannot grant authority.

The [transfer and recovery contract](docs/architecture/security.md) describes future trust
boundaries, and the [security roadmap](docs/plans/security.md) records open decisions and
evidence gates. These documents add requirements without claiming that controls are deployed.
Content, management, enrollment, recovery, and analytics are distinct authorities. Future
enterprise recovery must enforce assigned scope independently and protect its audit record.
A hub, valid credential, or imported instruction cannot silently widen decryption or access
authority. Before customer/internal network transfer, decide custody, recoverable scope,
offline enforcement, deletion, and telemetry, then verify hostile-peer/hub and restore behavior.
Endpoint/root compromise and retained plaintext limit revocation and content-blindness claims.

Assess reachable disclosure, unauthorized mutation, policy/revision rollback, unsafe import,
or recovery-scope bypass against the implemented surface and documented boundary. The future
design is review context, not proof that an absent feature is already vulnerable or secure.

## What is not a vulnerability by itself

A feature listed as future work, a lexical search result with poor relevance, and a missing
platform adapter are product gaps unless they violate an implemented security contract.
Process owners reading their own files or already delivered plaintext does not violate a
revocation guarantee. Claims about hostile owners, filesystem administrators, or deleting
information from existing conversations require an explicit threat boundary. A reachable
exposure or authorization bypass is still worth reporting, even in an experimental feature.

## Dependencies and releases

Review dependency changes and prefer a small dependency surface. CI reviews dependencies
introduced by pull requests and checks reachable Go vulnerabilities. GitHub
security alerts track repository-wide advisories; unrelated changes should not bypass a
known finding silently. Avoid credentials in fixtures and keep security checks actionable.

CodeQL scanning is disabled to conserve the current usage budget, and its automatic workflow
has been removed. Revisit it when maintainers allocate a scanning budget or before the first
supported production release. Dependency checks, secret scanning and push protection, private
vulnerability reporting, and branch and release-tag protections remain part of the baseline.

Never move or reuse a published release tag. Publish a new version for a correction, identify
the affected versions, and retain the original provenance. Release tags and branch rules are
configured separately on GitHub and require verification after publication.
