# Security roadmap and evidence

**Last reviewed:** 2026-10-05. Re-read at each release, trust-boundary change, enterprise
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
| D16 | Verified for initial local stdio; broader transports open | Official Go MCP SDK v1.8.0 and actual versioned Codex, Claude, Cursor, and OpenCode tool traffic are recorded in the development guide. Remote HTTP MCP and native activation require separate evidence. |
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
| Local owner Git registration/approval, scoped qualified/alias delivery, immutable dependency bundles | Verified with local evidence dated 2026-10-04; project maintainers own review | `internal/artifacts` selection/bundle tests and separate owner/scoped CLI commands; dated commands and limits below. Re-run after source, selection, schema, or authority changes. |
| Scoped local conflict resolution and trusted owner artifact restoration | Verified with local WSL 2 evidence dated 2026-10-04; project maintainers own review | `internal/artifacts/reconciliation.go`, explicit source conflict review, and CLI regressions enforce current expected heads, retained history, atomic receipts/work, and fresh source approval after restoration. This is distinct from application backup/restore and multi-node reconciliation. Re-run at the next lifecycle or authority change. |
| Current scoped lexical retrieval, revision associations, private node IPC, and stdio MCP delivery | Verified with local WSL 2 evidence dated 2026-10-04; project maintainers own review | `internal/artifacts`, `internal/node`, `internal/mcpbridge`, and attached CLI tests; actual four-client calls and resource measurements are recorded below and in the development guide. Re-run after retrieval, transport, SDK, client-version, or authority changes. |
| Scoped browser inspection and exact local derived-memory publication | Verified with local WSL 2 evidence dated 2026-10-04; project maintainers own review | `internal/artifacts/publication*`, `internal/authority/review.go`, protected browser forwarding, owner CLI, and packaged browser regressions enforce independent export/write policy, exact short-lived review, immutable retry receipts, and atomic completion. Re-run after publication, browser, schema, or authority changes. |
| Durable foreground maintenance and scoped browser queue/coverage status | Verified with local WSL 2 evidence dated 2026-10-05; project maintainers own review | `internal/artifacts/maintenance.go`, `internal/node/maintenance.go`, scoped inventory and CLI/browser regressions; re-run after scheduling, schema, shutdown, or scope changes. |
| Broader artifact action policy, remote administration/MCP authorization, shared imports/remote publication | Planned | Full first-release boundaries, actual caller attribution, and transport/client-specific checks; local inspection/publication evidence does not verify these capabilities. |
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

On 2026-10-04, the issue #7 worktree passed `env PATH="/usr/local/go/bin:$PATH"
GOCACHE=/tmp/substrate-go-cache TMPDIR=/var/tmp make check` on Linux x86_64 under WSL 2
with Go 1.27.1, Node.js 24.15.0, npm 11.12.1, Git 2.43.0, and SQLite 3.53.4. Runtime revision
`85940872f62cbaffcca65801053d1b6e52c46d75` includes the preceding storage-boundary repairs.
Sixteen new test functions verify immutable approved content after source movement/deletion and
reopen, exact dependency snapshots/provenance, bounded and invalid-UTF-8 inputs, expected-head and
candidate-base checks, retirement, unique qualified identities, ambiguous aliases, explicit
one-level override pins and their invalidation, inspectable relationship metadata, transaction
rollback, version 1 migration, and current Work/Personal and same-space repository filtering.
Removing the repository predicate made its denial test fail; restoring it passed. Removing
approval text validation also failed the before-retry regression. These mutation checks use
synthetic data and leave the production controls intact.

A separate packaged command journey used private `/var/tmp` fixtures and 23 CLI subprocesses
for setup, candidate capture, owner registration, unapproved/stale-approval denial, exact approval,
source/reference edits and removal, pinned content read, duplicate alias denial and authorized
alternatives, qualified reads, retirement, pending work, and revoked reads/listing. Successful
content reads report native activation as unsupported. The final full check also passed the
inherited private-journal/hot-recovery and missing-object transport-denial tests, Go race/shuffle,
vet/format, packaged server smoke, all three existing browser flows, web checks, and built local
document links. The browser source did not change. The pinned govulncheck command reported no
reachable vulnerabilities. Documentation decisions passed against the stacked PR base and
`origin/main`; the issue #7 PR records exact commands and the final documentation revision.

This verifies a trusted local OS owner boundary, not a remote human-review or same-account
hostile-process boundary. It does not prove native installation/execution, complete dependency
inventory discovery, external source/history authorization, MCP/harness compatibility, lexical
retrieval, publication, network transfer, device/power-failure durability, complete backup/restore,
resource targets, another platform, or framework/audit conformance. Owner review must declare
the complete same-commit dependency inventory. Re-run at the next source, selection, schema,
or authority change; later adapters must preserve these checks and independently prove delivery.

A further source-provenance regression on 2026-10-04 demonstrated that `.` could capture
the first regular file from a one-file or multi-file root tree while recording the false
path `.`. The source reader now rejects `.` and requires exactly one NUL-terminated Git tree
entry with a raw path equal to the requested literal filename. Tests preserve exact bytes,
paths, and blob IDs for literal asterisk, tab, and trailing-space names and reject directories
and unmatched glob-looking paths. The final commands and source revision are recorded in
PR #18; these local checks do not extend the existing recovery or platform claims.

The issue #7 source-path follow-up also verified dependency handling on 2026-10-04. A fixture
containing only a regular root `SKILL.md` reproduced an acknowledged candidate for dependency
path `.` before the repair, then rejected it without a receipt after integrating the exact
Git-path check. Runtime revision `a67bfb1bf5eaf12b8b21ab295ac0b1cd314896f8` passed
`env PATH="/usr/local/go/bin:$PATH" GOCACHE=/tmp/substrate-go-cache TMPDIR=/var/tmp
go test -race -shuffle=on ./internal/artifacts ./cmd/substrate` and the full `make check`
under that same environment. All seventeen issue #7 test functions passed, as did the inherited
one-file/multi-file root and exact literal filename checks, the 23-command packaged source
journey, and pinned govulncheck. Documentation decisions passed against the stacked base and
`origin/main`. No private-placement or dependency-authorization exception was introduced;
all previously stated installation, execution, recovery, transfer, and platform limits remain.

On 2026-10-04, issue #8 passed the combined `env PATH="/usr/local/go/bin:$PATH"
GOCACHE=/tmp/substrate-go-cache TMPDIR=/var/tmp make check` at revision
`02b113d` on Linux amd64 under WSL 2 with Go 1.27.1, Node.js 24.15.0, npm 11.12.1,
Git 2.43.0, SQLite 3.53.4, and official Go MCP SDK v1.8.0. The final runtime
`badcd99646564d5d0f2baeef00a70176662fa826` only corrected CLI pause help and passed
`make build` before actual client checks. Both documentation guards passed. Regression
evidence covers current authorization before ranking and metadata, unchanged Personal results
after hidden Work additions, exact pending lookup, association endpoint eligibility, distinct
search/source aliases, stale and retired read denial, transactional indexing rollback and
coalescing, bounded coverage, supported schema migration with unchanged retry receipts,
revoked credentials, private installation locks, unavailable-node saves, concurrent connections,
and malformed UTF-8/UTF-16 or oversized transport frames. Independent review identified ignored
CLI query/limit/relationship arguments and a Unicode prefix-length mismatch; their new tests
failed before the repairs and passed afterward. Sixteen new test functions and the reproducible
resource workload are retained without a coverage quota.

Actual Codex CLI 0.160.0, Claude Code 2.1.289, Cursor Agent 2026.10.01-e373342, and OpenCode
1.18.32 subprocesses exercised discovery, capture, search, exact memory and both approved Git
artifact reads, Personal denials, concurrent sessions, and a stopped installation. Asserted
wire traffic preserved the same Codex artifact/revision/content/provenance when Claude read
from its registered Work worktree. Personal results and coverage stayed empty, direct ID and
qualified source denials disclosed no artifact metadata, and closing bridges preserved the
node lifetime. After a coordinated node restart, Codex and Claude retries returned identical
receipts and read the same saved revisions through actual tool calls. The
[development record](../development.md#actual-client-verification) pins
protocols, binary digest, measured overlap, synthetic-only configurations, and resource limits;
PR #20 records commands. Raw transcripts and credentials remain outside Git. The initial index
uses no model or global corpus statistics, caps each revision at 32,768 token positions, and
reports limited coverage. The node still wakes each second while paused; no energy or larger
corpus target is verified.

This verifies trusted local-owner delivery on the tested WSL 2 installation. It does not verify
remote HTTP MCP, native activation/execution, hostile same-account isolation, publication,
transfer, complete backup/restore, device/power-failure durability, other operating systems,
actual screen-reader or 400% zoom behavior, or framework/audit conformance. The copy-only UI
change reused all three packaged browser flows, keyboard/focus, axe scans, and 320-pixel reflow.
Project maintainers should repeat the scoped and actual-client checks at the next relevant
boundary, SDK, client-version, or release change.

On 2026-10-04, the issue #10 worktree passed `env PATH="/usr/local/go/bin:$PATH"
GOCACHE=/tmp/substrate-go-cache TMPDIR=/var/tmp make check` on Linux amd64 under WSL 2
with Go 1.27.1, Node.js 24.15.0, npm 11.12.1, Git 2.43.0, and SQLite 3.53.4. The new CLI
journey first failed because conflict resolution was unavailable, then passed with explicit
memory selection, retirement, owner restoration, and denial of a pre-retirement edit. Source
conflict review initially failed because stale candidates could not be selected; owner approval
now creates a fresh reviewed snapshot. Adversarial review identified old empty-head candidates
after restoration; regressions verify they remain inspectable and cannot be approved, even
through explicit conflict review. Restored source always needs a fresh candidate approval.
Focused race/shuffle checks also passed for concurrent replacements and resolution versus
retirement, independent observations and retries, stale resolution/restore denial, current
repository filtering and revoked retries, legacy approval receipts, and whole-transaction
rollback of history, pending work, and index invalidation. The issue #10 PR identifies the
immutable revision and exact commands. All existing packaged browser, axe, keyboard, web,
and built-document checks passed; no browser behavior or MCP mutation surface changed.

This evidence covers local lifecycle transitions under the trusted owner boundary. It does
not prove multi-node synchronization, coordinator authority, application backup/restore,
device-failure durability, shared acceptance, factual verification, publication, native
activation, hostile same-account isolation, other operating systems, or framework/audit
conformance. Private fixtures used `/var/tmp` because this host's unrelated `/tmp/.git`
marker makes `/tmp` invalid for authority placement; no control was exempted or removed.
Project maintainers should repeat these checks at the next lifecycle or authority change.

On 2026-10-04, the issue #9 worktree passed `env PATH="/usr/local/go/bin:$PATH"
GOCACHE=/tmp/substrate-go-cache TMPDIR=/var/tmp make check` on Linux amd64 under WSL 2
with Go 1.27.1, Node.js 24.15.0, npm 11.12.1, Git 2.43.0, and SQLite 3.53.4. The owner
policy/review commands and browser routes first failed as unsupported. Scoped credentials
cannot load or approve human review; exact proposal/source/bundle/override/destination and
policy epoch changes invalidate live review, with conjunctive source denial and no override
by confirmation. Separate grants cannot publish one proposal twice. Regression evidence covers
hidden Work/Personal and same-space repository sources, current source eligibility, expired
or revoked grants, exact historical retries, strict bounded UTF-8/UTF-16 transport, and private
recipient provenance. Deferred SQLite commit failure leaves no recipient, receipt, index work,
grant consumption, or proposal completion; repair permits the same exact retry. Temporarily
ignoring the commit error made that assertion fail. Moving the owner callback outside its
authority lock also failed a native lock assertion; restored controls passed race/shuffle
checks, and replacing a registered checkout with a same-path clone denied issued review.

All nine packaged browser flows passed, including an actual offline owner grant followed by
stale review denial and fresh exact approval into Personal. Axe scanned loading, empty, error,
unavailable, pending, denied, conflict, and success states. Native keyboard focus/confirmation,
late-response cancellation, exact retry identities, desktop/mobile screenshots, and 320-pixel
and 320-by-256 reflow were checked. No actual screen-reader application or true browser/OS
400% zoom was tested; equivalent viewport reflow and green axe scans do not prove conformance.
The pinned `govulncheck` v1.8.0 reported no vulnerabilities. Both documentation checks passed;
the issue #9 PR records the immutable revision and exact commands. Synthetic private fixtures
used `/var/tmp` without exempting this host's unrelated `/tmp/.git` placement restriction.

This verifies one trusted local OS owner and separate human review credentials, not remote
human authentication, hostile same-account isolation, organization administration, automatic
secret or undeclared-source detection, executable publication/native activation, network
transfer, complete backup/restore, power-failure durability, or another platform. The browser
lists at most 100 scoped artifacts/proposals, and serialized reviews over seven MiB cannot
receive a grant. Whole-installation restore must invalidate sessions and all review grants;
completed private proposal audits are distinct from credential authority. Project maintainers
should repeat these checks at the next publication, browser, source, policy, schema, or authority
change.

On 2026-10-05, the issue #12 worktree passed `env PATH="/usr/local/go/bin:$PATH"
GOCACHE=/tmp/substrate-go-cache TMPDIR=/var/tmp make check` on Linux amd64 in WSL 2,
with Go 1.27.1, Node.js 24.15.0, npm 11.12.1, Git 2.43.0, and SQLite 3.53.4. Nineteen new
Go regressions cover schema 4-to-5 migration, durable pause/startup override, coalescing to
current heads, unchanged-posting preservation, explicit forced rebuild, deferred bulk restart,
failed-job restart/edited-head persistence and healthy progress, a 100-failed-attempt bound,
pre-transaction and checkpoint cancellation, bounded incomplete-frame shutdown, credential
denial versus node unavailability, concurrent maintenance, and full scoped counts beyond the
bounded browser list. A status-poll regression first left five queued jobs indefinitely pending;
the corrected worker serves controls without resetting its active delay. Test-only SQL
triggers inject failures/cancellation without production test hooks. Cooperative explicit-batch pause and retained forced-rebuild intent after indexed/unindexed
history-only contributions also passed review regressions. Indexing preserves saved
receipts and the independent pending operation ledger.

All fifteen packaged browser flows passed, including six maintenance flows and a real
owner-controlled paused capture, rebuild, restart, isolated Personal status, resume, and
complete scoped lexical coverage. Missing/malformed status, inconsistent count relationships,
failures, deferred work, empty/partial/limited coverage, revoked access, and canceled late Work
responses remain distinct. Axe scanned relevant states, native keyboard controls and focus
were checked, and desktop/320-pixel reflow screenshots were inspected. An actual screen-reader
application and true OS/browser 400% zoom remain untested; equivalent viewport reflow does not
prove conformance. No new HTTP route, browser maintenance control, automatic refresh, or
external telemetry is introduced.

The [foreground resource evidence](../development.md#foreground-maintenance-evidence)
records a fresh packaged binary hash, synthetic workload, exact command, node CPU/RSS and
latency, and a real zero-route offline namespace restart on WSL 2. Both SIGINT and SIGTERM
preserve durable pause, receipt identity, current reads, pending work, and resumed retrieval;
duplicate startup fails and normal shutdown removes only the owned socket. The implementation
commit and PR identify the source revision and exact checks. This evidence does not establish
power/device-failure durability, whole-installation restore, continuous WSL/distro survival,
service packaging, native Windows/macOS, battery/host energy targets, or remote/shared
security. Project maintainers should repeat it after schema, indexing, queue, shutdown,
authority, or status-scope changes.

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
