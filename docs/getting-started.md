# Getting started

**Last reviewed:** 2026-10-04. Re-read whenever the CLI, HTTP contract, or visible UI changes.

## Prerequisites and build

Use Linux or WSL 2 with Go 1.27.1, Node.js 24.15.0, npm 11.12.1, Git, and Make. In WSL, put the
checkout in the Linux filesystem. Node.js is needed to build the web assets and documentation;
the resulting executable contains its UI and does not need Node.js or a model runtime.

```sh
npm ci
make build
./bin/substrate serve
```

Open <http://127.0.0.1:9842>. The page requests the local status endpoint and distinguishes
loading, connection success, and connection failure. The Refresh status button retries the
request and works by keyboard. Its successful state means the bootstrap endpoint is reachable;
it does not prove that the CLI artifact store is ready or that synchronization exists.

## Commands and configuration

`substrate serve` runs in the foreground. Its default listener is `127.0.0.1:9842`. Change it
with `-listen`, for example `./bin/substrate serve -listen 127.0.0.1:9843`. Only numeric IPv4
or IPv6 loopback addresses are accepted. The browser context endpoint requires a scoped
session credential; remote deployment remains unsupported. Stop it with Ctrl+C or SIGTERM; shutdown waits up to five seconds
for active requests.

`substrate version` prints the bootstrap version. Unsupported commands and invalid flags
return a nonzero exit code. This checkout does not install services. Explicit owner setup
creates a private local authority directory; merely starting the status page does not.

## Trusted local setup

Use the trusted CLI under the operating-system account that will own this installation. The
default state directory is `$XDG_CONFIG_HOME/substrate`, or `~/.config/substrate` when XDG
configuration is unset. `-state-dir` selects another owner-only directory outside all Git
checkouts. Keep that selection consistent across setup, inspection, and server commands.
The following example explicitly creates Work alongside the initial Personal space:

```sh
./bin/substrate init -owner "Local owner"
./bin/substrate space -name Work
./bin/substrate register -path "$HOME/repos/project" -space Work
./bin/substrate session -path "$HOME/repos/project" -space Work -out "$HOME/.config/substrate/project-session"
./bin/substrate context -path "$HOME/repos/project" -credential "$HOME/.config/substrate/project-session"
./bin/substrate bindings
./bin/substrate serve
```

A session is a profile for exactly one space, logical repository, and checkout. Its bearer
credential expires after 24 hours and cannot register repositories, create spaces, or change
owner policy. This local credential lifetime does not select future shared offline grant
durations. `substrate revoke -credential <file>` revokes future use immediately; delete the
credential file separately. None of these commands retracts content already received by a
harness. Any process with the owner’s filesystem access can read credentials or change policy.

Register each sibling worktree explicitly. Native Git common-directory metadata lets those
registrations reuse one logical repository in the same space. A clone needs a new registration,
and an independently rooted nested repository needs its own binding. A subdirectory in the
same checkout resolves to that registered root. Directory names such as Work or Personal never
select policy, and a parent folder containing several projects is not an implicit workspace.
A session cannot switch to a different checkout through `-path`; issue a separate credential.
A changed Git identity fails closed and requires a newly trusted registration location.

`substrate discover -path <checkout>` reads an optional `.substrate.json` file of at most 8192
bytes. Its only fields are `schema_version` (1), `repo_id`, and `space_id`, and its output marks
them as untrusted hints. Use IDs from trusted `register` or `bindings` output if adding that
nonsecret file. Discovery neither registers the checkout nor issues a credential. Copied or
edited manifests never replace accepted bindings. Keep credentials outside every checkout,
including untracked files. Reads, writes, and ongoing authority access inspect canonical
ancestor directories and reject any `.git` marker, including malformed metadata. Missing Git
or inaccessible placement cannot prove a safe destination and fails before setup writes.
Credential files and the authority directory also reject symlinks.

On the packaged browser page, enter the credential’s contents in Session credential to inspect
the current space and registered checkout. The page clears the credential after each request,
keeps no persistent browser credential storage, and offers Clear context to clear the display. The inspected context is a snapshot, not an ongoing
browser grant; further requests require the credential again.
Denial and unavailable-authority states disclose no restricted repository metadata. This
read-only view does not activate artifacts or approve policy changes.

## Local artifact capture and inspection

With a trusted session credential, `capture` saves one memory observation from standard input.
It defaults to that session's space and repository. Keep the same `-state-dir`, `-path`, and
`-credential` selections as trusted setup. For example:

```sh
printf '%s\n' 'Use the documented build command.' | ./bin/substrate capture -path "$HOME/repos/project" -credential "$HOME/.config/substrate/project-session" -operation observation-1 -provenance 'Local synthetic observation'
./bin/substrate artifact -path "$HOME/repos/project" -credential "$HOME/.config/substrate/project-session" -id ARTIFACT_ID
./bin/substrate pending -path "$HOME/repos/project" -credential "$HOME/.config/substrate/project-session"
```

Replace `ARTIFACT_ID` with the saved receipt's `artifact_id`; quote it as an ordinary argument.
Capture returns a JSON receipt only after SQLite commits the observation, immutable revision,
provenance, retry identity, and pending work together. `pending-local` means saved locally,
unverified, and awaiting future indexing; it does not mean indexed, synchronized, or verified.
The store supports observation content up to 1 MiB and provenance claims up to 4096 bytes.
Operation IDs contain 1–128 bytes and must not have surrounding whitespace or control
separators. An exact retry with the same operation ID returns the original receipt, including
across process restarts. Changed content or metadata with that ID is rejected; choose a new
operation ID for a new contribution. Output failure can lose the displayed receipt after a
successful commit, so retry with the original input and operation ID.

To propose an edit, pass `capture -artifact <artifact-id> -expected <revision-id>` with a new
operation ID and content. An edit against the current memory revision advances the local head.
A stale edit returns `conflict`, preserves its candidate, and keeps the current head. Inspect
all authorized revisions with `artifact`. Resolve by submitting new content against the current
head; old candidates remain in history. This does not approve shared state or verify facts.
`retire -id <artifact-id> -expected <revision-id> -operation <operation-id>` marks an artifact
retired. A stale expected revision blocks retirement. Later edits return `conflict-retired`
and cannot restore it. The receipt records the original contribution outcome; it is not a
current lifecycle or delivery grant. Re-inspect before using saved content.

Every operation rechecks the session credential and registered Git checkout. Missing and
inaccessible object IDs share a generic denial, and failure returns no success JSON. Git-backed
skill and agent-definition storage retains committed source bytes and provenance as unapproved
candidates through its internal API. Source registration and approved-version selection, normal
recall, MCP, cross-space publication, indexing consumption, and backup/restore are separate work.
The browser remains a status and read-only context view, with no artifact editing endpoint.

## HTTP contract

`GET /api/status` returns JSON with `stage` equal to `bootstrap`, with caching disabled.
The UI uses this value to identify the available foundation. Anonymous status contains no
owner, space, repository, or credential information. `GET /api/context` requires one bearer
credential, the exact numeric-loopback listening Host and port, and, when supplied, the
matching HTTP Origin. Cross-site browser requests are denied without CORS grants or cookies.
Native CLI requests may omit Origin but still need the credential. An unavailable or corrupt
authority returns 503; untrusted or expired credentials return 403; missing credentials return
401. Credential checks revalidate the registered Git binding on each request. Unknown API and MCP routes return
404, including paths with encoded separators. Files under `/assets/` must exist; a missing asset returns 404 rather than HTML.
Client navigation without a file extension falls back to the embedded page. Unsupported
methods on the page routes return 405. The server applies a restrictive content policy and
does not load external scripts or fonts.

No artifact, MCP, pairing, or synchronization HTTP endpoint exists yet. Artifact CLI commands
use plaintext `artifacts.db` beside bounded plaintext JSON authority records in a 0700 directory,
with 0600 database, state, and credential files. Keep this directory on the Linux/WSL local
filesystem. SQLite uses rollback journaling with EXTRA synchronization and checks schema version
before access; unsafe file modes, symlinks, and unknown newer schemas fail closed.
Authority updates lock across processes and replace synced files atomically; persistence
failures return errors, and oversized updates retain the previous usable authority. There is no at-rest
encryption or protection against the owning OS account, root, or an endpoint compromise. See the
[first release contract](product/first-release.md) for their required behavior and
[development guide](development.md) for checks that validate this checkout.
