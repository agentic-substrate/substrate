# Repository context and artifact sources

**Last reviewed:** 2026-10-03 · **Re-read cadence:** at each scope, adapter, or storage decision

**Status:** repository-level artifacts, per-item review before newly derived cross-space
publication, a checked-in discovery manifest, and mixed source ownership are agreed. Captured
memory belongs in Substrate; reviewed Git knowledge, skills, and agent definitions can be
associated with the same repository. Easy user joins and administration are major product
goals. Exact registration, default write scopes, and adapter mechanisms remain proposals.
Manifest filename/format and identity/enrollment credentials are not selected.

## Observed environment

Read-only inspection of `~/repos` covered directory names, artifact/configuration locations,
and selected Git directory metadata. It did not read unrelated project content, credentials,
or remote URLs, and did not classify projects as Work or Personal.

- The folder contains main repositories, sibling linked worktrees, and hidden worktree folders.
- `plotlens` and `plotlens-wt-apibuild` have different checkout roots but share the same Git
  common directory. They demonstrate why a directory path is not a repository identity.
- Repository artifacts occur in locations such as `AGENTS.md`, `CLAUDE.md`, `.agents/skills`,
  `.claude/skills`, and `.claude/agents`. Nested instruction files also occur within monorepos.
- No targeted instruction, skill, agent, or MCP location was present at the `~/repos` root.
  That inventory does not establish what any running harness session actually loaded.

## Proposed identities and bindings

Register a logical repository with a stable identifier under its context-space authority.
Record each installation's canonical checkout roots and verified Git worktree relationships
as local bindings. A repository can have many checkouts on many devices without depending on
the same filesystem path everywhere. Reuse across context spaces requires explicit permission.

Paths, remote URLs, branch names, copied manifests, and matching commit histories are discovery
hints, not grants. Keep the approved association and session grants outside repository-controlled
files. An unknown checkout receives no restricted space access merely because it appears under
`~/repos`. A repository-local manifest can describe requested configuration without authorizing it.

Recognized linked worktrees can inherit an already approved repository association under a
defined local registration policy, avoiding repeated classification of every task checkout.
Each still has separate checkout, branch, task, and delivery state. Use native Git metadata
rather than directory-name suffixes to determine the relationship.

Another clone requires registration as a new placement. A verified move can preserve identity;
an ambiguous move or changed Git binding needs repair before restricted delivery continues.
Changing a remote URL does not transfer authority. Forks and independently rooted submodules
start with separate bindings unless explicitly associated. Removing a checkout does not delete
the logical repository's knowledge or unfinished work recorded elsewhere.

## Agreed discovery manifest, format under review

A small repository file can make clone and worktree setup predictable. For illustration,
`.substrate.toml` could describe an association without storing enrollment credentials:

```toml
schema_version = 1
repo_id = "repo_example"
space_id = "space_example"

[authority]
id = "authority_example"
endpoint = "https://context.example.com"
key_fingerprint = "sha256:<approved-public-key-hash>"

[sources]
skills = [".agents/skills"]
agents = [".claude/agents"]
```

The example is a design sketch, not an implemented format. Authority and artifact locations
are optional when a standalone repository uses only local registration. Source declarations
need explicit formats and revision ownership before adapters can import or deliver them.
Only metadata appropriate for the Git repository's readers belongs in this file.

Stable repository, space, and authority IDs associate the records. A public key or fingerprint
helps verify the expected authority; it is not a membership grant or proof of organization
ownership. Keep identity independent of rotating keys and changing endpoints. Authenticate
HTTPS normally; any signing-key pin has a distinct purpose and does not replace transport
validation or artifact authorization.

An unfamiliar authority requires a trusted setup channel or administrator-provisioned trust.
Store the accepted association and key pin outside the checkout. The authority checks the
person/device's membership before granting access, and an already authorized node can reuse
an accepted association for recognized worktrees or validated clones. A copied manifest or
changed branch cannot transfer membership, change the accepted authority, or activate drafts.

A checked-in join reference can locate an authenticated enrollment request. If possession of
the code itself grants enrollment or access, it is a bearer credential and belongs outside Git;
prefer narrow scope, short lifetime, and one-time use. Private keys, device credentials, local
trust decisions, and personal overrides also remain outside tracked files.

Initially the manifest can remain an unsigned discovery hint. Changing its space or authority
requires a new approved binding rather than silently replacing accepted trust. Signed binding
updates are a future option, verified against previously accepted authority keys with explicit
rotation and freshness rules. Existing permitted local context remains usable offline under
the chosen grant policy; a first shared join cannot manufacture trust while disconnected.

## Proposed applicability and access

Within authorized context spaces, repository scope supports narrower subdirectory, branch,
task, and session applicability. Durable repository knowledge can apply across registered
checkouts. A branch observation carries source revision and branch/task evidence so another
checkout does not receive it as a universally applicable fact. Branch names alone are not
durable task identities and can be reused.

A monorepo directory without its own Git root is a subpath scope. An independently rooted
nested repository or submodule has a separate binding; opening both does not combine audiences.
An explicit project/workspace association can select multiple repositories without changing
their access boundaries. Multi-repository writes require an unambiguous selected destination.

Repository applicability intersects user/group grants, session spaces, device placement,
and processing policy. A more specific instruction cannot weaken those policies. A launcher
or trusted session controller resolves the binding; ordinary tool arguments or a change of
working directory cannot widen credentials. Crossing a space boundary requires a separately
authorized session that does not reuse restricted conversation context.

## Agreed source ownership

Combine versioned file sources with Substrate-managed repository records:

| Artifact/state | Recommended ownership |
|---|---|
| Skill and agent-definition source, including scripts and references | Versioned Git source in the code repository or a separately permissioned source repository. |
| Captured memories, evidence, checkpoints, and proposals | Substrate records associated with the authorized repository and narrower applicability as needed. |
| Audience, placement, bindings, approval, and active-version selection | Substrate authority records; tracked files cannot grant themselves access. |
| Harness-specific output | Authorized delivery from selected revisions; distinguish it from editable source. |

Git source remains editable and reviewable through ordinary repository workflows. Its Git
readers must be authorized for the source, dependencies, and reachable history. Deliver an
authorized version bundle when recipients should receive one approved artifact without the
whole source repository. Sparse checkout and ref namespaces cannot create per-item read grants.

Keeping every artifact in the code repository simplifies clone-based portability but shares
it with that repository's readers and history. Keeping all content in Substrate gives one
policy surface but requires export and file-authoring workflows. The combination preserves
normal Git authoring while allowing private and group-restricted repository context; it also
requires explicit source ownership, import, revision selection, and conflict behavior.

Git-backed memory is not universally discouraged. Current Claude Code documentation stores
main auto memory outside the checkout but recommends version-shareable project memory for
subagents. VS Code recommends keeping evolving repository memory local and moving verified,
durable knowledge into reviewed repository guidance. These are different supported workflows,
not one industry-wide storage rule.

For Substrate, the agreed default is structured capture in its records because evidence,
verification, audience, placement, and synchronization need independent lifecycles. Selected
reviewed knowledge can be published to Git when its readers are authorized. Optional Git-backed
memory sources would keep Git authoritative for text and branch history; Substrate would index
the selected revision and propose edits through that source workflow. Neither direction should
silently create a second writable owner or automatically broaden the source's audience.

Allow existing repository instructions, skills, and definitions to be discovered for an
onboarding preview. Discovery does not approve activation, replication, or export. Preserve
native source files, distinguish imported source from generated delivery, and show changes
as proposals instead of silently overwriting authored content. Automatic ingestion of native
harness memories and synchronization of edits back to their sources need separate contracts.

Proposed save defaults: durable memories become unverified repository records; checkpoints
and temporary observations retain branch/task applicability; new skills and definitions begin
as repository proposals. A human can inspect the active space, repository, audience, placement,
and save destination. Creation does not activate a shared skill or definition. Default scopes
and the relationship between local proposals and approved context need an implementation
contract before capture is enabled.

## Harness research and adapter implications

| Harness | Documented behavior relevant to repository scope |
|---|---|
| Codex | Instructions concatenate from project root to launch directory. Skill discovery uses project ancestors up to the repository root alongside global roots. |
| Cursor agent | Supports repository/nested instructions and scoped skills. CLI MCP discovery includes parent directories; its outer cutoff needs a supported-version test. |
| Claude Code | Ancestor instructions can load from outside the repository. Auto memory is repository-scoped and shared across worktrees; documented worktree artifact fallback also needs adapter coverage. |
| OpenCode | Project skill discovery walks to the Git worktree and includes compatible folders. Configured instruction sources combine with native rules; exact nested-root behavior needs version tests. |

Substrate must resolve its authorized deterministic context before adapter delivery. Account
for parent instruction files, nested scopes, global compatibility folders, imports, symlinks,
worktree fallback, name collisions, and revisions. A common parent folder such as `~/repos`
must not become an implicit shared Work/Personal context store. Changing a harness configuration
directory alone is not evidence of isolation across all discovery and state locations.

Adapters cannot retroactively control native files an unrestricted harness can read. Strict
separation needs appropriate process/filesystem boundaries as described in [sharing](sharing.md).
Tests of supported harness versions must verify effective loading and CWD changes; a location
inventory and current documentation are not runtime compatibility tests.

## Operational implications

Single-device users need one-time repository classification and worktree-aware defaults.
WSL distributions and other devices register their own paths against approved logical
repositories. Offline use reads authorized local repository context and records proposals;
reconnecting rechecks current grants before replication or shared activation.

Personal administrators need an inventory of bindings, devices, and destinations with repair
and replacement workflows. Team/org administrators additionally need repository/group
associations, per-repository placement and processing controls, and explanations of effective
access. A restored binding cannot restore expired or revoked grants. Publication to another
space, including through a broader-readable Git repository, follows the agreed per-item review.

Research: [Git worktree metadata and lifecycle](https://git-scm.com/docs/git-worktree),
[Git path resolution](https://git-scm.com/docs/git-rev-parse),
[mutable remotes](https://git-scm.com/docs/git-remote),
[Git read-access limitations](https://git-scm.com/docs/gitnamespaces#_security),
[Codex instruction discovery](https://learn.chatgpt.com/docs/agent-configuration/agents-md),
[Codex skills](https://learn.chatgpt.com/docs/build-skills),
[Cursor instructions](https://prod.cursor.com/docs/rules),
[Cursor MCP discovery](https://prod.cursor.com/docs/cli/mcp),
[Claude Code instructions and auto memory](https://code.claude.com/docs/en/memory),
[Claude Code worktrees](https://code.claude.com/docs/en/worktrees),
[OpenCode rules](https://opencode.ai/docs/rules),
[OpenCode skills](https://opencode.ai/docs/skills), and
[multi-repository workspace settings](https://code.visualstudio.com/docs/editing/workspaces/multi-root-workspaces).

Manifest and memory research:
[public identity and explicit device sharing](https://docs.syncthing.net/intro/getting-started.html),
[public discovery key versus enrollment token](https://kubernetes.io/docs/reference/setup-tools/kubeadm/kubeadm-join/),
[public token IDs and secret credentials](https://kubernetes.io/docs/reference/access-authn-authz/bootstrap-tokens/),
[repository configuration trust](https://code.claude.com/docs/en/mcp#project-server-approvals-and-workspace-trust),
[Claude subagent memory scopes](https://code.claude.com/docs/en/sub-agents#enable-persistent-memory), and
[VS Code memory lifecycle](https://code.visualstudio.com/docs/agents/run/memory).
