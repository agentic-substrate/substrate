# Development

**Last reviewed:** 2026-10-04. Re-read whenever a command, tool version, or CI contract changes.

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
processes. Pending work is committed with content; there is no consumer or pause/resume control
yet. Existing receipts survive retries, while retirement remains authoritative for later reads.
Schema version 1 is created transactionally and unknown versions fail before contribution.
Do not edit the database directly or treat copying a live file as an application backup.

After `make build`, run `go test ./internal/artifacts ./cmd/substrate` for reopen, concurrency,
failed commit, receipt rollback, stale edits, retirement, authorization, private placement,
Git replacement/source, and CLI checks. The full `make check` still supplies the race detector,
web accessibility flows, static checks, and built-document validation. Tests use synthetic data.
Their temporary roots must be outside every Git checkout; an ancestor `.git` marker causes
intentional fail-closed placement denial, even if its Git metadata is broken. Select a clean
`TMPDIR` in that case. The issue #6 local verification used `TMPDIR=/var/tmp` because this host
has an unrelated `/tmp/.git` marker. That environment choice is not a source-code exception.
