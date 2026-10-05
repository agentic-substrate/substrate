# Artifact precedence, discovery, and policy

**Last reviewed:** 2026-10-05 · **Re-read cadence:** at each policy, search, or adapter decision

**Status:** artifacts means memories, skills, and agent definitions. The decision order,
type-specific precedence, search boundaries, segmentation, denied/approval/allowed distinction,
and mandatory organization restrictions are agreed. Per-item cross-space publication review
is also agreed. Local repository-scoped persistence is selected below. Broader policy
evaluation, remote publication, isolation mechanisms, and offline grant durations and enforcement remain open.
The [offline model](offline.md) uses finite administrator-controlled shared grants with
online-only sensitive exceptions and independent wholly owned Personal operation.

## One decision contract

Every interface uses the same accepted policy and trusted session binding. Relevance or
specificity cannot grant access. A manifest suggests a repository association; accepted
registration establishes the association used for authorization.

| Order | Decision |
|---|---|
| Trusted context | Resolve principal, authority/organization, permitted spaces, repository/project, installation, and processing destination from the session. |
| Permission | Require mandatory organization/space conditions and the action's audience/device grants. An applicable denial wins. |
| Applicability | Select artifacts relevant to the repository, subpath, branch, task, and explicitly permitted shared scopes. |
| State and revision | Apply lifecycle, verification, approval, and selected-version rules for the artifact type and operation. |
| Effective definition | Resolve stable artifact identities and explicit approved overrides/pins; report unresolved conflicts. |
| Relevance | Rank only eligible candidates. Scores cannot affect policy, approval, or authority. |
| Delivery or action | Recheck read, install, activation, replica, export, and processor permissions. A result ID grants no subsequent access. |

Permission filtering precedes disclosure and external processing. Indexes accelerate selection
but do not establish grants; stale permissions cannot leak through the response. Errors in
mandatory checks deny the operation. Missing authorization cannot become an approval request
that bypasses the missing grant. Offline use requires accepted policy and valid offline grants.

[Cedar authorization](https://docs.cedarpolicy.com/auth/authorization.html) provides a precedent
for denial overriding permission. Cedar skips erroneous policies, so diagnostics would need
explicit handling to satisfy this contract. [OPA filtering](https://www.openpolicyagent.org/docs/filtering)
illustrates protected collection queries. Neither evaluator is selected.

## Artifact precedence

Mandatory access, placement, processing, and export policies constrain every type. Narrower
team, repository, artifact, user, or session rules can restrict them further but cannot relax
a higher authority's restriction.

| Type | Search default | Resolution and use |
|---|---|---|
| Memory | Current permitted evidence with verification and pending-local state visible. Ordinary recall excludes superseded, rejected, retired, and deleted records. | Scope selects applicability; ranking orders evidence. Contradictions and supersession are explicit. Newer, narrower, or higher-scoring claims do not silently replace confirmed facts. |
| Skill | Applicable approved versions, with qualified identity and selected revision. | An explicit approved specialization can override an overridable default. Resolve one version for the requested identity/alias before delivery. Read permission does not authorize script execution. |
| Agent definition | Applicable approved versions, with qualified identity and selected revision. | Resolve approved specialization/pins before compilation. Requested tools/providers remain bounded by session policy; a definition cannot grant additional rights. |

Verification, shared acceptance, lifecycle, and activation are separate dimensions. Locally
written unverified evidence can remain searchable to its authorized author while awaiting
shared acceptance. It does not become a directive or approved fact. Draft skills/definitions
appear in authorized review views, not automatic installation. Memory is quoted evidence.

The instruction design resolves active keys from global through organization, team, project,
repository, branch, task, and session within authorized spaces. Preferences retain a separate
user/team/organization overlay. Neither order overrides mandatory policy. Exact matching and
override declarations require a schema before implementation.

Specificity selects only along explicitly declared, approved override relationships. Equal
names in different sources are not such a relationship. Qualified IDs preserve both allowed
choices; an explicit alias can select one. Unresolved ambiguity blocks activation of that
alias while discovery can show authorized alternatives. Timestamps, similarity, and filesystem
read order do not settle definition conflicts.

Native precedence differs across harnesses, even between skills and agent definitions in
Claude Code. Adapters must deliver Substrate's selected revision and reject delivery when
mandatory restrictions cannot be preserved. See [skill collisions](https://code.claude.com/docs/en/skills#resolve-skills-that-share-a-name)
and [agent-definition scopes](https://code.claude.com/docs/en/sub-agents#choose-the-subagent-scope).

## Organization-only example

An administrator can define a mandatory boundary for an artifact or collection:

```text
Owner: Org A
Use: sessions bound to Org A projects only
Audience: authorized Org A Engineering members
Placement: approved Work installations only
Processing: approved harnesses/providers only
Publication outside Org A: prohibited
```

This is a policy sketch, not a configuration format. Trusted organization/project association
is necessary in addition to membership. The same person's Personal session cannot receive
the information because that person also belongs to Org A. Repository instructions, copied
manifests, and tool arguments cannot defeat the rule. [Organization-context authorization](https://openfga.dev/docs/modeling/organization-context-authorization)
provides a precedent for including the active organization in the access decision.

For one person in Org A Engineering, the illustrative result is:

| Active session | Org A Engineering-wide | Org A app-specific | Org A Finance-only | Practice separately published to Org A and Personal | Personal-only |
|---|---|---|---|---|---|
| Org A / app | Eligible | Eligible | Hidden | Eligible | Hidden |
| Org A / ops | Eligible | Out of scope | Hidden | Eligible | Hidden |
| Personal / hobby | Hidden | Hidden | Hidden | Eligible | Eligible |
| Org B / service | Hidden | Hidden | Hidden | Hidden | Hidden |

Eligible still requires grants, state/revision eligibility, placement, and processor permission.
The published practice is a separate approved artifact, not automatic access to its source.
Organization-wide discovery needs an organization-bound session and explicit discovery grants;
it does not widen a Personal session.

## Search and explanation

Default search covers the active repository and permitted inherited/shared scopes. Explicit
discovery can inspect a broader authorized organization scope; matching a result does not make
it applicable for automatic use in the current repository. History/review modes require their
own grants and preserve draft/lifecycle labels.

Allowed results can include identity/type, revision, scope, status, visible evidence, policy
revision, selection reason, permitted actions, approval requirements, and lexical/semantic
relevance. Keep permission and relevance explanations separate. Do not invent confidence scores.

Explain individual exclusions only when the caller can inspect that artifact's metadata.
Otherwise explain the session boundary without unauthorized titles, IDs, snippets, paths,
hidden-result counts, or descriptions. Direct reads and dependency fetches repeat the checks.
MCP [tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools) and
[resources](https://modelcontextprotocol.io/specification/2026-07-28/server/resources) can vary
availability by request credentials; Substrate must still enforce the application policy.

| Outcome | Meaning |
|---|---|
| Empty | Search completed over permitted locally available context without a match. |
| Blocked | The requested operation lacks a grant or fails a mandatory condition. |
| Requires approval | Policy permits the operation, but its necessary approval is absent. |
| Reduced/unavailable | Indexing, provider availability, placement, or authority freshness limits service; report the actual limitation. |

## Actions and approval

| Action | Required decision |
|---|---|
| Search/list/preview/read | Action grant, session boundary, and any configured read approval; ordinary permitted access needs no publication review. |
| Capture memory/propose a revision | Authorized destination and contribute grant; inherit restrictions. Agents write unverified evidence or proposals. |
| Verify memory/accept shared changes | Explicit verifier/authority permission; ordinary writes cannot change trust or policy. |
| Install skill/definition | Approved selected revision, applicability, dependency access, and permitted destination. |
| Activate/invoke | Use permission, allowed harness/tools/providers, no unresolved conflict, and required approval. Installation grants no execution rights. |
| Synchronize approved versions | Authorized recipients/devices and placement; routine distribution needs no repeated publication review. |
| Publish derived versions across spaces | Every source permits export, destination permits publication, and a human reviews each new version's content and destination. |
| Broaden audience/placement/processing/policy | Authorized policy-management action under the controlling authority, within mandatory ancestor restrictions. |

Approval covers a version, audience, and destination. It cannot override denial. Under an
organization-only rule, even a generalized lesson stays prohibited outside the organization.
An authorized change to the controlling export policy must precede publication review;
a lower-scope exception or personal approval cannot bypass it.

An authority may require approval for an otherwise permitted read or use. The caller needs
permission to request it, and pending approval exposes only authorized metadata. This is
distinct from discovering an inaccessible artifact or requesting an export forbidden by policy.

## Segmentation and derivation

Keep ownership/space, group/action audience, applicability, placement, processing, and publication
independent. Logical partitions cover content, lexical/vector indexes, caches, rendered files,
backups, attachments, and provenance. Physical stores, keys, and OS boundaries remain choices.

Dependencies and derivatives preserve every source restriction. Combining Engineering-only
and Finance-only sources requires both audience conditions until authorized publication;
membership in either group is insufficient. Preserve each source's actual policy expression
rather than flattening group labels into a union. [Multiple restrictions](https://openfga.dev/docs/modeling/multiple-restrictions)
shows the conjunction pattern. Scripts, references, included skills, and prompt bodies need
the same checks as their container. Public provenance cannot reveal restricted source names.

The guarantee covers Substrate-mediated retrieval, delivery, installation, replication,
publication, and processing. Stronger isolation requires a harness environment that cannot
read unrelated Work files, reuse Work conversations, or send content to disallowed processors.
A Work-to-Personal switch needs fresh authorized context; old conversation content cannot
be retracted. Offline revocation follows the agreed [grant policy](offline.md), with durations
and enforcement mechanisms still open. See [sharing](sharing.md),
[retrieval](retrieval.md), and [repository bindings](repositories.md).

## Selected local persistence contract

The one-owner implementation stores artifacts in `artifacts.db` beside trusted authority
state, outside Git checkouts, under an owner-only directory. SQLite uses the pinned
`github.com/ncruces/go-sqlite3` v0.35.6 `database/sql` driver, rollback journaling and
`synchronous=EXTRA`. The encoded `modeof` URI parameter makes driver-created journals inherit
the validated private database mode without changing process-wide umask. Tests cover live
transactions, a waiting independent opener, and recovery from an abruptly exited process
with a hot rollback journal; they do not prove device-failure recovery. The cgo-free driver
avoids compiler/build-tag requirements and registers its supported FTS5 extension for later
lexical indexing. The actual capability check reports
SQLite 3.53.4. Compared with `modernc.org/sqlite` v1.60.1 it needs a smaller production dependency
set; `mattn/go-sqlite3` v1.14.52 needs cgo and an FTS5 build tag.
This choice does not establish performance or memory targets. The driver documents higher
per-connection memory use from its Wasm sandbox; representative foreground application
resource measurements appear in the
[development evidence](../development.md#foreground-maintenance-evidence), with workload and
platform limits. See the [driver guidance](https://github.com/ncruces/go-sqlite3/tree/v0.35.6)
and [SQLite synchronization settings](https://www.sqlite.org/pragma.html#pragma_synchronous).

A storage session retains its credential privately and authenticates through current local
authority on every operation. Caller-supplied destination IDs can narrow or match that binding;
they cannot grant another space or repository. Every lookup filters owner, space, and repository
before loading content, revision, receipt, or pending-work metadata. Missing and inaccessible
objects share a generic denial. This is repository-scoped authorization for one trusted OS
user; plaintext files and mode protection do not isolate that user or root.

A successful contribution atomically commits artifact identity, immutable revision content,
provenance, operation receipt, and pending incremental work. The original version 1 schema migrates
transactionally through selection tables, version 3 revision associations and the derived lexical
index, version 4 publication records, and version 5 durable maintenance pause/queue/failure state. A newer unknown schema fails with an actionable unavailable error. A
contribution operation ID is unique within owner/space/repository. An exact retry returns its
original receipt; a changed payload under that ID is rejected. Text content and every
contribution/lookup/lifecycle string must be valid UTF-8 before fingerprinting or persistence.
Invalid bytes are rejected instead of being normalized to U+FFFD; valid U+FFFD is preserved.
No receipt is returned before commit. Storage failure remains unavailable; derived index failure cannot invalidate a saved receipt.

Memory observations are unverified and pending-local. An edit against the current expected
revision advances the local head; an outdated edit is preserved as a conflict candidate and
cannot replace that head. Explicit scoped memory resolution checks the current expected head,
copies the selected revision's content, provenance, and associations into a fresh unverified
revision, and retains competing history with resolved state. Keeping the current revision uses
the same fresh-revision transition. Combining text is a separate contribution with its own
expected head; no automatic text merge or inherited factual verification occurs.
Retirement checks the expected head and persists lifecycle state; later edits remain restricted
candidates and cannot restore the artifact. A separate trusted local owner operation restores
memory into a fresh unverified head so pre-retirement edits stay stale. Shared acceptance and
factual verification remain separate from local publication. Provenance is a claim from
the contributing session, not independently verified factual evidence.

Skill and agent-definition contributions read exact bytes from a full Git commit in the
session's registered checkout, retain commit/path/blob identity, and remain candidates with
no effective head until separate trusted approval. A moving ref, an unsafe/non-UTF-8 path,
a symlink blob, or invalid UTF-8 source content is rejected. Source paths identify one literal
regular file, never a directory or `.`. Git tree reads require exactly one NUL-terminated entry
whose raw path equals the requested path; literal wildcard characters, tabs, and trailing spaces
remain supported. Source commands use an empty `GIT_ALLOW_PROTOCOL` allowlist, which overrides
repository transport settings and blocks missing-object promisor fetch helpers and network
access. Required objects must already be local. This mechanism is verified on Git 2.43.0,
rather than relying on an unsupported no-lazy-fetch flag. See the
[versioned Git environment contract](https://github.com/git/git/blob/v2.43.0/Documentation/git.txt).
Capturing does not approve or execute source. Trusted registration and explicit approved-version
selection and restoration follow the contract below; neither source text nor scoped operation arguments can
approve content.

The local reopen and rollback checks prove the tested application's acknowledgment boundary.
They do not prove device-failure durability, complete backup/restore, replica recovery,
synchronization, retrieval readiness, or harness compatibility. Re-read this decision when
schema, driver, authorization, Git selection, indexing, or recovery behavior changes.

## Selected local Git approval contract

The single local owner uses private filesystem authority through dedicated registration and
approval CLI commands. These commands accept no session credential and have no browser or MCP
endpoint. Scoped sessions can propose candidates, inspect their permitted metadata, and read
approved snapshots; they cannot register identities or approve content. This separates interface
capabilities within the trusted OS-account boundary, not hostile processes running as that account.
Remote human review remains unavailable. Local derived-memory publication uses the separate
exact-proposal credential and policy checks described below; source approval grants no export right.

Registration binds one Git-backed artifact to a stable qualified identity consisting of its
space, repository, kind, source namespace, and name. Namespaces and aliases are labels, not
permissions. Registration is immutable and unique within that qualified identity; a retired
identity cannot be re-registered to evade retirement. An optional alias may refer to several
registered artifacts. Permission filtering runs before computing candidates, conflicts, or
alternatives. Exact qualified reads preserve distinct permitted choices; ambiguous aliases
block content delivery instead of selecting by source order, time, or similarity.

Approval pins the whole immutable revision, including its explicit dependency inventory, and
compares the expected effective head and the candidate's base. Stale approval cannot replace a
newer head or undo retirement. Updating a Git branch, removing source files, or changing dependency
bytes does not alter the stored approved snapshot. A new source or dependency version requires
another candidate and explicit approval. Schema version 2 transactionally adds registrations and
approved override records to the version 1 store. Approval, receipt, selected head, and pending
index work commit together; a failed commit returns no success receipt.

The owner can explicitly review a competing candidate against the current expected head.
Conflict approval copies that exact immutable bundle and its associations into a fresh revision,
checks current registration and override rules, and commits its approval with the new head.
Prior competing source candidates retain resolved history. Existing approval retries retain
their original fingerprints when the conflict-review flag is absent. Combined source text or
dependencies must first become a fresh Git candidate; approvals of the inputs do not approve
the result. Scoped sessions cannot approve source or settle definition precedence.

Restoration is a separate owner command with the expected retired head and a new operation
identity. It invalidates prior source candidates, including edits captured while retired, as
`retired-candidate`. A previously approved head becomes a fresh candidate with no effective
head and no copied approval or override relationship. A source retired before any approval
returns `restored-awaiting-candidate` with no revision and requires a fresh proposal. Only a
fresh candidate and owner approval can make restored source effective. The owner must declare
current override pins again; restoring content cannot restore stale precedence. Receipts,
pending work, revision changes, lifecycle, and index invalidation commit atomically. Exact
resolution and restoration retries return historical receipts without changing current state.

An override must explicitly name an active, approved, same-kind default in the same authorized
repository and space, pin its current revision, and target a default marked overridable at
registration. The first implementation permits one-level specialization only: override chains
and cycles are rejected. Multiple approved specializations remain incomparable alternatives and
block their alias. A changed or retired default invalidates its existing specialization and
blocks that alias until explicitly resolved. A qualified default remains independently readable
while active; qualification is never permission to read a different scope.

A source bundle contains its main regular Git blob and up to 32 explicitly listed dependency
blobs from the same full commit and registered repository, bounded to one MiB combined. Import
uses raw objects with replacement refs disabled, rejects symlinks and unsafe paths, and stores
exact commit/path/blob provenance alongside each dependency's bytes. The owner is responsible
for declaring the complete dependency inventory during review. Substrate neither follows
references in source text nor loads external files or executes scripts. Content reads return
the stored bundle and do not establish native installation, activation, executable safety,
harness compatibility, broader source access, or transfer authorization. Future adapters must
reject any dependency or destination they cannot preserve under the governing boundary.

## Selected local publication contract

Schema version 4 adds source-scoped immutable proposal revisions, versioned local export/write
policy, hashed review grants, and private completion audits. A source-space export grant is
required; additional per-artifact denial and destination publication-write policy apply
independently. Every policy command advances its epoch. Each explicit source must be current
and eligible, with approved Git bundles and override relationships pinned as complete snapshots.
Candidate, retired, stale, or unresolved sources cannot satisfy review. Declared bundle files
inherit the local source policy; references are not followed and independent dependency policy
discovery is not implemented.

Publication creates a separate unverified Personal memory with empty associations, no Git
source/dependencies, and fixed generic provenance. The source-side audit retains exact content,
source inventory, destination bindings, and policy epochs privately. Policy, source, proposal,
or destination changes invalidate unconsumed review. Two grants for one proposal cannot create
two publications. All publication writes, receipt/index work, audit, and grant consumption are
atomic; failed commit returns no receipt. A review snapshot exceeding seven MiB of encoded
JSON is rejected before issuing a credential, preserving the bounded local transport.

Exact acknowledged proposal retries recover their original immutable payload after source or
policy changes if their current session still authenticates. An exact consumed publication
retry can recover its historical receipt while its grant remains unexpired, unrevoked, and
bound to the current checkouts. Neither historical acknowledgement establishes current export
or read permission. Live review and any new publication recheck current eligibility and policy.
Whole-installation restore must clear review grants and scoped authority sessions before
promotion; source-private completed audits remain in the proposal records.
