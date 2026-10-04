# Installation and node lifecycle

**Last reviewed:** 2026-10-04 · **Re-read cadence:** at each platform, install, or release decision

**Status:** CLI, MCP, and a Vite + React admin UI embedded in Go are agreed for the first
usable release. Initial platform coverage is Linux and WSL 2; macOS and native Windows follow
later. Concrete lifecycle, service, and release integrations remain open. No installation,
service integration, or release validation is implemented yet.

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
verification. Before schema migration, take a coherent application backup, stop gracefully,
migrate once, and report the result. An old executable does not automatically reverse a
schema migration. Restore revalidates shared authority and revoked grants as already agreed.
The [SQLite backup API](https://sqlite.org/backup.html) supports consistent database snapshots;
the full backup contract also includes related files and identity.

Release support requires evidence for installation, concurrent harness sessions, duplicate
startup, shutdown/restart, data preservation, updates, and restore on the stated OS and
architecture combinations. Cross-compilation alone does not satisfy those checks. Exact
architectures, service packaging, credentials, release tooling, and update compatibility
contracts remain open implementation details.

See [delivery](delivery.md), [deployment](deployment.md), and [onboarding](onboarding.md).

## Initial local command access

The current CLI opens the shared private authority directory and artifact database directly.
SQLite immediate transactions coordinate concurrent command processes and preserve atomic
receipts; the browser server still exposes no artifact API. This is the selected initial
local persistence path, preceding the proposed node-attachment/service model above. It does
not establish singleton node startup, service integration, complete backups, or migration
recovery. Keep data on the local Linux/WSL filesystem and re-evaluate this access path when
introducing indexing workers, MCP, background services, or recovery.
