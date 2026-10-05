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
the current space and registered checkout. The page clears the input after inspection and
holds the credential only in page memory for subsequent artifact requests, each of which
rechecks current authority. Clear context removes the credential, displayed artifacts,
proposals, and review state. No cookie, URL, or persistent browser storage holds credentials.
Denial and unavailable-authority states disclose no restricted repository metadata. This
inspection does not activate artifacts or approve policy changes. The artifact browser lists
up to 100 current-scope artifact summaries and 100 source-scoped proposals; the scoped CLI
can inspect additional known IDs. It displays candidate/history states, complete declared
Git dependencies, and backend explanations of permitted actions. A displayed snapshot or
historical receipt never grants a later action.

## Local artifact capture and inspection

Start `./bin/substrate node` in a separate terminal, using the same `-state-dir` as setup.
The node runs in the foreground; stop it with Ctrl+C or SIGTERM. Scoped artifact commands
require this explicit running node and never create a database fallback.

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
unverified evidence; it does not mean indexed, synchronized, or verified. Search separately
reports current index coverage.
Artifact content and all contribution/lookup/retirement metadata must be valid UTF-8.
Invalid byte sequences are rejected before fingerprinting or persistence; they are never
converted to replacement characters. A valid U+FFFD character is ordinary supported text.
The store supports observation content up to 1 MiB and provenance claims up to 4096 bytes.
Operation IDs contain 1–128 bytes and must not have surrounding whitespace or control
separators. An exact retry with the same operation ID returns the original receipt, including
across process restarts. Changed content or metadata with that ID is rejected; choose a new
operation ID for a new contribution. Output failure can lose the displayed receipt after a
successful commit, so retry with the original input and operation ID.

To propose an edit, pass `capture -artifact <artifact-id> -expected <revision-id>` with a new
operation ID and content. An edit against the current memory revision advances the local head.
A stale edit returns `conflict`, preserves its candidate, and keeps the current head. Inspect
all authorized revisions with `artifact`, including each candidate's base and the accepted head.
Use `resolve-conflict -id <artifact-id> -revision <revision-id> -expected <current-head-id>
-operation <operation-id>` to keep the accepted memory or choose an exact existing revision.
Resolution creates a fresh unverified, pending-local revision and marks existing competing
candidates resolved without deleting their history. An outdated expected head blocks resolution.
To combine observations, submit the reviewed text through `capture` against the current head;
Substrate never merges prose automatically. Neither action approves shared state or verifies facts.
`retire -id <artifact-id> -expected <revision-id> -operation <operation-id>` marks an artifact
retired. A stale expected revision blocks retirement. Later edits return `conflict-retired`
and cannot restore it. The receipt records the original contribution outcome; it is not a
current lifecycle or delivery grant. Re-inspect before using saved content.

Stop the node and use the trusted owner command `restore-artifact -artifact <artifact-id>
-expected <retired-head-id> -operation <operation-id>` to restore a retired artifact in its
registered repository. It accepts no session credential and is unavailable through the node,
browser, or MCP. Restoring memory creates a fresh unverified local head, so edits against the
pre-retirement revision remain conflicts. Restoring an approved Git source creates a fresh
candidate with no effective head; review and approve that exact revision again. All prior source
candidates remain inspectable as `retired-candidate` and cannot be approved after restoration.
If the source never had an approved head, restoration returns `restored-awaiting-candidate`
with an empty revision ID; make a fresh proposal before approval. Restore and resolution retries
return their original receipts without undoing later changes, and changed payloads require a new
operation ID. Restart the node before scoped reads or contributions.

Every operation rechecks the session credential and registered Git checkout. Missing and
inaccessible object IDs share a generic denial, and failure returns no success JSON. Git-backed
skills and agent definitions use the proposal and owner approval flow below. Git source text
must also be valid UTF-8. Each main or dependency path must name one literal regular file and
match the returned Git entry exactly; directories and `.` are rejected, while literal wildcard
characters, tabs, and trailing spaces are preserved. Reads use locally available objects only:
a missing promisor object remains unavailable without remote-helper execution or network access,
even when repository configuration allows a transport. Materialize required objects separately
through your trusted Git workflow. Lexical recall and local stdio MCP use the foreground node.
Local reviewed publication is described below. Whole-installation backup/restore remains
unavailable; explicit single-artifact restoration does not recover an installation.

## Git candidates and approved content

`propose` captures a skill or agent-definition snapshot from an exact full Git commit in the
session's registered checkout. It reads regular raw Git blobs; branch names, unsafe paths,
symlinks, and missing objects fail. Source access stays local and does not fetch remote objects.
Content and all source metadata must be UTF-8 text. Use `-dependency` once for each declared
script or reference file in the same commit, up to 32 distinct dependencies and one MiB combined
with the main document. No references are followed automatically, and no scripts are executed.
The owner must review the complete declared inventory before approving it.

```sh
./bin/substrate propose -path "$HOME/repos/project" -credential "$HOME/.config/substrate/project-session" -operation skill-1 -kind skill -commit FULL_COMMIT_ID -file skills/build/SKILL.md -dependency skills/build/reference.md
./bin/substrate artifact -path "$HOME/repos/project" -credential "$HOME/.config/substrate/project-session" -id ARTIFACT_ID
# Stop the node before these trusted owner review commands.
./bin/substrate source-register -path "$HOME/repos/project" -artifact ARTIFACT_ID -source repository -name build -alias build
./bin/substrate approve -path "$HOME/repos/project" -artifact ARTIFACT_ID -revision REVISION_ID -operation approve-skill-1
# Restart ./bin/substrate node before scoped reads.
./bin/substrate choices -path "$HOME/repos/project" -credential "$HOME/.config/substrate/project-session" -selector build
./bin/substrate read -path "$HOME/repos/project" -credential "$HOME/.config/substrate/project-session" -selector QUALIFIED_ID
```

Replace placeholders with the exact full commit, receipt artifact/revision IDs, and registration's
`qualified` identity. Keep any explicit `-state-dir` flag consistent throughout. Namespaces,
names, and optional aliases use 1–80 lowercase letters, digits, dots, underscores, or hyphens;
`.` and `..` alone are invalid. Registration binds the artifact permanently to its qualified
space/repository/kind/source/name identity. It neither grants access nor widens applicability.
Only trusted local owner commands register and approve; they accept no session credential and
are unavailable through browser or MCP interfaces. This assumes a trusted owning OS account:
a hostile process with that account's filesystem access can invoke the owner CLI.

A proposed source begins as a candidate. First approval expects an empty selected head; later
approval requires `-expected <current-revision-id>` and a candidate captured against that same
head using `propose -artifact <artifact-id> -expected <current-revision-id>`. Competing candidates
remain conflicts. Resolve with a new proposal based on the current head and explicit approval,
or review an existing candidate and use `approve -resolve-conflict -artifact <artifact-id>
-revision <candidate-id> -expected <current-head-id> -operation <operation-id>`. The latter
approves a fresh exact snapshot against the current head; it rechecks any declared override
and its current default revision. Scoped sessions cannot perform this owner review. Combined
Git text or changed dependencies require a fresh proposal and approval; no input approval
automatically covers the combined result.
Retirement continues to block delivery and cannot be undone by approval of old candidates.
`artifact` inspects authorized history and provenance; `choices` with no selector lists registered
sources with candidate, effective, overridden, conflict, or retired state, the overridable-default
flag, and the explicit override artifact/revision pins. These commands disclose
only the session's own space and repository.

An exact qualified read preserves each permitted approved choice. Equal names from different
sources do not establish precedence. If several approved sources share an alias, reading that
alias fails with a conflict; inspect alternatives and select a qualified identity or explicitly
retire an unwanted choice. Source order, time, and relevance never choose the winner. Unauthorized
sources do not affect conflict state or explanations. Missing and inaccessible reads share a
generic denial; storage or authority failures are unavailable and produce no success JSON.

To permit specialization, register a default with `source-register -overridable`. Register its
specialization separately with the same alias, then approve it with `-overrides <default-artifact-id>`
and `-override-revision <default-head-id>`. The default must be approved, active, the same kind,
and in the same authorized scope. Only one-level relationships are supported; chains and cycles
are rejected. Multiple incomparable approved specializations block alias delivery. Advancing or
retiring the pinned default invalidates its specialization and blocks that alias until resolved.
An active default remains accessible by its exact qualified identity. New specialization approval
must pin the current default and use a fresh candidate based on its own current head.

Approved reads return stored main and dependency bytes with exact commit/path/blob provenance,
even after the source branch advances or files disappear. Source or dependency changes create
new unapproved candidates and cannot silently replace the approved snapshot. The response marks
`native_activation` as `unsupported`; content read is not native installation, executable safety,
activation, or tested harness compatibility. Export still needs its independent policy and
exact derived-memory publication review.

## Local publication review

Publication preserves the restricted source and creates a separate unverified memory in a
registered Personal repository. It never copies executable bundles, source IDs, paths, names,
or private source provenance into the recipient record. Review the actual proposed text for
restricted details; the application does not automatically detect secrets or undeclared sources.

Register the Personal checkout once and use trusted `bindings` output to identify its space
and repository IDs. Stop the node before owner policy commands. Both source export and
destination publication-write default to denied:

```sh
./bin/substrate register -path "$HOME/repos/personal-project" -space Personal
./bin/substrate publication-policy -path "$HOME/repos/project" -action export -decision allow
./bin/substrate publication-policy -path "$HOME/repos/personal-project" -action publish -decision allow
./bin/substrate bindings
```

An additional source restriction uses `publication-policy -path <Work checkout>` with
`-artifact <source ID> -action export -decision deny`. Changing the artifact decision to allow cannot
relax a denied space policy. These trusted commands manage only this one-owner local policy;
they do not implement organization administration or authorize a network export.

Restart the node and browser server. Bind the Work session, inspect a current artifact, enter
the complete generalized lesson and requested Personal destination IDs, and save the proposal.
The draft remains in its source scope, pending exact review. Stop the node and issue a new
private credential for the displayed proposal ID/revision and registered destination:

```sh
./bin/substrate review-grant -path "$HOME/repos/project" -proposal PROPOSAL_ID -revision PROPOSAL_REVISION -destination-path "$HOME/repos/personal-project" -out "$HOME/.config/substrate/review-credential"
```

Restart the node, enter that file's contents in the separate Review credential field, inspect
the exact text, destination, source/dependency inventory, audience, placement, and policy,
then confirm and publish. The credential expires after fifteen minutes and approves only that
snapshot. Scoped session credentials and approval arguments cannot satisfy review. Changed
content, destination, effective source/override relationship, or policy invalidates stale review;
mandatory denial cannot be approved away. A new proposal revision needs a new credential.
`review-revoke -path <Work checkout> -review-credential <file>` revokes a grant while the node
is stopped; remove the credential file separately.

Exact retries return their original acknowledgement without new writes. Historical proposal
or publication receipts do not establish current permission; expired/revoked review credentials
cannot recover a receipt. Publication commit failure returns no success. Serialized review
inventories exceeding seven MiB are rejected before credential issuance; reduce source inventory
or content. Imported reference text is never followed or executed.

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

`GET /api/artifacts` lists the bounded scoped inventory; `GET /api/artifacts/{id}` inspects
content/history/actions. Scoped `POST /api/publications` proposes a derived lesson and
`GET /api/publications/{id}` inspects its current revision. Dedicated-grant
`GET /api/publication-review` loads the exact review and `POST /api/publication-review`
publishes its revision/snapshot with a stable operation ID. Protected routes forward to the
running node and never open a browser database fallback. Browser writes require the exact
Origin; all protected routes check Host/port, Fetch-Site, and one bearer credential, and
return JSON error codes with 400 invalid, 403 denied, 409 conflict, or 503 unavailable.
Mutation bodies allow one strict UTF-8 JSON object of at most two MiB with supported fields.
Ordinary missing/inaccessible object IDs share a generic denial. No HTTP MCP, pairing, or
synchronization endpoint exists. Artifact CLI commands
use plaintext `artifacts.db` beside bounded plaintext JSON authority records in a 0700 directory,
with 0600 database, state, and credential files. Keep this directory on the Linux/WSL local
filesystem. SQLite uses rollback journaling with EXTRA synchronization and checks schema version
before access; unsafe file modes, symlinks, and unknown newer schemas fail closed. SQLite
journals inherit the private database mode without changing the process umask. A supported
interrupted-process hot journal can recover earlier acknowledged state on reopen; this is
not a device-failure, backup, or replica-recovery guarantee.
Authority updates lock across processes and replace synced files atomically; persistence
failures return errors, and oversized updates retain the previous usable authority. There is no at-rest
encryption or protection against the owning OS account, root, or an endpoint compromise. See the
[first release contract](product/first-release.md) for their required behavior and
[development guide](development.md) for checks that validate this checkout.

## Scoped search and current reads

With the node running, use `search -query 'build frontend' -limit 20` with the same state,
checkout, and credential flags as capture. Empty queries list current permitted artifacts. The CLI applies the same query, result-limit,
and related-origin checks as the MCP search tool.
Exact artifact/revision IDs and explicitly recorded labels can find current content while
lexical indexing is pending. Ordinary terms are lowercase lexical matches; a final `*` permits
prefixes of at least three characters. Unrecorded paraphrases and typo correction are not
implemented. Results identify the current revision, unverified/approved state, lexical score,
excerpt, and permitted associations; scores never confer approval or permission.

Capture/propose accept repeated `-identifier`, `-search-alias`, `-topic`, and `-related` flags.
Each field allows up to 32 distinct UTF-8 labels of 256 bytes. Relationships name existing
artifacts in the current scope. `search -related-to ARTIFACT_ID` returns one-hop current
permitted endpoints, optionally narrowed with `-query`. A missing or inaccessible origin is
denied without identifying another scope's content. Search aliases do not change registered
source aliases. Read result IDs with `read -id ARTIFACT_ID` or `read -revision REVISION_ID`;
`read -selector` remains the registered qualified/source-alias channel. Supply exactly one
selector. Superseded, retired, candidate, inaccessible, and missing current revisions cannot
be read through retrieval; authorized historical inspection uses `artifact` separately.

The foreground node runs one indexing worker. Successful artifact mutations and startup wake
incremental work after a short coalescing delay; searches, reads, review, and status requests
never schedule indexing. Each artifact replaces postings and its revision checkpoint atomically,
with at most 32,768 token positions. Repeated edits coalesce to the newest committed head, and
history-only candidates with an unchanged checkpoint do not rewrite postings. Complete content
and the separate pending operation ledger remain durable and readable while indexing is paused.

Use the same `-state-dir` for the node and these trusted local owner commands:

```sh
substrate index -state-dir PRIVATE_DIR -status
substrate index -state-dir PRIVATE_DIR -pause=true
substrate index -state-dir PRIVATE_DIR -pause=false
substrate index -state-dir PRIVATE_DIR -rebuild
substrate index -state-dir PRIVATE_DIR -run
substrate index -state-dir PRIVATE_DIR -retry
```

Pause is installation-wide and durable across shutdown and restart. Omitted `node
-index-paused` preserves it; explicit `-index-paused=true` or `-index-paused=false` overrides
it. `index` without a control and `index -pause=false` resume and synchronously attempt at most
100 incremental artifacts before responding, including failed attempts in that bound.
Background incremental work can continue afterward. `-status` reads without changing pause,
clearing failures, or scheduling work. The pause, status, rebuild, run, and retry controls are
mutually exclusive. `-rebuild` queues discretionary bulk work without running it; bulk work
survives restart and only `-run` attempts up to 100 jobs, including bulk, while unpaused.
A full rebuild regenerates postings even when the revision checkpoint matches. Remaining bulk
work requires another explicit run. A failed artifact remains failed through restart and later
edits; `-retry` explicitly clears failures and wakes permitted incremental work. Operational
failures return actionable unavailable errors; per-artifact failures use generic maintenance
status without private database diagnostics. Cancellation preserves unfinished queued work.

CLI maintenance counts cover the installation. The existing browser artifact inventory adds
read-only maintenance and lexical coverage for the full authenticated owner, space, and
repository, independently of its 100-record list limit. `queued` counts healthy incremental
jobs even while paused; `deferred` counts healthy discretionary bulk jobs even while paused;
`failed` counts failed jobs instead of including them in either other count. Coarse queue
state prioritizes failed, then pending incremental, then deferred bulk, then ready. All three
counts and the installation pause remain visible. A ready queue can have empty eligible
content or limited lexical coverage and does not prove complete search recall. Search's
`index` object and browser coverage count only current eligible scoped revisions, with
`limited` reporting truncation. Browser inspection cannot resume indexing or clear failures.

Supported foreground shutdown handles SIGINT and SIGTERM, cancels index transactions and
incomplete accepted IPC frames, and releases only its own socket and installation lock.
The [development evidence](development.md#foreground-maintenance-evidence) records the tested
Linux/WSL environment, offline restart, and synthetic latency/resource limits. No service,
continuous WSL availability, battery target, or platform beyond that evidence is advertised.

## Connect a local MCP client

Configure your MCP client's local command as the absolute built executable path with arguments
`mcp -state-dir PRIVATE_DIR -path REGISTERED_CHECKOUT -credential PRIVATE_FILE`. Start the node
separately with that same directory. Use a credential file outside Git rather than putting its
bearer value in command arguments or committed configuration. Each bridge has one fixed session;
provision a distinct credential for another registered worktree or a fresh Personal context.

The tools are `discovery`, `capture`, `search`, and `read`; their structured inputs follow the
CLI behavior above. Discovery and empty search list up to 20 permitted current choices.
Capture requires `operation_id`, `content`, and `provenance`. Search accepts `query`, optional
`limit`, and optional `related_to`. Read requires exactly one `artifact_id`, `revision_id`, or
`selector`. No tool approves, installs, executes, or publishes content. Closing the client ends
its bridge and leaves the node alive. Missing/stopped nodes return an explicit unavailable
error with a bounded timeout; no automatic startup or remote fallback occurs. The browser
HTTP `/mcp` route stays unsupported. See the development guide for actual client verification.

Basic local stdio flows were verified on 2026-10-04 with Codex CLI 0.160.0, Claude Code
2.1.289, Cursor Agent 2026.10.01-e373342, and OpenCode 1.18.32 on WSL 2. The
[client verification record](development.md#actual-client-verification) describes the exact
runtime, protocols, Work-to-worktree reuse, scope denials, and concurrent/unavailable states.
These checks verify content delivery; native harness installation and execution remain
unsupported.
