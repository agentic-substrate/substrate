# Offline artifact access and revocation

**Last reviewed:** 2026-10-05 · **Re-read cadence:** at each grant, recovery, or deployment decision

**Status:** bounded administrator-controlled grants for ordinary shared offline access,
sensitive online-only exceptions, and independent wholly owned Personal operation are agreed.
These are design decisions, not implemented behavior. Grant format, durations, clock trust,
runtime enforcement, and recovery mechanisms remain open.

## Policy options

Offline authorization answers how long accepted permissions remain usable without the authority.
It is distinct from replica availability, index freshness, model availability, and pending
shared acceptance.

| Model | Availability | Revocation consequence |
|---|---|---|
| Indefinite accepted shared replicas | Cached work survives arbitrarily long disconnection. | No enforced deadline for learning access changes. |
| Bounded grants plus online-only exceptions | Ordinary work continues until its approved deadline; sensitive access needs connectivity. | New governed access stops at expiry or earlier receipt of revocation. |
| Live check for every shared operation | Every governed request needs current authority authorization. | Network/coordinator outages immediately block shared access. |

The bounded model is agreed. Wholly owned local Personal artifacts remain governed by their
local authority without a shared-coordinator renewal dependency. Imported restricted artifacts
retain their source policies. Administrators set collection-level limits; individual artifacts
can require stricter treatment. No universal numeric duration is justified before disconnected
workflows and acceptable stale-access intervals are agreed.

Authorization caching creates a revocation delay; expiry caps the cache window. This tradeoff
appears in [OAuth token introspection](https://www.rfc-editor.org/rfc/rfc7662.html#section-4).
It is a precedent, not a selected credential protocol. [OPA bundles](https://www.openpolicyagent.org/docs/management-bundles)
can preserve accepted policy offline, but signature verification does not itself establish
authorization expiry. Authenticity and freshness require separate checks.

## Agreed policy

| Artifact context | Behavior |
|---|---|
| Wholly owned local Personal | Use stays independent of a shared coordinator. |
| Ordinary team/organization replicas | Accepted policy and an applicable unexpired grant permit offline use; renewal requires authority. |
| Sensitive shared artifacts | Current online authorization, permitted processing, no offline replica, and compliant retention/runtime delivery. |
| Missing/invalid authorization | Apply inherited restrictions; never infer indefinite offline rights. |

Bind a grant to authority/space, person, installation, repository/artifact scope, actions,
accepted policy/version constraints, and absolute expiry. Organization, group, placement,
processing, and export restrictions continue offline. A Work grant cannot be borrowed by
Personal, another organization, or another installation.

Renew ordinary grants automatically while connected and show their next expiry. During
disconnection, perform only operations covered by valid grants. Shared approval, enrollment,
policy changes, and renewal still require authority acceptance. Received revocation overrides
an unexpired grant. Reconnect checks current access before exchanging content or accepting
queued submissions.

Expiry stops new governed reads, delivery, installation, activation, and processing requiring
that grant. Explain renewal requirements instead of returning a false empty search. Preserve
saved observations, checkpoints, and proposals under their original boundaries; do not erase
them, silently move them to Personal, or approve their submission. Further capture/use requires
its own permissions.

## Worked example

This illustrates the agreed model, not an implemented interface. The 48-hour duration is an
illustrative administrator choice, not a selected default. All times below use the same local
time zone. The node remains asleep after its Tuesday renewal, so no later renewal succeeds.

| Collection | Administrator's example policy |
|---|---|
| Engineering knowledge | Engineering members in approved Work projects and installations may replicate artifacts and read them offline for up to 48 hours after renewal. |
| Production-sensitive | Each new access needs current online authorization; no offline replica or persistent installation into native harness state. |
| Wholly owned Personal | Its local authority operates without organization renewal. Restricted imports keep their original restrictions. |

All artifact types follow these boundaries. A collection or artifact may impose a shorter
window or require online authorization. Lower-level choices can tighten inherited limits,
not relax them. Each installation renews independently: a laptop's grant does not authorize
a separate WSL node.

| Time | Laptop and authority behavior |
|---|---|
| Tuesday 09:00 | Authority renewal succeeds. Engineering access expires Thursday 09:00. |
| Wednesday 08:00 | The laptop wakes without authority connectivity. Its permitted Engineering replicas remain usable for 25 hours; Production-sensitive access requires connection. |
| Wednesday 10:00 | The authority accepts an administrator's revocation and rejects further affected server access and renewal. The disconnected laptop has not received the change. |
| Wednesday 11:00, if it reconnects | The laptop receives the revocation and stops affected governed access immediately. |
| Thursday 09:00, if it stays disconnected | Its existing grant expires and affected governed access stops. |

The accepted Wednesday revocation leaves at most 23 hours of unaware, conforming offline
use in this example. Opening the application, disconnecting, restarting, or reaching only a
model provider cannot extend that deadline. Successful authority renewal is required.

A proposed user status display makes independent states visible:

```text
Context: Work / Example Organization / approved repository
Shared access: offline, valid until Thursday 09:00
Retrieval: lexical only — embedding provider unavailable
Changes: 3 observations saved locally; 1 policy proposal awaiting authority
```

The embedding-provider status above assumes an optional model-based retrieval profile.
The initial retrieval direction uses methods without neural inference. Loss of authority connectivity
alone does not imply lexical-only retrieval: permitted local retrieval components can continue
when their dependencies remain available.
At expiry, the agent receives an explicit authorization-expired result for affected searches,
reads, and activation requests, rather than a misleading empty result or Personal fallback.
Earlier search results do not authorize later reads. Online-only requests explain the
connection requirement even while ordinary Engineering access remains valid.

Previously saved Work drafts remain under Work restrictions. Their preservation grants no
new read, capture, or export rights. Reconnection checks current permissions before exchanging
restricted content or accepting queued submissions; renewal does not approve a policy proposal.
Standalone Personal work remains independent.

The administrator's device roster shows last contact, accepted policy version, outstanding
grant deadlines, revocation acceptance, and each node's acknowledgment. An acknowledgment
reports enforcement; it does not certify erasure of every delivered copy.

For skills and agent definitions, bounded access requires controlled delivery and activation.
An installed plaintext file cannot expire itself, and a running conversation already contains
what it received. Managed session and retention behavior must enforce the requested boundary;
unsupported environments cannot receive artifacts requiring those guarantees.

## Revocation, outage, and recovery

An initial coordinator outage pauses acceptance and renewal. Existing grants keep their
original deadlines; nodes cannot extend them locally. Requested revocation not accepted by
the authority remains pending. Show authority acceptance separately from node acknowledgment,
last contact, and outstanding expirations. After accepted revocation, a disconnected conforming
node may continue only until existing expiry or earlier receipt of that change. Shortening a
policy cannot remotely shorten already issued grants before contact. See [OAuth revocation](https://www.rfc-editor.org/rfc/rfc7009).

Restarts, sleep, clock changes, and restores cannot manufacture new validity. Time uncertainty
and restored credentials need explicit revalidation behavior. Go monotonic readings are
process-local, are not serialized, and may pause during sleep on some systems; persisting
`time.Time` does not solve expiry across reboot. See [Go time](https://pkg.go.dev/time#hdr-Monotonic_Clocks).
Stronger guarantees against a hostile machine owner require the deployment's broader boundary.

The selected [one-owner local restore](security.md#selected-local-recovery) clears all
session/review credentials and preserves captured pending work, pause, bulk jobs, failures,
retirement, and policy. Fresh credentials must pass current surviving-checkout identity checks.
Restored and newly captured authorized work can resume without routes; the
[packaged evidence](../development.md#local-recovery-evidence) includes a new paused offline
capture and another restart. A snapshot contains no later revocation or policy changes.
Shared grant renewal/revalidation remains a future authority protocol.

An extracted content key or copied plaintext cannot be recalled by a software lease. Key
rotation must account for retained old keys and wrappers; rewrapping alone cannot revoke
past possession. Before network transfer, decide custody, recovery scope, and offline key/use
enforcement together. The [transfer/recovery contract](security.md#transfer-and-stale-state)
also requires stale-peer and backup tests that preserve deletions and current membership.

## User and harness behavior

Show access validity, retrieval mode, and pending changes separately. An offline grant can be
valid while an optional retrieval component is unavailable; locally saved drafts can remain pending shared
acceptance. A first shared join has no cached grant to reuse, while Personal initialization
still works independently.

Check before governed delivery, activation, and resume. Native caches, installed plaintext
files, memory stores, and transcripts need controlled destinations. Expiry cannot retract
content already delivered to a model or stop unrestricted filesystem reads. Sensitive online-
only delivery cannot become ordinary persistent global harness state. Pause/restart and
retention behavior need adapter contracts.

OIDC/MCP `offline_access` concerns refresh tokens, not permission to search replicas without
connectivity. [MCP authorization](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)
requires authorization on HTTP requests; local offline grants are a separate Substrate decision.

See [artifact decisions](artifacts.md), [deployment](deployment.md), [sharing](sharing.md),
and [onboarding](onboarding.md). Enrollment sequencing and initial SQLite storage are agreed;
vector retrieval, embedding runtime/model, credential and recovery mechanisms, and
administration interfaces remain subsequent decisions.
