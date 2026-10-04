# Security policy

**Last reviewed:** 2026-10-03. Re-read before each release or trust-boundary change.

The project is in bootstrap development and has no supported production release. Report
suspected vulnerabilities privately through the repository's GitHub security reporting page
when that facility is enabled. Until then, contact an organization maintainer to arrange a
private reporting channel. Do not publish exploit details or private artifacts in an issue.

Include the affected revision, environment, expected boundary, reproduction steps, and impact.
Use synthetic data where possible. Maintainers will investigate and coordinate disclosure;
there is no guaranteed response deadline at this stage.

The current server is intended for a trusted Linux or WSL user and accepts loopback listeners.
It serves a status page and has no artifact storage, authentication, enrollment, or MCP tools.
Future artifact authorization must apply to every read, search, list, mutation, delivery, and
publication surface. Repository contents and model-supplied arguments cannot grant authority.

## What is not a vulnerability by itself

A feature listed as future work, a lexical search result with poor relevance, and a missing
platform adapter are product gaps unless they violate an implemented security contract.
Process owners reading their own files or already delivered plaintext does not violate a
revocation guarantee. Claims about hostile owners, filesystem administrators, or deleting
information from existing conversations require an explicit threat boundary. A reachable
exposure or authorization bypass is still worth reporting, even in an experimental feature.

## Dependencies and releases

Review dependency changes and prefer a small dependency surface. CI reviews dependencies
introduced by pull requests, checks reachable Go vulnerabilities, and runs CodeQL. GitHub
security alerts track repository-wide advisories; unrelated changes should not bypass a
known finding silently. Avoid credentials in fixtures and keep security checks actionable.

Never move or reuse a published release tag. Publish a new version for a correction, identify
the affected versions, and retain the original provenance. Release tags and branch rules are
configured separately on GitHub and require verification after publication.
