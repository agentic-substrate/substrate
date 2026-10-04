# Repository instructions

Substrate is public open source. Prefer the least code that satisfies the accepted behavior.
Code quality, security, and maintainability take priority over feature breadth.

## Work and verification

- Use native Git tooling. Do not add tool or model authorship attribution or coauthor trailers.
- Read the relevant product contract and architecture decision before changing behavior.
  Record a changed decision explicitly instead of silently contradicting it.
- Split code by responsibility when it reduces cognitive load. Avoid speculative abstractions,
  unused dependencies, and narration that merely repeats the code.
- Test behavior that can regress: complex logic, authorization, routing, asynchronous flows,
  durability, and integrations. Prefer TDD and report the observed failure and passing check.
  Do not add tests for constants or straightforward code solely to increase coverage.
- Simple configuration and presentation changes may use builds, lint, and focused manual
  evidence. There is no coverage floor. Missing prerequisites must fail with an actionable error.
- Put exact commands, environment, and material limits in the PR's `### Verified` section.

## Commands

Use Go 1.27.1, Node.js 24.15.0, npm 11.12.1, and Make on Linux or WSL 2.

```sh
npm ci
npx playwright install --with-deps chromium
make build
make check
npm run check:docs-required -- --base origin/main
npm run docs:dev
```

`make build` generates the embedded UI before compiling Go. `make check` runs the Go tests,
race detector, vet, formatting, TypeScript/Biome checks, browser flow, and built-doc link checks.
It does not prove compatibility with an untested platform or harness.

## Documentation currency

Write complete sentences explaining behavior and limits, with enough context for a developer.
Update the prose when changing a documented surface:

| Changed surface | Paired documentation |
|---|---|
| `cmd/`, `internal/`, or `web/src/` | `docs/getting-started.md` |
| `Makefile`, `package.json`, `go.mod`, `web/vite.config.ts`, `web/tsconfig.json`, `playwright.config.ts`, `biome.json`, `scripts/`, or `.github/workflows/` | `docs/development.md` |

`npm run check:docs-required -- --base origin/main` checks the committed branch and local
changes. A commit trailer `Docs-not-needed: <reason>` is the escape hatch; its explanation
must contain at least 12 characters and apply to the changed behavior. CI checks the PR's
commits. The check records a decision; reviewers still verify the sentences are true.

Product and architecture documents state their review cadence. Re-read the affected decisions
when implementing their contracts. `npm run docs:build` and `npm run check:docs` check built
local pages and anchors. External source availability requires a separate review.

## Security decisions and reusable guidance

For changes to identity, permissions, local interfaces, MCP, keys, recovery, transfer,
telemetry, or releases, read `SECURITY.md`, `docs/architecture/security.md`, and
`docs/plans/security.md` with the affected product contract. Use the repository's
`skills/substrate-security/SKILL.md` workflow for that work. `CLAUDE.md` includes these
instructions; repo-specific memories and installed skill links must point back to the
current checkout's contracts rather than replace them.

Keep confirmed direction, proposed architecture, open decisions, implementation, and dated
verification separate. Scoped enterprise recovery is required; custody, recovery granularity,
and offline enforcement remain unresolved. Customer-controlled custody is a recommendation.
Settle those decisions and meet the transfer gate before carrying customer/internal data
over a network. Management, enrollment, recovery, analytics, and content authority are distinct.
Approve field-level telemetry policy before collection. Do not claim content blindness,
remote erasure, framework conformance, or audit status without the relevant evidence.

## Accessibility

Target WCAG 2.2 AA. A UI change includes markup, styles, rendered strings, controls, and
interaction handlers. Use native elements first: buttons for actions, links for navigation,
and labels for inputs. ARIA adds semantics; it does not provide keyboard behavior. Explain
any role added where a native element could serve, and implement the complete keyboard contract.

Controls need accessible names, visible focus, logical tab order, and keyboard operation.
Use text as well as color for state. Keep text contrast at least 4.5:1, UI contrast at least
3:1, and pointer targets at least 24 by 24 CSS pixels. Provide alternatives to dragging,
respect reduced motion, and check reflow at 400% zoom. Scan relevant interactive and error
states with axe in the browser. A green scan is necessary and insufficient to prove access;
manually check keyboard use and screen reader feedback for substantial UI changes.

Document an unavoidable exception with `a11y-exception(<criterion>): <reason and alternative>`
and report it in the PR. An unexplained exception is a defect.

## Current invariants

1. Build the UI before any whole-module Go command; its files are embedded at compile time.
   Missing build output is a build failure, not an empty successful application.
2. The bootstrap server accepts numeric loopback listeners only. Its status/static routes
   stay anonymous; the read-only context API requires a scoped credential and exact origin/Host checks.
   It has no artifact APIs; exposing it remotely would bypass the current boundary.
3. Unknown `/api/` and `/mcp` routes must return errors instead of the SPA entry point.
   Otherwise unsupported integrations can mistake an HTML page for an available API.
4. Local artifact storage is implemented through scoped CLI commands. Browser artifact APIs,
   retrieval, MCP, and synchronization remain unimplemented; preserve their unavailable states.
