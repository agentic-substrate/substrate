# Development

**Last reviewed:** 2026-10-05. Re-read whenever a command, tool version, or CI contract changes.

## Setup and verification

Install Go 1.27.1, Node.js 24.15.0, npm 11.12.1, and Make. Run `npm ci` from the repository
root. Install the browser and its Linux prerequisites with
`npx playwright install --with-deps chromium`. These are development dependencies; users of
the built executable do not need them.

`make build` type-checks and builds the web assets, then compiles `bin/substrate` with those
assets embedded. Whole-module Go commands require the generated assets first.

`make check` builds the executable, runs Go tests with the race detector and shuffled order,
prints coverage, runs tests for the documentation guard, and drives the actual executable in
Chromium. It also runs Go vet and formatting checks, TypeScript and Biome checks, and builds
the documentation before checking its local links and anchors. It fails when a required
dependency is missing. No test opt-out or coverage threshold is configured.

Use tests where they protect behavior that can fail. The routing suite checks the boundary
between client navigation and API errors. The browser flow checks connection failure,
keyboard retry with retained focus, error-state accessibility, and narrow-screen reflow. The docs guard suite
checks enforcement and its written-reason escape hatch. The artifact tests cover local capture/inspection and revision persistence; they do not
establish cross-harness compatibility, device-failure durability, or complete recovery.

## Local development and previews

After `make build`, run `./bin/substrate serve` in one terminal and `npm run dev` in another.
Vite serves the editable UI on loopback and proxies `/api` requests to the Go listener on
port 9842. Anonymous status works through this proxy. Credentialed context inspection uses
the packaged page at the Go listener origin: the protected route intentionally rejects a
Vite origin/Host rather than weakening its local browser boundary. Rebuild to inspect changed
context UI behavior with credentials. The built executable continues to serve its previously
compiled assets until rebuilt.

Run `npm run docs:dev` to preview documentation. `npm run docs:build` produces a static site in
`docs/.vitepress/dist`; `npm run docs:preview` serves that build. CI publishes the built site as
a downloadable preview artifact for each run. This bootstrap does not deploy a public docs
site automatically.

The stable VitePress release uses an overridden Vite 6.4.3 dependency to remove known build-tool
advisories in its older dependency range. The actual docs build and link check verify this
combination. Remove the override when a supported stable VitePress release adopts a patched
Vite. Application assets use Vite 8 independently. Biome supplies the JSX accessibility lint
rules and formatting; TypeScript performs the type check.

## Documentation decisions

For a branch with commits, run
`npm run check:docs-required -- --base origin/main`. The command includes committed branch
changes, tracked local changes, and untracked files. CI supplies the pull request base SHA.
The surface mapping in the repository instructions is enforced by the script. Update the
paired guide, or explain why its behavior does not change in a commit trailer:

```text
Docs-not-needed: The refactor preserves all documented behavior and configuration.
```

Reasons shorter than 12 characters fail. Only a trailer from the checked commit range can
satisfy the escape hatch. Read the changed prose against the code; this guard does not judge
its accuracy. The built link check validates local pages and anchors, while external sources
need review when the corresponding decision is revisited.

## CI and security

CI runs backend, frontend, and documentation jobs for pull requests, main pushes, and merge
groups without workflow path filters. A stable `gate` check requires their success and the
dependency review on pull requests. It runs with `always()` so failed or cancelled prerequisites
cannot turn the gate green. A cancelled hosted run can leave a pending check; rerun it. Revisit
merge queues when contributor volume makes that operational cost material.

Backend CI checks reachable Go vulnerabilities with govulncheck. Dependency review blocks
introduced advisories in runtime, development, and unknown scopes because build dependencies
also run in contributor and CI environments. Dependabot groups updates for Actions, npm,
and Go. Third-party Actions are pinned to commit SHAs. The documentation test-output
checks use the standard runner's grep command and do not require a ripgrep installation.
GitHub alerts, reporting, tag protection, and the required check are separate
repository settings that must be read back and canary-tested after publication.

CodeQL scanning is disabled at the repository, and its automatic workflow has been removed
to conserve the current usage budget. The required `gate` belongs to CI and does not depend
on CodeQL. Revisit scanning when maintainers allocate a budget or before the first supported
production release; review the workflow and repository setting together when restoring it.

The [security roadmap](plans/security.md) records recommended CSF/SSDF/SLSA baselines, a
control-evidence inventory, release verification, incident/restore work, and tests to retain
when CI cost is reduced. These are future gates and evidence requirements, not additional
implemented jobs or a claim of framework conformance. Keep the only test protecting a trust
boundary; a flaky check needs a tracked fix and compensating verification. Review dependencies,
untrusted build inputs, release credentials, and actual delivered artifacts at their boundaries.

The repository's `skills/substrate-security/SKILL.md` is the reusable security planning/review
workflow referenced by `AGENTS.md` and, through it, `CLAUDE.md`. Repo-specific memory entries
and personal skill installations should link to these maintained contracts; they are reminders,
not independent policy or control evidence.

## Local SQLite development

The artifact store uses `github.com/ncruces/go-sqlite3` v0.35.6 with the driver's supported
FTS5 connection initializer; an ordinary driver import alone does not enable FTS5. Its pinned
SQLite build reports version 3.53.4 in the capability check. There is no cgo, external SQLite
installation, or FTS5 build tag. The module lock records the accompanying generated Wasm-to-Go
SQLite module and other runtime dependencies. Revisit this choice with the
[artifact persistence contract](architecture/artifacts.md#selected-local-persistence-contract)
when changing versions or indexing behavior. No benchmark or resource target is claimed.

`internal/artifacts` splits schema/private-file access, revisions/receipts, scoped inspection,
and immutable Git reads. Storage sessions authenticate on every operation; ordinary serialized
context is not a storage credential. A single connection per store bounds connection count,
and SQLite's immediate transactions and five-second busy timeout coordinate separate command
processes. URI `modeof` points to the validated private database so journals are also owner-only
under an ordinary 022 umask. A subprocess regression holds a live transaction while another
process opens the store, and abrupt-exit/cache-spill evidence exercises hot-journal rollback.
This process-interruption check does not establish device-failure or complete recovery.
Pending operation receipts are committed with content and retained. A separate coalesced index
queue supports bounded atomic batches and explicit pause/resume.
Existing receipts survive retries, while retirement remains authoritative for later reads.
Schema versions 1 and 2 migrate transactionally to version 3, preserving source selection and
retry receipts while adding revision associations and rebuildable token postings. Unknown versions fail before contribution.
Do not edit the database directly or treat copying a live file as an application backup.

After `make build`, run `go test ./internal/artifacts ./cmd/substrate` for reopen, concurrency,
failed commit, receipt rollback, stale edits, retirement, authorization, private placement,
Git replacement/source, invalid UTF-8, promisor helper/network denial, and CLI checks.
Source reads set an empty `GIT_ALLOW_PROTOCOL` allowlist, verified on Git 2.43.0, overriding
repository and inherited caller protocol settings before a transport or helper can run.
All stored text is valid UTF-8; limits count bytes and invalid input is rejected before
receipt fingerprinting. The full `make check` still supplies the race detector,
web accessibility flows, static checks, and built-document validation. Tests use synthetic data.
Their temporary roots must be outside every Git checkout; an ancestor `.git` marker causes
intentional fail-closed placement denial, even if its Git metadata is broken. Select a clean
`TMPDIR` in that case. The issue #6 local verification used `TMPDIR=/var/tmp` because this host
has an unrelated `/tmp/.git` marker. That environment choice is not a source-code exception.


Local Git selection separates trusted owner operations from credentialed proposals and reads.
`internal/artifacts/registration.go` binds qualified identities, `approval.go` handles expected-head
and default pins, `selection.go` filters scope before resolving alternatives, and `bundle.go`
captures an explicit bounded dependency inventory through the hardened raw Git reader. Owner
commands cannot be invoked through the scoped session command handler. Registration/approval
labels do not establish audience or execution rights. Approval and retirement enqueue durable
incremental work for the index consumer.

Focused checks cover source movement and deletion after approval, reopen, stale approval and
competing candidate state, ambiguous aliases, override target changes, retirement, same-space
repository and Work/Personal filtering, revocation, failed approval rollback, version 1 migration,
bounded UTF-8 dependency snapshots, and the proposal/owner/read CLI flow. Use the same clean
`TMPDIR` and full verification environment described above. Content delivery has no native
adapter, script runner, external dependency resolver, or tested harness-installation behavior.

## Retrieval and local MCP development

The official Go MCP SDK is pinned at `github.com/modelcontextprotocol/go-sdk` v1.8.0. It supplies
stdio protocol negotiation, schemas, and tool dispatch; Substrate's bridge validates bounded
UTF-8 frames before the SDK decoder. `internal/node` owns private IPC, the installation lock,
connection limits/deadlines, and bounded indexing. `internal/artifacts` owns current eligibility,
revision associations, derived index transactions, deterministic lexical ranking, and current
reads. `internal/strictjson` rejects text normalization at both transport boundaries. There is
no new Node or model runtime requirement in the installed executable.

After building embedded assets, run `go test ./internal/artifacts ./internal/node
./internal/mcpbridge ./cmd/substrate` with the clean temporary-directory environment described
above. Tests cover scope-invariant scores and coverage, hidden aliases/topics/identifiers,
current direct reads, endpoint-filtered relationships, stale index/retirement invalidation,
failed index rollback, paused save/reopen/resume, posting limits, legacy retry migration,
source/search alias separation, private lock failures, malformed bounded frames, concurrent
scoped bridges, current revocation, stopped-node errors, and bridge/node lifetime separation.
The CLI test observes absent-node capture failing without opening a database. Owner source
commands share the runtime lock and require an explicitly stopped node. These SDK-level
checks do not replace versioned actual harness calls or establish complete recovery.

Use `make build` before a packaged client journey. Client configuration must be scoped to a
synthetic fixture or one invocation, retain credentials outside Git, and avoid logging bearer
values. Record initialization client/version/protocol and actual tools/list/tools/call traffic,
including each advertised client's discovery/capture/search/read/denial/concurrency/unavailable
path. A successful model answer, process exit, or version command alone is not compatibility
evidence. Client native installation/execution remains unsupported. Resource measurements and
actual client verification are recorded below when observed; no latency, memory, or energy
target is promised by source or a protocol specification alone.

### Measured lexical workload

On 2026-10-04, the issue #8 worktree ran the compiled artifact test workload on Linux amd64
under WSL 2, Go 1.27.1, Git 2.43.0, SQLite 3.53.4, and an Intel Core i9-13980HX with
GOMAXPROCS 16. Nine five-artifact query cases checked exact identifiers/revisions, explicit
aliases/topics, lexical phrases, prefixes, approved skills/definitions, and expected misses
for a typo and unrecorded paraphrase. All expected leading matches/misses passed; individual
query calls took 6.3–8.3 ms in that run. This is a small synthetic evaluation, not a corpus
recall claim.

Run the reproducible resource workload after `make build`:

```sh
go test -c -o /var/tmp/substrate-retrieval-benchmark.test ./internal/artifacts
TMPDIR=/var/tmp /usr/bin/time -v /var/tmp/substrate-retrieval-benchmark.test -test.run '^$' -test.bench BenchmarkScopedLexicalSearch -test.benchtime=20x -test.benchmem -test.count=1
```

The fixture contains 256 repository-scoped synthetic observations, each about 180 bytes with
an identifier/topic. Cold indexing took 222.6 and 225.7 ms across calibration and measured
setups; first-query latency was 15.8 and 11.8 ms. Twenty warm mixed lexical/exact/prefix/miss
queries averaged 11.64 ms, 1,050,904 allocated bytes, and 12,768 allocations per operation.
The entire compiled test process used 3.17 seconds user CPU, 2.37 seconds system CPU,
9.09 seconds elapsed, and 24,320 KiB peak RSS, with zero major faults/swaps. Those process
figures include two fixture creations/calibration, Git prerequisite processes, SQLite commits,
and indexing; they are not query-only resource costs or node peak measurements. Other activity
was running on the same host. The executable/test makes no model runtime or inference calls;
cloud clients used separately for compatibility are not part of this measurement. No battery,
energy, large-corpus, concurrent-load, model-quality, or other-platform target is established.
Repeat with representative real permitted data before selecting an optional enhancement.

### Actual client verification

On 2026-10-04, actual client subprocesses passed against runtime revision
`badcd99646564d5d0f2baeef00a70176662fa826` on Linux amd64 under WSL 2. The packaged binary's
SHA256 was `8e30d95d656a1b042e2ac415f8734481bceb49df16ac1db175fc4d9643707dae`.
Each client listed the four tools and made successful discovery, capture, search, and current
read calls, including both approved Git artifact types with exact revision, source, content,
and provenance. Each also exercised empty Personal discovery/search, generic denials for a
Work artifact ID and qualified source identity, and explicit errors from a separate stopped
installation. Two overlapping processes per client saved distinct observations and read their
receipts. Wire responses, identities, and error contents were asserted; process exit and model
narration alone were insufficient.

| Tested executable | Observed MCP negotiation | Concurrent process overlap |
|---|---|---|
| Codex CLI 0.160.0 | `initialize`, `2025-06-18` | 25.77 seconds |
| Claude Code 2.1.289 | `server/discover`, `2026-07-28` | 8.58 seconds |
| Cursor Agent 2026.10.01-e373342 | `initialize`, `2025-11-25`; wire client version `1.0.0` | 21.45 seconds |
| OpenCode 1.18.32 | `initialize`, `2025-11-25` | 10.68 seconds |

Codex saved a synthetic Work observation; Claude searched and read the identical artifact and
revision from the separately registered Work worktree, preserving its content and provenance.
Personal results and eligible/indexed/pending/limited counts remained zero, and denied reads
returned no structured artifact metadata. Closing every bridge left the explicitly started
node alive. The node was then stopped and reopened using the same final binary and state.
Both Codex and Claude repeated their original save operations through actual tool calls:
each returned an identical receipt and read the same revision, content, and provenance after
the restart. This checks process restart, without establishing device-failure recovery.

The manual fixture used private state and credential files outside Git, synthetic repositories,
and a transparent Python stdio recorder. Invocation or private project configuration enabled
only the synthetic MCP servers and disabled native file, shell, and web actions. Prompts
prohibited delegation, and emitted client tool events were checked for unexpected actions.
Global client configuration
and authentication were not changed. Existing Codex, Claude, and Cursor authentication was
used. OpenCode used the working anonymous `opencode/space-bunny-free` provider; two earlier
free-provider requests returned service errors and did not count as compatibility evidence.
These cloud model calls evaluated client integration, independently of the model-free retrieval
workload above. Credentials, raw provider output, and private transcripts stay outside Git.

The PR's `### Verified` section records the exact fixture-runner and assertion commands.
Re-run this matrix after SDK, transport, authority, node, or advertised client-version changes.
The observations establish basic local stdio behavior for these executable versions and this
synthetic workload. They do not establish native installation/execution, remote MCP, other
platforms, a latency guarantee, complete recovery, or complete accessibility conformance.


## Foreground maintenance evidence

The single foreground worker coalesces incremental artifact jobs and commits one artifact per
cancellable transaction. Maintenance tests cover durable pause, unchanged checkpoints, forced
bulk rebuilds, deferred work across restart, SQL-injected rollback and generic failures,
explicit retry after edits, hidden Work failures, scoped counts beyond the 100-record browser
inventory, bounded failed attempts, and concurrent pause/capture/index/shutdown. A status-poll
regression first left five healthy jobs pending after the synchronous 100-attempt barrier;
serving controls without resetting the coalescing timer lets scheduled work finish. The CLI
keeps installation queue counts separate from browser-scoped lexical coverage.

Run the reproducible packaged synthetic workload after `make build`:

```sh
python3 scripts/measure-maintenance.py --binary bin/substrate --memories 256 --idle-seconds 2
```

It requires Python 3, Git, Linux `/proc`, and permission for `unshare --user
--map-root-user --net`. Missing prerequisites or failed assertions stop with errors; an
unavailable namespace is not skipped. The harness creates only synthetic private fixtures in
`/var/tmp`, starts its own foreground nodes, and removes its own fixture after stopping them.
It captures 256 memories of 126 words and imports one approved Git skill with a main document
and two explicit dependency files, each containing 512 words. It pauses capture, checks duplicate
startup, uses both SIGINT and SIGTERM shutdown, verifies socket cleanup, restarts the same state
in a namespace with zero network routes, and preserves exact receipts and the pending ledger
through resume and lexical retrieval. Source registration and approval run offline under the
same installation lock. The namespace blocks networking; it does not stop or reboot WSL.

Latency samples use Python's monotonic performance clock around actual Unix IPC calls;
they exclude command-process startup. Incremental wall time includes the synchronous first
100 attempts and automatic completion observed through status reads. Bulk time includes
explicit bounded run commands. Node CPU uses `/proc/PID/stat` user plus system ticks, excluding
Git subprocesses, CLI/harness CPU, and host energy. RSS is sampled every ten milliseconds and
reports the maximum observed node resident set rather than an absolute peak. Idle samples
measure finite intervals at the kernel clock resolution; observed zero ticks cannot prove no
work or a battery benefit. Database sizes are local main-file sizes after committed stages,
not complete installation or backup size. Repeat with representative permitted data before
claiming performance targets, large-corpus behavior, concurrent-load latency, other hardware,
services, WSL distro survival, native Windows/macOS, or battery use.


On 2026-10-05, the command above passed on Linux amd64 in WSL 2 on host `Legion`, kernel
`6.6.114.1-microsoft-standard-WSL2`, with a reported Intel Core i9-13980HX and 16 logical
CPUs. The toolchain was Go 1.27.1, Node.js 24.15.0, npm 11.12.1, Git 2.43.0, and Python 3.12.3.
The freshly built packaged executable SHA256 was
`9b6f37b052b48eebfbe39f921666edc567fa527ab87d8d49ee238aa24402e22e`.
Runtime sources match [36de0e4](https://github.com/agentic-substrate/substrate/tree/36de0e4c7f5b4d182152b6ef39989af85360cb86);
the measurements used its pre-commit build. Other task agents held heavy work during this measurement window;
background host activity was not controlled. Node CPU resolution was 0.01 seconds.

| Observed stage | Result for this synthetic workload |
|---|---|
| Capture Unix IPC, 256 samples | p50 15.619 ms; p95 23.465 ms |
| Search Unix IPC, 40 samples | p50 10.184 ms; p95 11.179 ms |
| Incremental completion, 257 artifacts | 2,658.360 ms elapsed; 1.36 seconds node CPU |
| Explicit bulk rebuild, 257 artifacts | 3,407.937 ms elapsed; 2.24 seconds node CPU |
| Sampled maximum node RSS | 26,260 KiB |
| Paused idle and ready idle, two seconds each | 0.00 observed node CPU seconds in each interval |
| Main database after paused memory capture | 1,150,976 bytes |
| Main database after approved source and indexing | 11,915,264 bytes |
| Main database after full rebuild | 12,025,856 bytes |
| Offline namespace | `/proc/PID/net/route` was empty; zero routes |

Both supported signals stopped the node successfully and removed its socket. Duplicate startup
failed while preserving the live node. Restart omitted the pause override and retained pause;
exact capture retry returned its original artifact/revision receipt, and current reads worked
before indexing resumed. After resume and explicit bounded rebuild, all 257 eligible revisions
were indexed, with zero queued/deferred/failed/limited records. The complete pending operation
ledger was unchanged by indexing. These observations establish foreground process behavior
for this tested WSL distribution, without claiming systemd support, host reboot or distro
shutdown survival, device/power-failure durability, whole-installation recovery, another OS,
absolute RSS peaks, charging policy, or energy/battery savings.
