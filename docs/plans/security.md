# Security roadmap and evidence

**Last reviewed:** 2026-10-04. Re-read at each release, trust-boundary change, enterprise
commitment, or material change to a referenced standard.

**Status:** codified Substrate guidance from *Security frameworks and enterprise roadmap for
PlotLens Parley and Substrate*, dated 4 October 2026 (source filename:
`plotlens-parley-substrate-security-roadmap.md`).
Product direction is confirmed; recommendations, unresolved decisions, and future controls
are labeled below. This is not implementation evidence, certification, an audit result, or
authorization to purchase assurance or publish customer information. PlotLens/Parley-specific
controls and commercial facts are outside this repository's scope.

The [transfer/recovery contract](../architecture/security.md) records the threat boundaries.
The [first release](../product/first-release.md) remains one owner on one Linux/WSL node.
Execution tracking belongs in the GitHub Project; gates below are capabilities, not dates.

## Framework baseline

Use one small evidence register with a Substrate profile instead of duplicating programs.
The following are planning baselines, not achieved conformance:

| Reference | Substrate use and limit |
|---|---|
| [NIST CSF 2.0](https://www.nist.gov/cyberframework) | Organize risk, ownership, current/target profiles, incidents, and recovery. It is not certification. |
| [NIST SSDF 1.1](https://csrc.nist.gov/pubs/sp/800/218/final) | Secure development, review, build/release, and vulnerability-response evidence. Reassess later revisions when adopted. |
| [SLSA 1.2](https://slsa.dev/spec/v1.2/) | Plan verifiable distributed releases; consider Build L2 provenance before Build L3 hardening. Claim a track/level only after its requirements are verified. |
| [OWASP ASVS 5.0.0](https://github.com/OWASP/ASVS/tree/v5.0.0) | Map applicable admin UI/API requirements with exact versioned IDs; supplement for CLI, MCP, local files, and transfer. Selected tests do not establish full Level 2. |
| [NIST SP 800-207](https://csrc.nist.gov/pubs/sp/800/207/final) | Separate principals, resources, policy decisions, and enforcement before network transfer. Network location grants no implicit authority. |
| [OWASP LLM risks](https://genai.owasp.org/llm-top-10/) and [NIST AI RMF](https://www.nist.gov/itl/ai-risk-management-framework) | Supplement malicious-context and downstream-harness evaluation. Pin editions when assessing; evaluations cannot replace deterministic permissions. |

These sources are reference baselines from the supplied roadmap, not a promise to adopt every
control. Verify publisher status and pin applicable requirements when implementation or an
assessment starts. SOC 2 or ISO/IEC 27001 is conditional on an actual buyer/risk and a defined
operated service or organizational boundary. Open-source distribution and customer-operated
deployments do not inherit a vendor or supplier's assurance report.

## Decision register

Maintainers must assign an accountable owner and review trigger when scheduling unresolved
work. No owner acceptance, deadline, numeric lease, or recovery objective is inferred here.
Roadmap D identifiers preserve traceability to the supplied document.

| Roadmap ID | Status | Substrate decision and next evidence |
|---|---|---|
| D1 | Recommended baseline | Maintain a shared CSF/SSDF/SLSA program with a product profile and evidence references. |
| D3 | Confirmed goal | Prepare for enterprise procurement; qualify actual buyer needs before commissioning assurance. |
| D5 | Confirmed direction | Relay permitted information first; do not become a general agent execution engine. |
| D6 | Confirmed direction | Future peer-to-peer and hub-and-spoke transfer of customer/internal data; preserve the initial HTTPS/coordinator sequence. |
| D7 | Confirmed direction; limits open | Optional authoritative hub management and metrics; decide authority over enrollment, policy, and decryption recipients. |
| D8 | Confirmed requirement; mechanism open | Enterprise recovery only within assigned scope; independently test authorization and custody. |
| D9 | Proposed architecture | Separate relay/control/recovery/audit; review common host, IAM, deployment, and browser-code authority. |
| D10 | Open; decide first | Choose key custody, effective decryptors, key-release authority, rotation, loss, and emergency recovery. Customer-controlled custody is a candidate. |
| D11 | Open; decide first | Select recoverable tenant/project/collection granularity and Personal versus enterprise ownership. Existing artifact scopes do not select cryptographic recovery scope. |
| D12 | Partly agreed; enforcement open | Finite shared offline grants and online-only sensitive exceptions are selected; choose duration, clock trust, caching, and runtime enforcement. No compromised-device erasure promise. |
| D13 | Open mechanism | Identity-bound device enrollment/removal/re-enrollment; existing local pairing/invitation/OIDC sequence remains selected. |
| D14 | Open mechanism | Authenticated tombstones/checkpoints, compaction, stale-node rebootstrap, backup expiry, and legal retention if applicable. |
| D15 | Open | Approve exact telemetry fields, purposes, recipients, permissions, retention, region, and exports before collection. |
| D16 | Open | Select actual MCP transports/revisions/SDKs and validate advertised client versions; stdio and HTTP have distinct security requirements. |
| D17 | Open | Define permitted/prohibited data classes and scope regulated uses/contracts before accepting obligations. |
| D18 | Open | Establish measured restore/incident objectives and sustainable support commitments. |

Settle D10–D12 before building network synchronization around recovery/privacy/offline claims.
Record selections in the architecture contract, including abuse cases and residual limits.
D2 and D4 are PlotLens/Parley-specific and are not adopted as Substrate requirements.

## Control and claims evidence

For each control record scope, accountable owner, status, code/configuration reference,
verification method, evidence date/revision/environment, exception and expiry if any, and next
review trigger. Link framework mappings and exact ASVS IDs where relevant. Use four statuses:
**planned**, **implemented but unverified**, **verified with evidence**, and **exception accepted
by an accountable owner**. Sensitive evidence stays private; public references use synthetic
or redacted data. Accepted exceptions need an owner and compensating check.

Source/tests can identify implementation but do not supply a dated passing result. This seed
inventory distinguishes inspected implementation from recorded local verification and future
operational evidence:

| Control | Status at this documentation change | Implementation or next evidence |
|---|---|---|
| Bootstrap numeric loopback restriction and API/MCP errors | Verified with evidence on local WSL 2 | `cmd/substrate/lifecycle.go`, `internal/server/server.go`, their tests, and the check recorded below; other environments remain unverified. |
| Locked dependencies, pinned CI Actions, restricted CI credentials, dependency review/govulncheck | Implemented but unverified by this change | Lockfiles and `.github/workflows/ci.yml`; hosted runs and settings readback establish operational evidence. |
| Branch/tag protection, private reporting, secret scanning/push protection | Planned verification of external settings | Read back actual settings; repository prose cannot prove they are enabled. |
| Local owner, space/checkout/session binding, bearer context Origin/Host checks, private state persistence | Verified with local evidence dated 2026-10-04 | `internal/authority`, `internal/server/context.go`, CLI and packaged browser tests; plaintext owner-only state is selected, with no same-user/root protection. Re-run after authority/interface changes. |
| Local repository-scoped artifact persistence, immutable Git candidates, current session checks | Verified with local evidence dated 2026-10-04; project maintainers own review | `internal/artifacts`, CLI tests and packaged command journey; permission filtering, retries, stale revisions, commit failure, and retirement verified below. Re-run after storage, source, or authority changes. |
| Broader artifact action policy, remote administration/MCP authorization, shared imports/publication | Planned | Full first-release boundaries, actual caller attribution, and transport/client-specific checks; local storage evidence does not verify these capabilities. |
| Scoped recovery, transfer crypto, stale-state handling, telemetry | Planned; decisions pending | D10–D16 and adversarial evidence from the architecture contract. |
| Release manifest, SBOM/provenance, consumer verification, restore/incident exercises | Planned | Verify delivered artifacts and actual restore results before making claims. |

On 2026-10-04, `env PATH="/usr/local/go/bin:$PATH" make check` passed on Linux x86_64 under
WSL 2 with Go 1.27.1, Node.js 24.15.0, and npm 11.12.1. Runtime code was revision `84b93fa`
with documentation/instruction changes only. Its lifecycle, routing, race, packaged smoke,
and browser checks support the bootstrap row; they do not verify future artifact/MCP/transfer
controls or hosted repository settings. Re-run relevant checks after a boundary change.

On 2026-10-04, the issue #5 worktree passed `env PATH="/usr/local/go/bin:$PATH"
GOCACHE=/tmp/substrate-go-cache TMPDIR=/var/tmp make check` on Linux x86_64 under WSL 2
with Go 1.27.1,
Node.js 24.15.0, and npm 11.12.1. Tests used synthetic `~/repos` equivalents and verified
manifest nonauthority, explicit worktree/nested binding, scope denial, Git identity replacement,
expiry/revocation, concurrent grant persistence, failed/oversized state updates, malformed
Git metadata and missing-Git placement denial, whitespace-preserving paths, browser
Origin/Host checks, and a real packaged-browser credential/revocation journey. Playwright
checked keyboard focus, live status semantics, axe states, and 320-pixel reflow. No actual
screen-reader application, other operating system, artifact policy, MCP client, transfer, or
hosted security setting was verified. The issue #5 PR identifies the final source revision
and commands; re-run at the next local trust-boundary change. Final synthetic fixtures used
`/var/tmp` because a pre-existing `/tmp/.git` marker made `/tmp` unavailable for private
authority and credential placement; the check did not exempt or remove that marker.

On 2026-10-04, the issue #6 worktree passed `env PATH="/usr/local/go/bin:$PATH"
GOCACHE=/tmp/substrate-go-cache TMPDIR=/var/tmp make check` on Linux x86_64 under WSL 2
with Go 1.27.1, Node.js 24.15.0, npm 11.12.1, and SQLite 3.53.4 through the pinned driver.
Synthetic tests verified acknowledged reopen, whole-transaction and commit-failure rollback,
expected-base candidates, immutable retry receipts, changed-payload rejection, retirement,
Work/Personal and same-space repository filtering, revoked-session reads/writes/queue access,
private database/journal modes, unknown-schema rejection, eight concurrent retries across two
connections, FTS5 initialization, and raw Git commit/blob identity despite replacement refs.
The same-space read check also failed when its repository predicate was deliberately removed,
then passed after restoration. A compiled CLI subprocess journey verified capture, subsequent
process inspection/retry, changed retry denial, pending work, retirement, and revoked access.
Existing packaged-browser checks covered axe error/context states, keyboard focus, and
320-pixel reflow after a text-only capability correction. The pinned CI govulncheck command
reported no reachable vulnerabilities. The issue #6 PR records the final source revision and
exact commands. This does not verify device/power failure, backups/restores, replica recovery,
normal retrieval, MCP clients, shared policy, publication, performance, an actual screen reader,
or other operating systems. Clean `TMPDIR` avoids this host's unrelated `/tmp/.git` marker;
private-placement enforcement has no test exemption. Re-run at the next local boundary change.

The issue #6 review corrections were verified on 2026-10-04 in the same toolchain/environment,
with Git 2.43.0. Invalid UTF-8 previously received receipts and could alias changed payloads;
new text validation rejects every contribution/lookup/retirement string and raw Git text before
fingerprinting, while preserving valid U+FFFD. Live and hot journal tests initially observed
0644 files; the supported encoded `modeof` URI now yields 0600 journals under a child process's
022 umask. An independent opener waits for a live transaction, and abrupt-process hot-journal
rollback preserves an earlier acknowledged revision while discarding uncommitted writes.
A missing promisor blob previously executed a synthetic repository uploadpack helper. The
empty `GIT_ALLOW_PROTOCOL` environment allowlist now overrides even permissive repository
and caller settings: uploadpack and external-helper markers remain absent, and a synthetic
loopback HTTP receiver records zero requests. Final check commands and the repaired source
revision are recorded in PR #18. These controls do not claim arbitrary Git plugin isolation,
device/power-failure recovery, backup completeness, or network synchronization readiness.

CodeQL remains disabled under the documented budget decision. This roadmap does not enable
scanners or replace remaining checks. Revisit that decision under its existing release/budget
trigger. Require strong authentication for source/release authority, keep access least privileged
and recoverable, and prevent one compromised contributor account from unilaterally replacing
release artifacts. Review boundary-changing changes, isolate untrusted pull-request jobs from
release secrets/data, and prefer short-lived
credentials. Scan secrets and actionable dependency risks; triage by reachability/impact and
track time-bound exceptions. Never put production data or credentials in fixtures or memory.

Before distribution, a release manifest should link source commit, dependency/SBOM output,
provenance, artifact digest, and consumer verification instructions. Test modified artifacts,
wrong builder/signer/source, and unexpected build policy. A signature or SBOM alone proves
neither a SLSA level nor absence of vulnerabilities. Define supported versions, private reporting,
advisories, patch delivery, safe upgrades/rollback, and tested restoration with required keys.
Preserve deletion and current membership on restore; a backup setting is not recovery evidence.
An incident playbook needs containment, credential/key revocation, protected evidence,
operator/customer communication, and escalation. Measure objectives before promising them.

The operational inventory includes stores, indexes/embeddings if introduced, caches, queues,
logs, telemetry, backups, providers, regions, and every exposed endpoint. Define retention
and deletion for source and derivative data; restrict privileged and emergency access, record
its use, and review it after role changes. Keep test/development separate from real customer
data and review supplier processing before adding an outbound destination.

## Capability gates

| Gate | Required exit evidence |
|---|---|
| A: Establish scope | Inventory data flows/classes, deployments, owners, identities, providers, local/network surfaces, and known controls; responsibility map and factual claims register. |
| B: Protect local users | First-release evidence plus negative scope/role tests, browser-driven local access checks, credential/file controls, bounded import/export, restore, and release verification. |
| C: Decide transfer | Reviewed D10–D16 selections, scope/custody/offline abuse cases, deletion and telemetry policy; recovery and privacy claims agree. |
| D: Carry customer/internal data | Implement agreed design; hostile-peer/hub, replay/rollback, offline removal, recovery-scope, deletion/restore, key-failure tests and design review, independent where risk warrants it. Document residual limits. |
| E: Enterprise procurement | Dated factual evidence packet, continuing owners, tested identity/admin behavior, scoped external assessment when warranted, findings resolved or disclosed. |
| F: Selected assurance | Buyer-justified scope and continuing capacity; actual issued report/certificate with scope/dates, or explicit remaining gap. |
| G: Sustain | Refresh after material changes; patch supported versions, review access/suppliers/incidents, rotate/revoke credentials, and repeat representative recovery exercises. |

For limited capacity, prioritize boundaries, secret handling, restores, and release integrity
before elaborate management dashboards, duplicate framework systems, or speculative audits.

## Tests to retain

Keep deterministic tests protecting these invariants when reducing CI cost. Add them as their
surface is implemented; the bootstrap does not already provide them all:

- Scope/role checks cover direct reads, mutations, list/search, exports, dependencies, caches,
  background jobs, and recovery. Valid identity is insufficient for wrong objects or actions.
- Tokens reject wrong issuer/audience, expiry, forged callbacks, and cross-connector reuse.
  Local interfaces reject hostile Origin/CSRF attempts where applicable and unsafe exposure.
- Imported instructions cannot grant permissions; model evaluations are separate evidence.
- Transfers reject forged, duplicate, replayed, reordered obsolete grants and rolled-back policy.
  A compromised hub cannot silently enroll a decrypting recipient or redirect recovery.
- Import/export rejects traversal, unsafe names, malformed/oversized input, and interrupted
  migration; resource use, retries, clients, and bulk export remain bounded.
- Recovery denies wrong scope, expired approvals, and self-granted roles; wrong context/key,
  corrupt ciphertext, interrupted rotation, and unavailable key service fail safely.
- Stale peers and restored backups cannot revive deletion or membership. Representative restore
  proves required key availability, retained pending work, and measured recovery objectives.
- Delivered release verification rejects altered bytes or unexpected provenance/source/builder.

Identify invariant, scope, failure signal, owner, and evidence for each retained check. Keep
cheap authorization/serialization tests frequent; run relevant stateful/security integration
gates before release and after boundary changes. A flaky gate needs a tracked fix and
compensating check, not silent removal of the sole invariant test.

## Operators and enterprise assurance

Maintainers own secure code/defaults, release integrity, defect response, and accurate limits.
Operators own hosts/network/IdP, installation and membership, permitted data/retention, key
custodians, recovery approvals, and restores. Any vendor-operated relay, telemetry, support
access, or recovery custody has its own processing and trust boundary; self-hosting does not
erase that responsibility.

A dated procurement packet should explain system/data flows and responsibilities, AI/provider
use if present, actual identity/admin capabilities and gaps, privileged access/audit, retention
and deletion, recovery evidence, secure development/releases, independent assessments, privacy
and contract scope, and support/incidents. Give each item an owner and review trigger.
Share sensitive evidence only with authorized recipients. Do not promise SSO/SCIM, residency,
customer-managed keys, fixed deletion/recovery/notification deadlines, or regulated use without
implementation and operating evidence. Personal-data, health-data, payment, federal-cloud,
and controlled-information obligations require actual deployment/contract scoping.

Commission SOC 2 or ISO/IEC 27001 only when buyer value, stable service scope, control ownership,
material-gap disposition, and continuing operating capacity justify it. Readiness work is not
an issued attestation or certification; a customer's infrastructure needs its own assessment.
