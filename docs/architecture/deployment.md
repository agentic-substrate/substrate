# Deployment and disconnected operation

**Last reviewed:** 2026-10-04 · **Re-read cadence:** at each architecture decision

Go is selected for the backend, CLI, and MCP. Vite + React is selected for the browser,
with its static build embedded in Go for the first usable release. See the
[interface contract](delivery.md).
The agreed direction is locally runnable nodes, synchronization of selected context, and
shared authority for team and organization policy.
The initial release may pause shared acceptance during a coordinator outage, with automatic
coordinator failover as a planned evolution. Initial networking uses authenticated HTTPS
synchronization to configured, reachable endpoints with explicit device enrollment.
Hybrid retrieval and work/personal isolation with group restrictions are required.
Initial retrieval uses ranked text and explicit semantic associations without neural inference.
Embeddings are an optional candidate subject to quality/resource evaluation. Battery use during
interactive work is a product constraint; optional enrichment must be resource-aware.
Shared offline access uses administrator-controlled finite grants, with online-only sensitive
artifacts and independent wholly owned local Personal operation. Grant durations and enforcement
mechanisms remain open.
SQLite is selected for local nodes and the initial single coordinator. The
[local persistence contract](artifacts.md#selected-local-persistence-contract) selects its Go
driver. Vector backends, embedding runtimes/models, network credential protocols and
identity-provider integrations, and synchronization mechanisms remain under discussion.

Future peer-to-peer and hub-and-spoke transfer of permitted customer/internal context is
confirmed direction. A hub may provide authoritative management and metrics, and enterprise
administrators need content recovery within assigned scope. Initial HTTPS synchronization and
one coordinator remain selected; topology does not grant content or recovery authority.
The [transfer/recovery contract](security.md) records proposed plane separation and unresolved
custody/enforcement. Meet its [security gates](../plans/security.md#capability-gates) before
network transfer carries customer/internal data.

The [installation contract](packaging.md) selects Linux and WSL 2 initially, with
macOS and native Windows later. Foreground operation and optional user services are the recommended
lifecycle direction; concrete integrations remain open.

## Supported deployments

| User situation | Required outcome |
|---|---|
| One user, multiple harnesses, one machine | Harnesses use common context without operating a separate remote service. |
| One user, multiple WSL distributions | Isolated Linux environments exchange context while retaining their own local paths and runtime state. |
| One user, multiple laptops or servers | Context moves across a network, with useful local operation during disconnection. |
| Multiple users in a team | Shared context and review coexist with personal context and explicit membership. |
| Multiple teams in an organization | Organization and team context can be shared selectively; membership in one team does not expose another team's private context. |

Every deployment must account for temporary disconnection, enrollment, reconnection,
permanent departure, and replacement of a device. A physical computer, a local installation,
a person, a harness, and a context scope are distinct identities.

The [sharing requirements](sharing.md) define separation between work and personal context,
group restrictions for memories, skills, and agent definitions, and controls over local
placement, replication recipients, and harness delivery.
The [repository research](repositories.md) covers context shared across registered clones
and worktrees while preserving branch, task, and subdirectory applicability. Each installation
needs local checkout bindings; another machine's absolute paths are not portable identifiers.
Easy user joins and administration are major product goals. The [onboarding contract](onboarding.md)
distinguishes people, enrolled installations, repository associations, and restricted harness
sessions with a combined setup experience. Local ownership and pairing come first,
administrator-verified invitations serve early teams, and optional organization OIDC follows.

## Disconnected operation

- Available local context remains usable according to the selected offline policy.
- Authorized new work is durably recorded locally; disconnection must not silently discard it.
- Reconnection reconciles changes and reports conflicts or rejected submissions.
- Content never stored locally cannot become available merely because offline mode begins.
- A new standalone personal installation can initialize without joining a remote service.
- Removing a member cannot instantly notify a disconnected machine or erase copies it already received.

Personal changes and authorized new observations can be written locally while disconnected. Changes to
shared instructions, approved skill versions, membership, and permissions remain proposals
until the relevant shared authority accepts them. Local operation uses the last accepted
shared policy, subject to the agreed bounded offline authorization model and valid grants.
Reconnection revalidates pending submissions against current access and accepted versions;
it does not guarantee their acceptance. The agreed [reconciliation contract](reconciliation.md)
uses automatic synchronization of independent observations and explicit resolution of competing
artifact revisions. Wire formats, retention, and implementation remain open.

Read access, local authorship, submission, and shared activation have separate guarantees.

The [offline access contract](offline.md) defines finite administrator-controlled grants
for ordinary shared artifacts and online-only treatment for sensitive artifacts. Owned local
Personal remains independent of a shared coordinator. The policy model is agreed; durations,
clock trust, runtime enforcement, and recovery mechanisms still require decisions.

## Membership and lifecycle

- Joining requires an explicit trust and access decision before receiving shared content.
- Temporary unreachability does not mean permanent removal.
- Reconnection checks current access before transferring additional shared content.
- Retries must not duplicate submissions or lose an acknowledged change.
- Deletions and replacement versions must survive reconnection with older replicas.
- A node returning after synchronization history has been compacted needs a recovery path
  that preserves its unsent local work.
- Backup restoration and cloned installations need defined identity and synchronization behavior.

## Agreed architecture direction

A locally runnable Go node can operate independently and synchronize selected context with
other installations. Personal, team, and organization context spaces have explicit authority
and access boundaries. A node may participate in several spaces without sending personal
context to every shared coordinator. Physical topology need not mirror ownership.

Shared coordination can run on an existing node or a dedicated service. Ordinary laptops and
WSL installations are synchronization participants; their departure must not remove a
consensus quorum needed to accept shared policy. Automatic coordinator failover is a planned
evolution; its stable-server membership and failover require a separate design.

Automatic peer discovery, multi-peer reconciliation, failover mechanisms, future coordinator storage, and
offline grant durations and enforcement still need explicit decisions before implementation.
Application synchronization uses HTTPS initially; its record format and reconciliation
protocol remain open. SQLite is the initial durable database with the local driver selected;
vector storage remains open.

## Agreed availability path

Initially, a shared context space can use one coordinator. If it becomes unavailable,
shared approvals and permission changes pause. Nodes continue using locally available
accepted context under the offline policy and durably record personal work, observations,
and pending proposals. Recovery must include tested backup and restore procedures.

Automatic failover is a planned capability. Its target behavior is to resume shared
acceptance after a coordinator server fails, while maintaining a single shared authority.
Failover time and allowable loss of acknowledged changes still need explicit targets.

The design should preserve stable context-space and node identities when coordinator
addresses or storage change. Synchronization contracts should support endpoint replacement
and recovery from a stale checkpoint without discarding pending local submissions. These
constraints prepare for failover without requiring a cluster in the initial release.

## Agreed initial storage

SQLite is the selected local store: embedded transactions support durable local changes
without a separately operated database service. Each installation keeps its database on
local storage; WSL distributions and networked machines exchange application records
through synchronization rather than share a live database file. The current local store uses
rollback journaling with EXTRA synchronization and immediate transactions; concurrent retry
tests validate that acknowledgment path. The selected cgo-free driver is pinned in the
[local persistence contract](artifacts.md#selected-local-persistence-contract). SQLite still
serializes writers, and representative workload/resource measurements remain future work.

For shared coordination, a single service using SQLite is the agreed initial default,
consistent with the agreed availability path. This is a design decision, not implemented behavior.
PostgreSQL is a candidate for greater write concurrency and database replication; automatic
failover still requires orchestration. Replicated SQLite through a stable-server Raft system
such as rqlite is another candidate for the planned failover capability. The future backend
is not selected; migration and failover need their own validation.

Database replication does not replace selective application synchronization, authorization,
or proposal review. Hybrid retrieval is required; its vector backend remains open.

| Initial storage path | Personal installation | Shared operation | Main tradeoff |
|---|---|---|---|
| SQLite local nodes and single coordinator | Embedded database; no separate database service. | Coordinator owns its local database and serves application requests. | Simplest initial deployment; each database serializes writes. |
| SQLite local nodes, PostgreSQL coordinator | Same embedded local experience. | Coordinator connects to an operated PostgreSQL service. | Greater database write concurrency, with an additional service and two storage implementations. |
| PostgreSQL on every installation | Each node operates a local database service for disconnected work. | A coordinator can also use PostgreSQL. | Consistent database family, with more installation, update, and recovery work on laptops and WSL. |
| Replicated SQLite coordinator | Local nodes remain independent. | Stable coordinator servers maintain quorum. | Adds consensus and read-consistency decisions; reserved for the planned availability evolution. |

Use SQLite locally and at the initial single coordinator. Keep record identities,
revision and acceptance semantics, and selective synchronization independent of SQL dialect;
a future backend migration must preserve those contracts. This does not require implementing
several database backends now. Write concurrency and index workload measurements should
determine when the coordinator needs a different backend, not a guessed user-count threshold.

Each installation owns its database on its local filesystem. Harnesses and worktrees use
the node instead of opening separate databases or sharing a live database file. Separate WSL
installations and laptops exchange authorized application records, not database-file copies.
The selected embedded engine does not select a vector implementation or an embedding provider.

Backup and restore are product operations. Use a consistent database snapshot and include
authoritative records, pending work, and policy/revocation state; define recovery of any
external authored content separately. Derived indexes can be rebuilt without losing those
records. Restores cannot clone a device's enrollment or reset expired grants. A durability
contract must define when local writes and coordinator acceptance are acknowledged; SQLite
WAL synchronization settings affect survival of power/system failure. See the
[backup API](https://www.sqlite.org/backup.html) and
[synchronization settings](https://www.sqlite.org/pragma.html#pragma_synchronous).

Storage selection alone does not establish Work/Personal isolation or encryption. Every
representation, provider call, and backup needs the accepted artifact boundaries; physical
partitions, keys, retention, and restore procedures remain implementation decisions.

Storage research: [SQLite deployment fit](https://www.sqlite.org/whentouse.html),
[WAL concurrency](https://www.sqlite.org/wal.html),
[cgo-free SQLite driver](https://pkg.go.dev/modernc.org/sqlite),
[PostgreSQL concurrency](https://www.postgresql.org/docs/current/mvcc-intro.html),
[PostgreSQL failover](https://www.postgresql.org/docs/current/warm-standby-failover.html),
and [rqlite design](https://rqlite.io/docs/design/).

## Agreed initial connectivity

Start with authenticated HTTPS synchronization to configured, reachable endpoints. An
endpoint can be another personal node or a shared coordinator. Personal installations do
not require a dedicated always-on server, but exchanges wait until a configured endpoint is
reachable. Network addresses locate nodes rather than define their identity.

Enrollment explicitly authorizes a node and its permitted context spaces, with
credentials independent of any optional VPN account. Reachability alone must not grant
context access. The enrollment sequence is agreed; credential formats, verification and
recovery procedures, and provider integrations remain open.

A LAN, a user-operated VPN, or a reachable self-hosted endpoint can provide initial network
connectivity. Tailscale is an optional candidate, including its Go embedding library; it is
not selected or required. Built-in peer discovery, NAT traversal, and relay fallback are
deferred; their priority remains open. HTTPS alone does
not establish a route through firewalls or NAT.

Connectivity research: [Syncthing device identities](https://docs.syncthing.net/specs/bep-v1.html),
[Syncthing relaying](https://docs.syncthing.net/users/relaying.html),
[Go TLS](https://pkg.go.dev/crypto/tls),
[Tailscale embedding](https://tailscale.com/docs/features/tsnet), and
[libp2p hole punching](https://docs.libp2p.io/concepts/hole-punching).

Research inputs: [local-first software](https://www.inkandswitch.com/essay/local-first/),
[replication checkpoints](https://docs.couchdb.org/en/stable/replication/protocol.html),
[replication conflicts](https://docs.couchdb.org/en/stable/replication/conflicts.html), and
[consensus membership](https://etcd.io/docs/v3.6/learning/design-learner/).
