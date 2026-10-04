# Getting started

**Last reviewed:** 2026-10-04. Re-read whenever the CLI, HTTP contract, or visible UI changes.

## Prerequisites and build

Use Linux or WSL 2 with Go 1.27.1, Node.js 24.15.0, npm 11.12.1, and Make. In WSL, put the
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
it does not mean artifact storage or synchronization exists.

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
including untracked files; reads and writes reject checkout destinations and symlinks.

On the packaged browser page, enter the credential’s contents in Session credential to inspect
the current space and registered checkout. The page clears the credential after each request,
keeps no persistent browser credential storage, and offers Clear context to clear the display. The inspected context is a snapshot, not an ongoing
browser grant; further requests require the credential again.
Denial and unavailable-authority states disclose no restricted repository metadata. This
read-only view does not activate artifacts or approve policy changes.

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

No artifact, MCP, artifact database, pairing, or synchronization endpoint exists yet. Authority
records use bounded plaintext JSON in a 0700 directory, with 0600 state and credential files.
Updates lock across processes and replace synced files atomically; persistence failures return
errors, and oversized updates retain the previous usable authority. There is no at-rest
encryption or protection against the owning OS account, root, or an endpoint compromise. See the
[first release contract](product/first-release.md) for their required behavior and
[development guide](development.md) for checks that validate this checkout.
