# Transfer, authority, and recovery

**Last reviewed:** 2026-10-04. Re-read before changing identity, key custody, recovery,
network transfer, offline grants, or telemetry.

**Status:** information relay first, future peer-to-peer and hub-and-spoke transfer of
permitted customer/internal context, scoped enterprise recovery, and optional authoritative
hub management and metrics are confirmed product direction. None is implemented. Separating
relay, control, recovery, and audit services is proposed; key custody and concrete protocols
remain open. The [security roadmap](../plans/security.md) records decisions and release gates.

This extends [deployment](deployment.md), [sharing](sharing.md), [onboarding](onboarding.md),
[offline grants](offline.md), and [reconciliation](reconciliation.md). It preserves the
single-node [first release](../product/first-release.md), initial HTTPS synchronization with
one coordinator, and finite shared offline grants. Peer-to-peer topology is future scope;
it does not replace the selected initial coordinator or require automatic discovery.
Substrate relays context and definitions; a downstream harness retains its own execution
and permission boundary.

## Assets and authority

Protect content, provenance, approved revisions, credentials, membership, policy, key material,
deletion history, and recovery records. Personal and employer/customer context occupy separate
trust domains by default. Cross-domain transfer requires source export permission, destination
permission, and the existing exact-content publication review where applicable.

Decide the recoverable tenant/project/collection boundary before designing keys. Ownership,
artifact applicability, replication placement, processing, and recovery scope are separate
conditions. An email address, repository path, enrolled device, reachable internal network,
or valid TLS credential cannot establish current object/action authorization. This applies
[NIST SP 800-207](https://csrc.nist.gov/pubs/sp/800/207/final) to Substrate's boundaries.

Treat five permissions independently:

| Authority | Required boundary |
|---|---|
| Content | Read, write, share, export, and delete only permitted objects/actions. |
| Management | Configure only assigned spaces and policy; mandatory ancestor restrictions still apply. |
| Enrollment | Approve an identity/device and possession of its key without self-granting content or recovery scope. |
| Recovery | Recover content only within currently assigned scope under independently enforced policy. |
| Analytics | Read approved operational fields without acquiring content, enrollment, or recovery rights. |

A hub may be authoritative for assigned membership and management functions. That does not
settle who can change decryption recipients or recovery policy. Dashboard administration
must not silently confer recovery custody. Signed content provides origin/integrity evidence,
not permission or proof that its instructions are safe. Imported memories remain evidence;
skills and definitions need explicit approval before trusted activation. Downstream policy
must deny unauthorized actions even when imported text requests them.

## Proposed service separation

| Component | Proposed responsibility and trust |
|---|---|
| Endpoint/adapter | Validate and expose permitted plaintext to a harness; compromise exposes that plaintext. |
| Encrypted relay | Route/store envelopes; see only permitted routing metadata and hold no ordinary content/recovery keys. |
| Control plane | Maintain scoped membership, devices, policy versions, and management authority. |
| Customer recovery authority | Independently enforce key release and scoped recovery with separate credentials and approvals. |
| Audit/telemetry | Retain minimized events; protect recovery records from the recovering actor. |

This separation is a recommendation, not a selected deployment or a content-blindness claim.
Shared host/root access, cloud IAM, build/deployment authority, and secret stores can collapse
these boundaries. Record every actor who can decrypt or cause decryption. A self-hosted
operator with combined authority may subvert the data path. A compromised hub serving
JavaScript to a key-capable browser can steal plaintext or keys; separate API routes do not
solve that. Stronger claims require independently trusted client delivery and key-release
enforcement, with adversarial evidence.

## Key custody and enterprise recovery

Enterprise administrators must be able to recover content in their assigned scope. This
requires a decryption path beyond the originating user. Promise controlled, auditable, scoped
recovery only when verified; an absolute claim that nobody else can decrypt contradicts this
requirement. A relay can be blind to content only when custody, enrollment, clients, recovery,
and privileged operational access enforce that promise.

| Custody option | Consequence |
|---|---|
| User/device-only | Lost keys may mean lost content; alone it cannot meet enterprise recovery. |
| Customer-controlled scope keys/recovery | Recommended starting candidate; customer custodians, KMS/root administrators, and key-release service remain trusted. |
| Provider-operated custody | Makes the provider decryption-capable and changes the service, contractual, and incident boundary. |
| Independent quorum/threshold recovery | Can reduce unilateral misuse, but adds enrollment, availability, and restore complexity. |

No option is selected. Define the Personal recovery mode separately. Use reviewed envelope
encryption rather than inventing cryptography: bounded data keys, scope-bound wrapping,
authenticated tenant/scope/object/revision and key/policy epoch context, distinct device-signing
and content-encryption purposes, rotation, recovery copies, and destruction behavior.
Avoid globally exportable recovery material. One master key able to unwrap all scopes cannot
enforce assigned-scope recovery against its holder through a UI filter. Use
[NIST SP 800-57 Part 1 Rev. 5](https://csrc.nist.gov/pubs/sp/800/57/pt1/r5/final) for lifecycle
planning and obtain cryptographic design review before transfer ships.

A proposed recovery operation identifies the exact scope, purpose, destination, and expected
content. Independently check current identity, recovery assignment, and device trust with
step-up authentication. Apply a decided approval/quorum policy; a requester cannot assign
itself the needed scope. Issue short-lived bounded authorization and prefer controlled
rewrapping or approved-client delivery over exporting reusable root keys. Protected audit
records identify requester, approver, scope, policy version, reason, destination, and outcome;
notify the customer security owner and expire temporary rights. Test out-of-scope denial and
restore when the normal IdP, hub, or a custodian is unavailable. Emergency access needs a
declared governance path. Customer policy must distinguish enterprise recovery from entitlement
to personal/private content, employee departure, incident access, and legal retention.

## Transfer and stale state

Authenticate peers through a reviewed mechanism and independently authorize each operation:
the sender may disclose this object and the recipient may receive it in this destination.
Direct and hub routes enforce the same policy. Bind enrolled keys to verified identities,
approved installations, and assigned scopes; define removal and re-enrollment after compromise.

Authenticated envelopes need opaque identifiers, provenance/type, revision, and policy/key
epochs bound to their scope. Apply strict schemas, size/work bounds, version negotiation,
integrity checks, duplicate handling, and replay defenses. Preserve trusted checkpoints or
other rollback resistance where needed: a signature can authenticate an obsolete grant.
Less-trusted peers cannot resolve conflicts by broadening membership, access, or ownership.

Finite offline grants and online-only sensitive artifacts remain the selected shared model.
Durations, clock trust, key caching, and runtime enforcement are unresolved. Leases limit
conforming clients; malware can retain extracted keys or plaintext. Revocation prevents future
access/key release and governed use after policy refresh or expiry; it cannot recall past
copies. Rotate policy/key epochs after removal and consider old wrappers and retained data
keys when assessing exposure. Rewrapping alone does not invalidate retained old access.

Authenticated deletion history/checkpoints must prevent stale peers and backups from reviving
retired objects or membership. Define compaction lifetime and the supported offline horizon;
sufficiently stale peers need an authorized snapshot/rebootstrap that preserves pending local
work under its original restrictions. Distinguish active-store/index/cache deletion, managed
replica propagation, backup expiry, all relevant key/wrapper destruction, and unmanaged copies.
Restore tests must prove key availability and deletion/revocation handling together. Legal
retention, when applicable, needs separately restricted policy rather than an invisible bypass.

## Metadata and telemetry

Management metrics are intended future capability, not permission to collect arbitrary data.
Before collection or egress, approve a field allowlist for each purpose: delivery, customer
administration, security audit, billing if introduced, and product analytics. Specify recipients,
permissions, retention, region, exports, and vendor visibility. Prefer opaque identifiers and
aggregates. Raw content, prompts, tool arguments, repository paths, personal information,
credentials, and predictable-content hashes must not enter routine logs or analytics by default.
Routing, timing, sizes, titles, and filenames can disclose work patterns; aggregation alone
does not establish anonymity. Review proxy headers as well as application logs.

## Local interfaces and MCP

The bootstrap exposes only loopback status/static routes and rejects unsupported API/MCP
paths. Before artifact/admin operations exist, decide authentication, browser Origin/CSRF
protection, credential storage, file permissions, and interface exposure. Loopback alone is
insufficient protection from hostile websites or processes running with the same privileges.
Restrict filesystem, SQLite, and backup access; record the local encryption/key-storage
decision and the boundary for other users/processes. Attribute privileged and MCP operations
to their actual caller without logging content or secrets.
CLI import/export needs safe destination handling, bounded parsing, and interrupted-write
behavior; local encryption does not defend against an already compromised endpoint.

Pin implemented MCP transports, protocol/SDK versions, and tested clients. The referenced
[2026-07-28 Streamable HTTP revision](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http)
requires Origin and mirrored header/body validation and removes protocol-level sessions.
Do not make an old session ID an authorization boundary; review mirrored arguments for log
exposure. Local stdio protects the launching environment/process and scoped operations rather
than applying HTTP OAuth. Protected HTTP follows the
[authorization model](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization):
resource-specific tokens, issuer/audience/expiry checks, minimal scopes, reviewed PKCE,
redirect handling, discovery, and secure token storage. Incoming bearer tokens must not become
upstream integration credentials. Defend discovery/proxy flows against SSRF and confused deputies.
Every authenticated operation still checks Substrate tenant/object/action policy.

## Adversarial evidence before transfer

Exercise a relay that substitutes/replays envelopes, withholds deletes, or denies service;
a control plane that enrolls rogue recipients or rolls policy back; an administrator recovering
another scope or suppressing audit; an endpoint exporting plaintext or poisoning revisions;
and malicious context requesting downstream disclosure or permission changes. Also test
offline removal, stale reconnect, wrong key/context, interrupted rotation, and restored old
membership/deletion state. Document privileged-operator and endpoint limits. Design review
and deterministic negative tests, with separate model evaluations where useful, must support
the exact guarantees claimed. The [release gates](../plans/security.md#capability-gates)
apply before transfer carries customer or internal data.
