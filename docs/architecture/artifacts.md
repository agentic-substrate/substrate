# Artifact precedence, discovery, and policy

**Last reviewed:** 2026-10-04 · **Re-read cadence:** at each policy, search, or adapter decision

**Status:** artifacts means memories, skills, and agent definitions. The decision order,
type-specific precedence, search boundaries, segmentation, denied/approval/allowed distinction,
and mandatory organization restrictions are agreed. Per-item cross-space publication review
is also agreed. Local repository-scoped persistence is selected below. Broader policy
evaluation, publication, isolation mechanisms, and offline grant durations and enforcement remain open.
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
`synchronous=EXTRA`. The cgo-free driver avoids compiler/build-tag requirements and registers
its supported FTS5 extension for later lexical indexing. The actual capability check reports
SQLite 3.53.4. Compared with `modernc.org/sqlite` v1.60.1 it needs a smaller production dependency
set; `mattn/go-sqlite3` v1.14.52 needs cgo and an FTS5 build tag.
This choice does not establish performance or memory targets. The driver documents higher
per-connection memory use from its Wasm sandbox; actual application resource measurements
remain future evidence. See the [driver guidance](https://github.com/ncruces/go-sqlite3/tree/v0.35.6)
and [SQLite synchronization settings](https://www.sqlite.org/pragma.html#pragma_synchronous).

A storage session retains its credential privately and authenticates through current local
authority on every operation. Caller-supplied destination IDs can narrow or match that binding;
they cannot grant another space or repository. Every lookup filters owner, space, and repository
before loading content, revision, receipt, or pending-work metadata. Missing and inaccessible
objects share a generic denial. This is repository-scoped authorization for one trusted OS
user; plaintext files and mode protection do not isolate that user or root.

A successful contribution atomically commits artifact identity, immutable revision content,
provenance, operation receipt, and pending incremental work. Schema version 1 is installed
transactionally; a newer unknown schema fails with an actionable unavailable error. A
contribution operation ID is unique within owner/space/repository. An exact retry returns its
original receipt; a changed payload under that ID is rejected. No receipt is returned before
commit. Storage failure remains unavailable, even when indexing has no consumer yet.

Memory observations are unverified and pending-local. An edit against the current expected
revision advances the local head; an outdated edit is preserved as a conflict candidate and
cannot replace that head. Retirement checks the expected head and persists lifecycle state;
later edits remain restricted candidates and cannot restore the artifact. Restoration,
verification, publication, and shared acceptance are unavailable. Provenance is a claim from
the contributing session, not independently verified factual evidence.

Skill and agent-definition contributions read exact bytes from a full Git commit in the
session's registered checkout, retain commit/path/blob identity, and remain candidates with
no effective head. A moving ref, an unsafe path, or a symlink blob is rejected. Capturing does
not approve or execute source. Trusted registration and explicit approved-version selection
are separate work; neither source text nor operation arguments can approve content.

The local reopen and rollback checks prove the tested application's acknowledgment boundary.
They do not prove device-failure durability, complete backup/restore, replica recovery,
synchronization, retrieval readiness, or harness compatibility. Re-read this decision when
schema, driver, authorization, Git selection, indexing, or recovery behavior changes.
