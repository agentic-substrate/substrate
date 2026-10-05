# Context boundaries and selective sharing

**Last reviewed:** 2026-10-03 · **Re-read cadence:** at each access, replication, or delivery decision

**Status:** work/personal separation, group restrictions, explicit placement, repository-level
artifacts, organization-only restrictions, and per-item cross-space publication review are confirmed.
The artifact decision contract is agreed. The one-owner local publication mechanism is selected
below; broader shared policy and publication mechanisms remain open.

The selected one-owner local publication mechanism uses trusted offline owner commands to
configure export and destination publication-write policy. Source-space export defaults to
denied; per-artifact restrictions can add denial but cannot relax the space policy. Every
policy update advances its epoch. Scoped sessions may create restricted derived proposals
from current permitted memories or approved Git snapshots, with their complete declared
dependency inventories. Bundled files inherit their source artifact and space policy; the
implementation does not follow external references or discover independent dependency policy.

A separate 256-bit review credential expires after fifteen minutes and covers one exact
proposal revision, effective source snapshots and override relationships, destination binding,
policy epochs, audience, placement, and recipient-visible provenance. Its trusted offline
issuance permits a browser review; it is distinct from a scoped session credential. The human
reviews the complete proposed text and Personal destination before publication. Publication
rechecks these conditions and atomically consumes the grant while creating a separate
unverified memory. It copies no executable bundle or private source lineage into Personal.
This trusts the local OS owner and browser delivery, not hostile same-account processes.
Whole-installation restore must discard review grants and scoped sessions before promotion.

Artifacts means memories, skills, and agent definitions. The agreed
[artifact decision contract](artifacts.md) maps precedence, search, segmentation, actions,
and approval across all delivery interfaces.

## Required outcomes

- Work memories must not enter personal projects merely because the same person, machine,
  or harness participates in both.
- Selected coding practices, behavioral improvements, and generalized lessons can become
  available across those boundaries through an explicit sharing mechanism.
- Memories, skills, and agent definitions can be restricted to users and groups.
- All three support repository-level applicability alongside broader and narrower scopes.
- An administrator can prohibit access from projects outside their organization, including
  another project used by the same person or installation.
- Policies define what is saved locally, what is replicated, which devices may retain it,
  and which recipients and sessions may retrieve or use it.
- The same controls apply to MCP access, generated harness context, and installed artifacts.
- A human reviews each newly derived item before publication outside its source context
  space. Already approved versions can synchronize to their authorized recipients automatically.

The [repository research](repositories.md) separates repository applicability from source
storage, checkout locations, and access grants.

## Proposed policy model

Separate authority and isolation from applicability. A Work context space and a Personal
context space can each contain project, repository, and task scopes. A reusable-practice
space could contain content explicitly approved for use in both. A person or node's
membership in several spaces does not authorize a session to combine them automatically.

Each artifact has independently meaningful policies:

| Dimension | Purpose |
|---|---|
| Context space and authority | Own its lifecycle and control boundary changes. |
| Applicability | Select the projects, repositories, tasks, or harnesses where it applies. |
| Audience and actions | Grant users/groups read, contribute, approve, export, or publication rights. |
| Placement and replication | Allow local-only persistence, selected device replicas, or authority-hosted content with no endpoint persistence. |
| Processing destinations | Permit or deny transfers to embedding and other processing providers. |

Group membership and role grants are scoped to their authority. Read access does not imply
replication, export, or policy-management rights. Effective access must satisfy the artifact
policy, the principal's grants, the session's permitted spaces, and any device restrictions.
Receiving nodes can request a smaller subscription; they cannot enlarge the sender's grant.

## Proposed enforcement

- Bind each harness/MCP session to a trusted profile and restricted credentials. Tool
  arguments, requested group names, or a claimed repository path cannot widen those grants.
- Keep authorization-sensitive caches separate by profile/principal and policy version.
  Use private cache hints where the negotiated MCP version supports them; changing profiles
  must not reuse restricted resources, tool descriptions, or earlier conversation context.
  Cache freshness hints do not define offline authorization duration.
- Save to an authorized destination with explicit or profile-bound defaults. An ambiguous
  destination must not silently become broadly visible. Agents cannot broaden an audience
  or authorize replication through ordinary memory-writing tools.
- Enforce policy on search, direct reads, listings, subscriptions, writes, replication,
  context compilation, and skill/agent-definition installation. Hiding a listed item is not
  sufficient if its identifier can still be read directly.
- Keep Work and Personal stores and indexes separate where practical. Within a space,
  group restrictions remain necessary. Do not pass unauthorized candidates to rerankers,
  embedding providers, tools, or harness context.
- Apply the same placement and audience rules to content, embeddings, index metadata,
  attachments, rendered files, backups, and access-controlled provenance. Cross-space
  deduplication must not reveal private content or create shared deletion dependencies.
- Install restricted skills and definitions into the relevant profile/project environment.
  Avoid a shared global directory that makes them discoverable in unrelated sessions.
  Git-backed sources need matching visibility boundaries or an authorized version bundle;
  a sparse checkout cannot provide per-artifact authorization within a readable repository.
- Version policy and replication grants. Revalidate access after reconnecting; access changes
  must update subscriptions and remove ineligible local indexes/artifacts where possible.
  Deletion and audience narrowing need explicit synchronization records so old replicas
  cannot revive content. The bounded offline model is agreed; grant durations and enforcement
  remain open.

These controls govern Substrate's application interfaces. Strong isolation from an agent
with unrestricted filesystem access also requires separate OS users, containers, or other
filesystem boundaries. A local-only persistence rule does not itself prohibit delivering
content to a harness that uses a remote model; processing and harness egress require their
own deployment controls. Revocation cannot instantly erase an unreachable recipient's copies.

## Cross-space publication

Keep the original work artifact restricted. Create a separate, generalized version for a
destination such as reusable practice or Personal. Publication requires source export
authorization and destination write/publication authorization. A human reviews each proposed
item's actual content and destination before publication; agents may propose the extraction.
The review includes audience, placement, dependencies, and any provenance visible to recipients.
Approval applies to that version. Synchronization of the approved version to its permitted
recipients does not require another publication review. A revised derived version needs a
new review before it replaces the publication.
The original source link and audit trail remain access controlled and must not leak private
names or details to destination readers.

A summary, consolidation, skill, or agent definition derived from restricted sources retains
those restrictions until an authorized publication decision changes them. Making text more
general does not automatically authorize exporting it. Subsequent edits to the Work source
do not silently update or expand the separately published version.

Publication through a Git repository is also a transfer to that repository's readers and
must satisfy the same review and export requirements. Policy evaluator, credential format,
offline grant durations and enforcement, and physical isolation mechanisms remain open.
The bounded shared offline authorization model is agreed in the [offline contract](offline.md).

Human review cannot override an organization-only or no-export policy. A publication request
remains denied unless the authority controlling that policy authorizes the necessary change.
The artifact contract distinguishes permitted, approval-pending, and denied actions.

## Findings from the research perspectives

The laptop workflow needs a visible active profile, a predictable save destination, and
separate states for local persistence, replication, and shared acceptance. Routine writes
can inherit narrow defaults. Initial publication of a newly derived Work lesson merits
review; subsequent authorized synchronization of that published version need not repeat it.
A standing rule based only on an agent calling something a generic lesson is insufficient.

The harness review found project and global artifact discovery in Codex, Cursor, Claude Code,
and OpenCode, including compatibility directories in several tools. Changing a configuration
directory is not documented as a complete isolation boundary across discovery, credentials,
state, and transcripts. Adapter contracts must test the supported harness versions and use
profile/project delivery with suitable process and filesystem isolation. Access removal
cannot retract context already present in a running or resumed conversation.

An authorized skill or definition must include only authorized dependencies: scripts,
references, prompt bodies, and tool descriptions need the same checks as its main document.
Distribution must not expose the complete source history merely to deliver one approved item.

Personal-device administration needs pairing, a device roster, replacement, and recovery.
Team and organization administration also needs independent device approval, group grants,
processing rules, and an explanation of the applied policy. Backups and restores must not
reactivate revoked grants or deleted content. Authority-hosted content with no endpoint
persistence must be visibly unavailable when disconnected.

Research: [NIST attribute-based access control](https://csrc.nist.gov/pubs/sp/800/162/upd2/final),
[domain-scoped roles](https://casbin.apache.org/docs/rbac-with-domains/),
[group/object relationships](https://openfga.dev/docs/modeling/user-groups),
[MCP authorization](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization),
[MCP tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools), and
[vector filtering and partitioning](https://github.com/pgvector/pgvector).
Workflow research: [explicit device and folder sharing](https://docs.syncthing.net/intro/getting-started.html),
[workspace-associated profiles](https://code.visualstudio.com/docs/configure/profiles),
[harness instruction locations](https://code.claude.com/docs/en/memory), and
[MCP private caching](https://modelcontextprotocol.io/specification/2026-07-28/server/utilities/caching).
Adapter research: [Codex skills](https://learn.chatgpt.com/docs/build-skills),
[Cursor skills](https://prod.cursor.com/docs/skills),
[Claude Code sandbox scope](https://code.claude.com/docs/en/sandboxing),
[OpenCode security model](https://github.com/anomalyco/opencode/security), and
[Git sparse checkout](https://git-scm.com/docs/git-sparse-checkout).
