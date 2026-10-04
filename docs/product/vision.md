# Vision: context that compounds across sessions

**Last reviewed:** 2026-10-04 · **Re-read cadence:** at each phase exit

Substrate's goal is a durable, self-hosted context layer beneath interchangeable coding
harnesses. What a person and a project learn should outlive the conversation that discovered
it. Choosing a different tool or machine should preserve the permitted knowledge, working
practices, and eventually the state of unfinished work.

The intended experience is simple: begin a session with the right context, do useful work,
leave behind evidence and improvements, and let the next session build on them. People should
spend less time repeating project briefings and reconciling copied configuration. Administrators
should be able to explain and control that flow without managing every harness separately.

This is the destination. Today the repository provides status/context inspection, trusted
local bindings, and repository-scoped artifact persistence through the CLI. Retrieval,
approved Git version selection, MCP tools, and synchronization are not implemented. The
[getting started guide](../getting-started.md) describes current behavior, while the
[first release contract](first-release.md) defines the first usable product.

## Knowledge becomes practice, and practice carries forward

Memories, skills, and agent definitions are **artifacts**. They serve different purposes;
sharing a storage or delivery layer must not erase those distinctions. Instructions,
preferences, and planned checkpoints complete the broader context experience.

**Instructions establish constraints.** Applicable rules have a deterministic resolution within
an authorized context space. A narrower repository or task instruction can specialize an
allowed default, but it cannot weaken mandatory access or processing restrictions. Required
instructions must survive context-budget pressure instead of competing with memory relevance.

**Preferences preserve working choices.** A person's permitted preferences can follow them
between projects and machines. Preferences remain separate from instructions and cannot
override required project rules. Work-specific choices remain within Work until sharing is
authorized; a preference being personal does not make its source unrestricted.

**Memories preserve knowledge and evidence.** Facts, decisions, lessons, incidents, and
observations carry source, scope, status, and history. Agent observations start unverified.
Verification can establish a reliable lesson; newer or conflicting text cannot silently
replace a confirmed fact. As code changes, stale claims and contradictions should become
visible. Feedback and relevance scores alone cannot establish truth.

**Skills preserve repeatable practice.** A reviewed procedure should be available at a known
version wherever it is permitted. Git owns its authored content and history; Substrate tracks
applicability, approval, and the selected version. A proposed improvement goes through source
review and activation rather than silently replacing the procedure in use. Repository-level
applicability does not require keeping every source in that repository.

**Agent definitions preserve roles and behavior.** Versioned definitions describe how a harness
uses context, tools, and approved procedures for a role. They have the same explicit audience,
placement, and approval boundaries as other artifacts. Receiving a definition grants no
additional authority. Substrate supplies it; the harness runs the agent.

**Continuity preserves unfinished work.** Structured checkpoints and handoffs should eventually
carry the objective, accepted decisions, progress, and next steps into another session or
harness. A person can continue without reconstructing an entire conversation. Full transcript
restoration is an optional convenience for validated clients; basic continuity must work
without access to a harness's private storage format.

## A day with the intended system

A developer begins a Work project in one harness. Its trusted repository binding selects the
permitted instructions, preferences, relevant memories, and approved skills and definitions.
The session does not receive unrelated Personal context or another team's private artifacts.

During an investigation, the harness records an observation with its source and destination.
The developer switches harnesses in a sibling worktree. The next session can find the permitted
observation and see that it is unverified, alongside the currently approved procedure. Review
and verification can turn the investigation into a dependable lesson and an improved skill.

That lesson may contain a useful coding practice for Personal projects. The original stays
inside Work. If the source policy permits export, the developer proposes a separate generalized
version and reviews its exact content, audience, and destination before publication. Later
changes to the Work source do not silently alter the published copy. An organization-only
restriction keeps the material inside the organization, even when a person calls it generic.

Later, the developer moves to another enrolled laptop. Selected artifacts and a checkpoint
let a fresh session continue the investigation. Locally available context remains usable while
disconnected according to its policy: wholly owned Personal work stays independent; shared
context needs valid offline authorization, and online-only sensitive content is unavailable.
Authorized new work is saved locally. Reconnection checks current access before transferring
content, preserves competing revisions, and reports proposals that cannot be accepted.

The lesson has become useful context for another session without making all of Work available
everywhere. This learning and continuity loop is the end result we are building toward.

## One experience from a laptop to an organization

Start with one person using several harnesses on one Linux or WSL node. Prove useful recall,
clear boundaries, reviewed versions, and durable saves there before adding distribution.
Then support multiple WSL instances, followed by multiple laptops and servers. The same
principles extend to a team and to an organization with several teams; those are distinct
administrative and trust contexts, rather than one unrestricted pool of knowledge.

Joining a device, associating a repository, or inviting a teammate should be straightforward.
A portable repository manifest can help discovery; trusted registration and current grants
establish access. Paths, directory names, copied manifests, and network reachability do not
confer membership. Administrators need understandable enrollment, departure, replacement,
and recovery flows, including when installations disconnect and later return.

A harness needs a predictable active context, save destination, selected version, and honest
result state. A person needs to inspect and correct knowledge, manage their devices, and review
sharing. An administrator needs to control membership, group access, permitted projects,
replication, and processing destinations. These views describe the same governed artifacts.

## Trust and resource use are part of the product

Shared authority is assigned per context space. Keeping Work and Personal on one machine
must not make them mutually accessible. Permission checks come before search ranking and
before disclosure through listings, direct reads, generated files, installation, replication,
or external processing. A more relevant result or more specific instruction cannot defeat a
denial. An administrator can prohibit outside-organization project access across every surface.

People should be able to see why an artifact applies, which revision is effective, whether it
is verified, what is saved locally, and what can be synchronized. Empty results, unavailable
context, pending approval, and denied actions must have distinct meanings. Explanations must
not reveal unauthorized titles, snippets, source details, or hidden-result counts.

Useful context must fit the user's work and machine. The intended context pack protects
required instructions and preferences, selects eligible relevant knowledge, and provides an
index of approved procedures instead of loading every artifact into every prompt. Retrieval
starts with text, trusted context, and explicit associations. Embeddings are optional and must
justify their quality and resource costs; background maintenance should be incremental,
bounded, observable, and pausable without losing newly captured work.

These are application guarantees, not a promise to erase information from hostile device
owners or existing conversations. Memory remains evidence rather than a directive. Scoped
delivery and review reduce risk; they do not make prompt injection a solved problem.

## What success looks like

- A harness switch preserves the applicable rules, working choices, and permitted knowledge.
- A verified lesson or reviewed procedure improves later work, with its evidence and version visible.
- A fresh session on another machine continues from a useful checkpoint without another briefing.
- Work, Personal, repository, and group boundaries remain understandable during search and use.
- A person can add or replace a device without recreating each harness's setup.
- An administrator can explain access, enforce organization restrictions, and manage departures.
- Disconnection preserves authorized work, and reconnection exposes conflicts without silent loss.
- Knowledge stays useful as code changes, without constant model work draining the laptop.

The [roadmap](https://github.com/agentic-substrate/substrate/blob/main/ROADMAP.md) orders these
outcomes; it does not claim they all ship in the first release. The [architecture decisions](../architecture/index.md)
record selected boundaries and open mechanisms for deployment, policy, retrieval, delivery,
and reconciliation. Runtime libraries, identity protocols, synchronization formats, and any
embedding implementation still need review when their contracts are built.

Substrate remains beneath the harness. GitHub or Linear remains the human work board; future
worker coordination may use this context foundation without turning Substrate into an IDE or
a general-purpose agent loop. The [non-goals](non-goals.md) keep that boundary explicit.
