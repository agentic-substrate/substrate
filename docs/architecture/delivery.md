# User interfaces and harness delivery

**Last reviewed:** 2026-10-04 · **Re-read cadence:** at each UI, harness, or packaging decision

**Status:** easy joins, inspectable administration, scoped harness access, and concrete human
publication review are agreed goals. CLI, MCP, and a browser administration interface are
selected for the first usable release. The browser uses Vite + React, with its static build
embedded in the Go executable. The local stdio MCP/Unix node mechanism is selected below; background service installation,
remote MCP, shared credentials, and concrete platform packaging remain open. The local owner/session credential
and browser inspection mechanism is selected in [onboarding](onboarding.md); inspection
displays an authenticated snapshot and does not persist browser authority. The bootstrap
uses TypeScript and npm,
with tool versions pinned in the repository. Linux
and WSL 2 are selected for the initial release; macOS and native Windows follow later.
These choices define the release direction. The executable provides trusted local setup, scoped artifact capture/inspection, owner-controlled
Git registration/approval, and approved snapshot reads. The browser serves status and authenticated
context inspection. Local CLI/MCP retrieval attaches to the foreground node; browser artifact
APIs and native artifact adapters remain unavailable.

## Interface recommendation

| Surface | Role | Main tradeoff |
|---|---|---|
| CLI | Setup, explicit node operation, headless administration, automation, and diagnostics. | Complex content/access review needs careful readable output. |
| MCP adapter | Scoped artifact retrieval, contributions, and proposals from existing harnesses. | A model-controlled tool call is not evidence of human publication review. |
| Local browser UI | Visible context, repository bindings, artifact inspection, and review. | Adds UI maintenance, accessibility, and browser-session requirements. |
| Terminal UI or desktop application | Potential later convenience for terminals, tray state, or notifications. | Additional interaction and platform lifecycle work. |

CLI, MCP, and a small browser administration interface are agreed for the first usable
release. Human and administrator workflows benefit from reviewing content, destination,
recipients, and effective policy together; agents primarily need stable structured operations.
Use one application command and policy layer behind all interfaces.

## Agreed Vite + React delivery

The selected browser stack fits the Go backend and single-executable distribution:

| Environment | Behavior |
|---|---|
| Development | Vite serves React with hot reload and proxies API requests to the Go node. |
| Release build | Build the frontend into static HTML, JavaScript, CSS, and other assets, then embed that output during Go compilation. |
| Installed application | Go serves the UI and API from the same origin; React runs in the browser. Users need no Node or Vite service. |
| CLI or MCP use | The Go application operates with the browser closed. |

This uses [Vite's static production build](https://vite.dev/guide/static-deploy.html),
[development proxy](https://vite.dev/config/server-options.html#server-proxy), and
[Go embedded assets](https://pkg.go.dev/embed). The production server will be Go;
Vite's development and preview servers are development tools.

Embed the UI initially: one release carries matching UI and backend assets,
and the local interface can load without internet. UI changes require rebuilding the complete
binary. Developers and release builds need the frontend toolchain as well as Go. Separate
static hosting can remain a later deployment arrangement if administrators need independent
frontend releases.

Build frontend assets before compiling Go, into a location the embedding package can include.
Keep browser routes separate from API and MCP paths; an unknown API route must return an API
error rather than the SPA entry page. Bundle required UI assets locally and handle stale pages
after upgrades. Keep credentials in runtime storage rather than the downloadable frontend.
React displays policy outcomes; the shared Go application layer authenticates the caller,
checks authority roles, and validates exact human approval.

The product screens will follow the first release contract as their underlying capabilities
are implemented. The current bootstrap toolchain is described in the [development guide](../development.md).
[Syncthing's interface](https://docs.syncthing.net/intro/gui.html) is a workflow precedent for
inspecting devices, selected shares, and independent sync states.

## Initial human surface

Keep the first personal interface focused on capabilities that actually exist:

- Current node, owner, space/repository binding, save destination, and placement.
- Artifact content, exact revision, applicability, source, and permitted actions.
- Pairing and publication review where implemented, with complete proposed content and destination.
- Retrieval/index state, deferred work, resource settings, and backup/recovery status.

Add people/groups, verified invitations, device approval, offboarding, and authority policy
administration with team sharing. Add organization-provider configuration with the agreed
later optional OIDC scope. Pending features should not appear as operative controls.

Local ownership does not grant organization administration. A Work replica shows accepted
policy and requests the viewer may submit; local proposals remain pending until authority
acceptance. Explain access using only metadata that viewer may see. Default local
administration to local access; remote administration needs authenticated deployment.

CLI review must satisfy the same human/content/version/destination contract as browser review.
Ordinary agent credentials may propose a publication, but cannot turn a tool argument into
proof of human review. Exact human-session validation is still an implementation decision.

## Initial harness connection

Recommend a small Go stdio bridge connecting each local harness to the trusted node.
Codex, Cursor Agent, Claude Code, and OpenCode support local MCP processes. Their individual
configuration entries reuse Substrate enrollment while each session receives a narrowed
approved repository/space binding. This inference needs supported-version integration tests;
native MCP approval, a working directory, or an environment variable cannot grant Substrate access.

Prefer explicit, observable node startup initially. Daemon installation and coordinated
on-demand startup are packaging/lifecycle options, not authorization mechanisms. Closing a
bridge ends its session without stopping a node owned by another lifecycle.

Direct authenticated HTTP MCP remains another deployment path, particularly for remote
execution environments. Stdio and HTTP have different transport/credential conventions,
with the same application checks. See [MCP transports](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports)
and [authorization](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization).

Before adding artifact operations, apply the [local-interface/MCP requirements](security.md#local-interfaces-and-mcp)
for browser-origin protection, transport-specific authentication, token scope, header validation,
and versioned client evidence. The local implementation pins the official Go SDK as described below; remote authorization
and untested client compatibility remain unverified.

Return explicit readiness, blocked, approval-required, and pending states. Headless workers
use their own provisioned service principals; they cannot wait indefinitely for interactive
approval. Native artifact rendering/installation delivers only the approved version to the
permitted profile and reports destination and activation state. Installed plaintext and
existing conversations still require the agreed runtime/isolation boundary.

See [installation and lifecycle research](packaging.md), [onboarding](onboarding.md), [artifact decisions](artifacts.md),
[repository bindings](repositories.md), and [deployment](deployment.md).

## Selected local MCP transport

The bridge uses the official `github.com/modelcontextprotocol/go-sdk` v1.8.0 stdio transport.
The pinned SDK negotiates its supported protocol version with each client; record the actual
initialized client/version/protocol rather than assuming the latest design reference was
negotiated. Each process fixes its private state directory, registered checkout, and private
credential-file path through trusted startup arguments. Client roots, tool arguments, source
instructions, and source aliases cannot enlarge that session. The bridge reads the credential
for every tool operation, and the node authenticates current authority before disclosure.

The four tools are `discovery`, `capture`, `search`, and `read`. Discovery lists up to 20
current permitted choices and scope-only index coverage. Capture records unverified memory
with provenance and a stable retry ID. Search uses lexical queries, exact identifiers, explicit
associations, and one-hop related results. Read requires exactly one current artifact ID,
revision ID, or registered qualified identity/source alias. Missing, stale, retired, or
inaccessible objects receive a generic denial; permitted ambiguity receives a conflict.
Search labels cannot change source approval resolution. There are no registration, approval,
publication, native-installation, or execution tools. A content read reports native activation
as unsupported. Tool errors preserve denial, conflict, pending coverage, and unavailable states.

Both stdio and private IPC bound frames to eight MiB and reject invalid raw UTF-8 or unpaired
Unicode escapes before decoding. Capture remains bounded to one MiB of text and 4096 bytes
of provenance. IPC connections use two-second connect and at most thirty-second operation
deadlines, and the runtime admits at most 32 simultaneous connections. A node missing or
stopped after bridge initialization remains an explicit tool error; no bridge owns its node's
lifetime. Private transport does not prove safety against the owner's OS account or root.
The foreground node contract is in [packaging](packaging.md#selected-foreground-node-and-local-command-access).

Actual client evidence belongs in the development guide with exact versions and limitations.
SDK-level integration checks alone do not advertise a harness as tested, and native feature
installation remains separate from MCP reads. Re-run client discovery, capture, search, reads,
denials, concurrent sessions, and stopped-node behavior after transport or client changes.
