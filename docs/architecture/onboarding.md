# Joining and administration

**Last reviewed:** 2026-10-04 · **Re-read cadence:** at each identity, enrollment, or delivery decision

**Status:** easy user joins and administration are major product goals. Repository discovery
manifests and reusable approved bindings are agreed. Local owner setup and device pairing
come first, administrator-verified invitations serve early teams, and optional organization
OIDC follows with organization features. Local owner and scoped session setup are implemented through the trusted CLI. Device pairing,
invitations, organization authentication, recovery mechanisms, and shared offline grant
durations remain unimplemented and their concrete credentials remain open.

## Selected local owner and session mechanism

The OS account initializes one opaque owner ID with a display name and an initial Personal
space. Explicit trusted commands create further spaces and checkout registrations. These
commands rely on OS filesystem ownership; session credentials cannot invoke administration
over HTTP. A profile/session credential has 256 random bits, is stored outside Git in a new
0600 file, and is represented only by its SHA-256 digest in private authority state. It binds
exactly one space/repository/checkout and expires 24 hours after issuance. Revocation removes
its grant, and subsequent requests re-read current state. This local expiry is independent
of future network membership or shared offline leases.

Authority uses bounded plaintext JSON with Linux file locking, synced atomic replacement,
0700 directories, and 0600 files. It provides no at-rest encryption and trusts the owning OS
account and filesystem administrators. Unsafe permissions, symlinks, corrupt/unavailable
state, changed Git identity, and ambiguous context fail closed. Starting the anonymous
bootstrap page does not silently enroll an owner. Pairing, automated moves, token refresh,
and remote/local-browser administration are future work.

## Required product outcomes

Personal users can start locally without an external service, add installations without
rebuilding every harness configuration, and understand where their context will be stored.
Team and organization administrators can manage joins, access, departures, and replacement
devices without treating each checkout as a fresh account setup.

Keep people, installations, repository associations, and harness sessions distinct in the
records while combining related setup steps in one understandable flow. Membership in a
space does not automatically approve every device or replica. Recognizing a repository
manifest does not grant membership. Routine reuse of an accepted binding should not repeat
enrollment or ask for wider grants.

## Agreed enrollment sequence

Start with explicit local owner setup and device pairing. Early teams use administrator-verified
named invitations with approved access; their first release need not include organization SSO.
Preserve an identity boundary for later optional OpenID Connect (OIDC) sign-in through an
organization's provider. No external provider is required for personal use. This sequence
does not select a password system or a credential protocol.

| Approach | Benefit | Cost or limitation |
|---|---|---|
| Local owner, pairing, approved invitations | Supports standalone use and personal multi-device setup with no identity-provider prerequisite. | Substrate needs recovery, revocation, and understandable membership administration. |
| OIDC for shared joining | Reuses an organization's existing authentication and familiar sign-in. | Adds integration and provider availability requirements; still needs Substrate membership and device policy. |
| Invitations/API tokens as the entire account model | Quick initial access and automation. | Credential possession alone does not provide durable person/device ownership or clear offboarding. |

Invitations can approve a selected membership, groups/actions, and first-device enrollment
together where policy permits. A short-lived invitation is consumed into durable records;
revoking an unused invitation is distinct from removing a person who already joined. The
authority must establish the intended person's identity and the installation's credential
possession through a defined acceptance flow. Raw bearer secrets stay outside Git.

Possession of an invitation, a declared name, or an email address does not authenticate the
intended person. A small trusted-team flow can require an authenticated administrator to
confirm acceptance through an existing trusted channel, binding the intended person and
requesting installation before approval. Organization sign-in can instead authenticate the
person through an approved provider; membership, device approval, and placement remain
separate decisions. The exact verification ceremony is not selected.

OIDC standardizes end-user authentication and identity claims; it does not itself define
Substrate group membership, device approval, or replication eligibility. Keep internal
principal IDs independent of the provider, mapping external identities by issuer and subject
rather than email alone. See [OIDC Core](https://openid.net/specs/openid-connect-core-1_0.html)
and its [identity stability requirements](https://openid.net/specs/openid-connect-core-1_0.html#ClaimStability).

Provider identities need explicit verified linking to existing records; matching emails must
not merge accounts or grants automatically. When an organization requires provider sign-in,
Personal pairing and recovery cannot bypass that organization's authentication or enrollment
policy. Personal operation remains independent of the organization's provider.

## Enrollment journeys

| Situation | Desired flow |
|---|---|
| First local installation | Explicitly initialize a local owner and Personal space, bind the repository, and use local context without a remote account provider. |
| Same person, another laptop or WSL installation | Request pairing, confirm on an already trusted installation, and enroll independent device credentials with permitted spaces and placement. |
| New teammate | Accept a named invitation, verify the authority, establish identity, and enroll the first device in the same flow when policy permits. |
| Existing authorized node opens another checkout | Discover the manifest and reuse a validated binding and current grants; request only missing access. |
| Lost or replacement device | Recover through a surviving trusted installation, recovery material, or authorized administrator; revoke the old device without deleting the person's other memberships. |
| Coordinator unavailable | Continue permitted local work; retain shared join and approval requests as pending until authority validation is possible. |

Personal pairing can be self-approved through an existing trusted installation. Work spaces
may permit that flow or require administrator device approval. An explicit coordinator setup
assigns its first administrator; a remote request cannot claim initial ownership merely by
arriving first. Repository paths and credentials remain installation-specific across WSL
distributions and networked machines.

Existing products provide useful precedents for mutual device pairing and optional provider
sign-in: [Syncthing pairing](https://docs.syncthing.net/intro/getting-started.html) and
[Headscale registration](https://headscale.net/stable/ref/registration/). These are workflow
examples, not networking dependencies. OAuth's [device authorization grant](https://www.rfc-editor.org/rfc/rfc8628)
provides a browser-confirmation pattern for headless sign-in; it is not independently a
person-verification method or Substrate's selected device-pairing protocol.

## Proposed administration and harness delivery

Provide an inspectable roster of people, groups, owned installations, pending invitations,
last contact, repository bindings, and effective access. Apply group and placement policy
across repositories; do not require a separate human decision for every worktree. People and
devices can be revoked separately. Recovery must preserve the logical person's identity and
must not revive revoked grants from a restored backup.

Enterprise content recovery is a distinct assigned-scope authority, not an automatic consequence
of dashboard administration, device approval, or account recovery. Custody and independent
approval/key-release enforcement remain open. Recovery must be attributable and auditable,
with denial outside the assigned scope. See the [recovery contract](security.md#key-custody-and-enterprise-recovery).

A harness launch selects an approved binding and receives narrower session access. Reuse
node enrollment across harnesses so switching tools does not require another membership join.
A local Go stdio adapter backed by the trusted node is a candidate for initial delivery;
remote HTTP MCP would use its standard authorization flow. Stdio and HTTP have different
credential conventions, but application permission checks apply to both. See [MCP authorization](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization).

Unattended workers need owned service principals and scoped credentials rather than copied
human enrollment secrets. Their transport and lifecycle are separate implementation decisions.
The one-node owner/session credential storage and read-only browser inspection are selected
above. MCP connection, remote administration, shared credentials, and recovery procedures
still require contracts before implementation.
The [delivery research](delivery.md) records agreed CLI/MCP access and a small Vite + React
browser administration interface embedded in Go for the first usable release. Exact screens,
node lifecycle, and concrete platform packaging remain pending. Initial platforms are Linux
and WSL 2; macOS and native Windows follow later. See [installation](packaging.md).

## Agreed SSO scope

The first team release can use administrator-verified invitations. Add optional OIDC with
organization features, allowing an organization to require its approved provider for its
spaces while Personal remains independent. This sequence prioritizes the agreed initial
personal deployment without requiring a provider for every shared space. SSO alone does not
provide automatic departure synchronization; explicit administration or a separate
identity-lifecycle integration remains necessary.

First-time shared joining requires authority verification. Existing nodes follow the agreed
[bounded offline authorization model](offline.md); grant durations and enforcement remain open.
See [deployment](deployment.md), [repository bindings](repositories.md), and [sharing](sharing.md).
