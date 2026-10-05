# Offline changes and conflict resolution

**Last reviewed:** 2026-10-05 · **Re-read cadence:** at each synchronization or artifact-lifecycle decision

**Status:** local durability, explicit shared authority, current permission checks on reconnect,
Git source ownership, and reviewed cross-space publication are agreed. The type-aware
reconciliation policy below is selected for the initial implementation.
Synchronization wire formats, cross-node identifiers, retention, and implementation are
still open; the selected local persistence contract below is implemented.

## Agreed initial policy

Synchronize independent additions and deduplicate retries automatically. Represent edits as
candidate revisions based on a known version. Preserve competing authorized revisions and
resolve them explicitly rather than letting a device clock choose the effective content.
Use the relevant authority to accept shared state under current policy. This does not select
CouchDB, a CRDT library, or full event sourcing; SQLite remains the selected durable store.

| Situation | Agreed behavior |
|---|---|
| New independent memory observations | Retain both with scope, evidence, author, and unverified/verified status; apparent contradiction does not make them the same record. |
| Retry of the same contribution | Reuse its operation identity and receipt; acknowledgement loss must not create duplicates. Reject a changed payload under the same operation identity. |
| Edit based on the current version | Accept only when current permissions, expected version, and required review conditions still hold. |
| Competing edits to the same versioned artifact | Retain candidates, show a conflict to authorized users, and keep accepted effective content until resolution or retirement. |
| Git-owned skill or agent source | Reconcile source through the repository workflow; activate an explicitly approved source version. A clean Git merge does not approve execution or publication. |
| Shared membership, permissions, active version, or publication | Submit to the authority with expected-state checks; an offline proposal cannot directly activate shared state. |
| Accepted deletion versus an old offline edit | Keep the artifact retired; preserve the edit only as a restricted candidate under retention policy. Restoration requires an explicit authorized operation. |
| Submission after access changes | Revalidate before content transfer or acceptance; rejected work does not become shared merely because it was saved locally. |

Artifact revision conflicts and conflicting factual observations are different. Two memories
can disagree without being edits of the same artifact. Retrieval must preserve evidence and
verification distinctions rather than merging their prose into an apparently confirmed fact.
Identical edits to the same base can coalesce only when content and governed metadata match;
retain provenance. Equal text in different scopes is not permission to deduplicate records.

## Why this direction

[CouchDB's conflict documentation](https://docs.couchdb.org/en/stable/replication/conflicts.html)
illustrates keeping concurrent revisions while selecting one deterministic default view.
Its examples show why retaining an alternative is insufficient if the application hides it.
Substrate should expose authorized conflicts and report the effective/pending state explicitly.

[HTTP conditional updates](https://www.rfc-editor.org/rfc/rfc9110.html#section-13.1.1) establish
the expected-version pattern for avoiding accidental overwrites. A rejected activation must
not discard a candidate already acknowledged as durably saved. Permission checks precede
disclosure of current versions or conflict details.

[Git merge](https://git-scm.com/docs/git-merge) can combine changes against a common ancestor.
This is useful for source review and candidate generation, but a textual merge is not evidence
that instructions, memory claims, access conditions, or executable definitions are correct.
Merge assistance may create a draft for explicit resolution; general automatic text merging
is deferred initially.

[Automerge](https://automerge.org/docs/reference/documents/conflicts/) shows that CRDTs can
combine concurrent text edits, yet some scalar updates still have competing values and a
deterministically selected view. Convergence does not establish truth, permission, or review.
Consider CRDTs later for specific data types where automatic merging has a defined meaning;
do not make arbitrary prose and governed state silently merge by default.

## Review and harness behavior

Group competing revisions by artifact and show the common base, accepted version, candidates,
and permitted resolution actions. Offer keep, choose, or combine as new content. Recheck
current access before showing source or provenance and before accepting the resolution.
Resolution against an outdated head must itself report a conflict rather than overwrite it.

An agent may propose a resolution, with explicit pending/conflict status and permitted next
steps. It cannot claim shared activation or satisfy a human-review requirement through a tool
argument. Approval remains bound to exact content/version and destination: a merged candidate
is new content and does not inherit approval from either input.
An unchanged previously approved snapshot retains its applicable authorization; advancing the
source head does not silently substitute new content into it.

Ordinary recall returns eligible effective artifacts and evidence under the existing
[artifact contract](artifacts.md). Authorized locally saved unverified memory observations
can remain searchable with pending-local status while awaiting shared acceptance. Competing
replacement candidates and draft skills/definitions belong in authorized review views,
with their status visible. An unresolved proposed skill version
cannot replace an approved version automatically. Retired or denied content cannot become
eligible because an old node reconnects. Unauthorized conflict IDs, counts, sources, or
candidate snippets must not appear in search or error messages.

## Recovery and boundaries

Persist authorized local work and pending submission state before attempting synchronization.
On reconnect, establish current membership and policy before transferring content. Retention
and access still govern locally saved restricted drafts; preservation is not a new access or
export grant. Do not transfer a revoked writer's rejected draft merely to fill an admin inbox.

Represent accepted deletion/retirement so older replicas cannot resurrect content. History
compaction needs a recovery boundary: a sufficiently stale node revalidates and obtains an
authorized current snapshot before offering its remaining candidates for reconciliation.
Exact deletion markers, compaction rules, draft retention, and restore protocol remain open.

Validate duplicate retries, two offline edits, edit versus retirement, stale approvals,
revoked submissions, and permission-safe review before declaring synchronization ready.
The local store now preserves expected-base candidates, operation receipts, and retirement
state under the [selected persistence contract](artifacts.md#selected-local-persistence-contract).
This is not a synchronization engine or synchronization acceptance test.

Selected [local whole-installation recovery](security.md#selected-local-recovery) preserves
captured conflict candidates, immutable history, retirement, retry receipts, pending operations,
and durable maintenance state in a fresh installation. It clears session/review credentials
and rechecks surviving checkout identity when issuing new sessions. Snapshot retirement does
not establish knowledge of later retirement or revocation; shared current-authority
revalidation and stale-peer reconciliation remain separate unimplemented work.

## Selected local resolution and restoration

Scoped memory resolution chooses an exact authorized revision, including the accepted head
for a keep action, against the current expected head. It creates a fresh unverified local
revision with the chosen evidence and associations, and retains earlier competing candidates
as resolved history. Combined memory text is a new expected-base contribution; there is no
automatic merge. Independent observations remain separate artifact identities.

Git source resolution remains a trusted local owner approval. The explicit conflict-review
flag permits review of a current candidate or conflict whose original base is stale, but still
checks the current expected effective head and applicable override/default pins. The reviewed
exact bundle becomes a fresh approved revision; no textual merge or source-order preference
selects it. Combined source requires a new Git snapshot and explicit approval.

Only the trusted owner CLI restores retired artifacts. Memory receives a fresh unverified
head so old edits cannot replace it. A restored approved Git snapshot becomes a fresh candidate
without copied approval or override authority. Previous source candidates become
`retired-candidate` and cannot activate through an old empty-head expectation. If no approved
source head exists, restore changes lifecycle only and reports `restored-awaiting-candidate`;
a fresh proposal and owner approval are required. These owner actions have no node, browser,
or MCP endpoint and retain the trusted OS-account boundary.

Each transition commits its receipt, revisions, lifecycle/head, pending work, and incremental
index invalidation together. Changed arguments under an existing operation identity conflict;
exact retries return the original receipt without undoing later retirement. Current authority
checks still precede receipt disclosure. These local checks do not decide shared restoration,
tombstones, compaction, cross-node identifiers, or synchronization authority.

See [deployment](deployment.md), [offline grants](offline.md), [source ownership](repositories.md),
and [sharing](sharing.md).
