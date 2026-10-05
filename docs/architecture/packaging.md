# Installation and node lifecycle

**Last reviewed:** 2026-10-05 · **Re-read cadence:** at each platform, install, or release decision

**Status:** CLI, MCP, and a Vite + React admin UI embedded in Go are agreed for the first
usable release. Initial platform coverage is Linux and WSL 2; macOS and native Windows follow
later. Foreground node operation and owner offline whole-installation recovery are implemented.
Service installation, native packaging, and release validation remain open.

## Agreed platform scope and packaging direction

Validate Linux and WSL 2 for the first usable release. Recommend shipping a downloadable
executable containing the UI, supporting foreground operation, and offering explicit opt-in
installation as a per-user service where supported. macOS and native Windows are deferred.
Do not claim untested builds are supported.
Keep containers an optional deployment path rather than a personal-installation dependency.

| Approach | Benefit | Material tradeoff |
|---|---|---|
| Linux/WSL 2 initially — selected | Smaller initial installation and lifecycle validation scope. | Defers native Mac and Windows adoption. |
| macOS initially | Native Mac adoption using the same core and UI. | Adds macOS installation, service, distribution, and lifecycle validation; deferred. |
| Native Windows initially | Earlier adoption outside WSL. | Adds another installer, service, credential, filesystem, and recovery environment; deferred. |
| Required containers | Fits operators who already manage a container runtime. | Adds runtime, port, persistent-volume, and process-lifecycle configuration to personal use. |

Go supports Linux, macOS, and Windows targets, but compiling the application is not evidence
that its complete operational lifecycle works. Driver and runtime dependency choices must
preserve the selected release contract. See [Go targets](https://go.dev/doc/install/source)
and [minimum requirements](https://go.dev/wiki/MinimumRequirements). The latter explicitly
excludes WSL 1. Native-platform service integration has different operational requirements:
[Apple service management](https://developer.apple.com/documentation/servicemanagement) and
[Windows services](https://learn.microsoft.com/en-us/windows/win32/services/about-services).

## Proposed personal lifecycle

First startup displays the node identity, resolved data directory, and local admin URL.
Opening a browser is optional. Initial owned Personal setup works without a shared service.
The same application commands should support foreground and managed-service operation.

One node process owns each installation's state directory. Concurrent CLI, browser, and
scoped MCP sessions attach to it; switching harnesses or worktrees does not initialize new
node identities. This is an application ownership rule, not a claim that SQLite permits
only one connection. A stdio bridge ending closes its session without stopping a node
whose lifetime is owned elsewhere. Coordinated automatic node startup can be considered
later; naive per-harness startup introduces ownership races.

Make service startup opt-in and provide observable start, stop, status, logs, and disable
operations. Preserve foreground operation when service management is unavailable. Service
installation does not enable network access, change organization roles, or prove human approval.
Background operation retains the [retrieval resource policy](retrieval.md#resource-aware-operation).
Idle scheduling alone is not an energy-use guarantee.

## WSL and coordinator availability

Microsoft documents systemd support on WSL 2, with version and configuration prerequisites,
and states that systemd services do not keep the WSL instance alive. A managed WSL service
therefore runs within the distro's actual lifecycle; it does not establish continuous
availability after Windows boots or guarantee synchronization while the distro is stopped.
See [WSL systemd](https://learn.microsoft.com/en-us/windows/wsl/systemd).

User-service startup also depends on the user manager. Lingering can retain it after logout,
but does not remove the enclosing WSL lifecycle boundary. Detect capabilities and report the
actual behavior rather than changing host lifecycle settings implicitly. See
[systemd user lingering](https://github.com/systemd/systemd/blob/main/man/loginctl.xml).

Initially validate harness, bridge, and node in the same Linux/WSL distribution. Keep its
database on that environment's own local filesystem. Give each node an unambiguous identity
and admin address; test Windows-browser access separately under supported networking modes.
See [WSL filesystems](https://learn.microsoft.com/en-us/windows/wsl/filesystems) and
[networking](https://learn.microsoft.com/en-us/windows/wsl/networking).

When a shared coordinator is introduced, recommend a service on an always-on Linux host
under a dedicated non-root identity, with explicit persistent storage and authenticated
HTTPS. A personal or WSL-hosted coordinator can still be used with its actual availability
limitations. Stops, sleep, and host shutdown use the agreed disconnected-operation contract.

## Updates and release evidence

Keep identity, configuration, SQLite data, attachments, and pending work independent of the
executable. Replacing or uninstalling the executable/service preserves data by default;
explicit data deletion is a separate operation. Host-level removal of a WSL distro can still
remove its files. Backup and recovery procedures must account for that boundary.

Recommend explicit versioned updates initially. Publish checksums and define release-origin
verification. Before schema migration, stop gracefully, take a coherent application backup,
migrate once, and report the result. An old executable does not automatically reverse a
schema migration. The selected local restore discards credentials; future shared restore
still requires current authority and revoked-grant revalidation.
The [SQLite backup API](https://sqlite.org/backup.html) supports consistent database snapshots;
the full backup contract also includes related files and identity.

Release support requires evidence for installation, concurrent harness sessions, duplicate
startup, shutdown/restart, data preservation, updates, and restore on the stated OS and
architecture combinations. Cross-compilation alone does not satisfy those checks. Exact
architectures, service packaging, credentials, release tooling, and update compatibility
contracts remain open implementation details.

See [delivery](delivery.md), [deployment](deployment.md), and [onboarding](onboarding.md).

## Selected foreground node and local command access

`substrate node` owns one private state directory through a lifetime `runtime.lock`, distinct
from the authority store's short transaction lock. It opens/migrates SQLite once and listens
on `node.sock`, mode 0600 inside the validated owner-only directory. Scoped CLI and stdio MCP
sessions attach to that node; they do not open their own database or start it implicitly.
Duplicate startup fails without removing the live socket. After abrupt shutdown, the next
exclusive lock holder may replace a private stale socket and let SQLite recover its journal.
Normal shutdown stops accepting, cancels the indexing worker and accepted IPC connections,
waits for bounded in-flight mutations, closes SQLite, removes only its
own socket identity, and finally releases the installation lock. Closing a bridge leaves the
node running. A shorter private directory is required when its Unix socket path exceeds
100 bytes; unavailability returns an actionable bounded error without remote fallback.

This replaces the initial direct scoped CLI persistence path. Trusted owner `source-register`,
`approve`, `restore-artifact`, `publication-policy`, `review-grant`, `review-revoke`,
and `backup`
remain offline commands: stop the node, perform the command under the
same exclusive installation lock, and restart. They fail while a node owns the directory.
Authority setup, scoped credential creation/revocation, and read-only browser context keep
using the separate authority lock; they do not open SQLite. Installation locks and file modes
protect against other OS users, not hostile processes using the owner's account or root.

Foreground operation is implemented on the tested Linux/WSL environment. Durable indexing
pause survives SIGINT/SIGTERM restart; explicit startup overrides retain the same identity
and state directory. The [dated foreground evidence](../development.md#foreground-maintenance-evidence)
includes a real WSL 2 network-namespace restart, duplicate-node rejection, exact receipt retry,
current read, lexical retrieval, and node CPU/RSS measurements. It does not test distro shutdown
or a host reboot. User-service installation,
on-demand startup coordination, native platform packaging,
and continuous WSL availability remain unimplemented. The browser `serve` command retains
its separate numeric-loopback listener and forwards credentialed artifact inspection and
publication review to the running node. It does not own SQLite or expose an HTTP MCP API.

## Selected local backup and restore

`backup -state-dir PRIVATE_DIR -out UNUSED_BACKUP_DIR` holds the exclusive installation
lock, then the authority transaction lock through authority serialization and SQLite's
incremental backup API. The separate authority lock matters because setup/session commands
can run while the node is stopped. The source is opened read-only without migration or empty
database creation; original schemas 1–5 remain unchanged in the snapshot. Missing, hot-journal,
oversized, incompatible, or corrupt state fails with an actionable error. Recover a journal
with a compatible node and stop it before retrying backup.

`restore -backup PRIVATE_BACKUP_DIR -state-dir UNUSED_DESTINATION` validates and copies
the same opened bytes into a private sibling stage. Supported migration runs only there,
then credentials are cleared and state is validated/synchronized before Linux no-replace
rename publishes the unused destination. A failed no-replace syscall has no unsafe rename
fallback. Existing installations are never restored in place. Runtime locks, authority locks,
sockets, external credential files, journals, and external Git are not backup payload.

The [security contract](security.md#selected-local-recovery) defines exact inventory/limits,
credential reset, stale checkout denial, stage cleanup, and the post-publication directory-sync
limit. [Local recovery evidence](../development.md#local-recovery-evidence) exercises the
packaged executable and restored/new captures across restart in a zero-route namespace.
It does not establish native platform packaging, service installation, host-reboot survival,
power-loss guarantees, encrypted custody, or shared restore authority.
