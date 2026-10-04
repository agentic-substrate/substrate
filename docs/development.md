# Development

**Last reviewed:** 2026-10-03. Re-read whenever a command, tool version, or CI contract changes.

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
checks enforcement and its written-reason escape hatch. None of these establishes artifact
functionality or cross-harness compatibility.

## Local development and previews

After `make build`, run `./bin/substrate serve` in one terminal and `npm run dev` in another.
Vite serves the editable UI on loopback and proxies `/api` requests to the Go listener on
port 9842. The built executable continues to serve its previously compiled assets until rebuilt.

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
introduced advisories; Dependabot groups updates for Actions, npm, and Go. A separate CodeQL
workflow reports Go and JavaScript/TypeScript findings. Third-party Actions are pinned to
commit SHAs. CodeQL uses separate language jobs: Go requires the real build, while JavaScript
and TypeScript use source extraction without a manual build. The documentation test-output
checks use the standard runner's grep command and do not require a ripgrep installation.
GitHub alerts, reporting, tag protection, and the required check are separate
repository settings that must be read back and canary-tested after publication.

This temporary verification branch exercises dependency review and workflow protection.
