# Bootstrap implementation plan

**Last reviewed:** 2026-10-03. Re-read this plan before changing the bootstrap checks.

The goal is a small, runnable foundation for the agreed local-first product. The executable
will serve a React status page and a narrow status API. Artifact storage, MCP tools, and
synchronization remain roadmap work. Go, Vite, React, and TypeScript form the application
stack; documentation uses VitePress for local preview and a static build.

## Constraints

Code quality, security, and maintainability take priority over feature count. Prefer the
least code that satisfies the goal, split code by responsibility, and write documentation
in complete sentences. Use TDD for meaningful behavior and regression risks. Do not add
constant assertions, empty tests, coverage thresholds, speculative abstractions, or unused
runtime dependencies. Work in the isolated checkout and keep publication separate.

## Work

1. Establish a reproducible build. Put the executable in `cmd/substrate`, HTTP routing in
   `internal/server`, embedded assets in `internal/webui`, and browser source in `web/src`.
   Build browser assets before Go. Verify route separation, safe local startup, the packaged
   executable, and the browser's actual loading and error behavior.
2. Establish contributor guidance and documentation. Keep goals in `docs/product`, decisions
   in `docs/architecture`, and current setup in `docs/getting-started.md`. Provide a local
   documentation preview. Write the license, security policy, contribution guidance,
   repository instructions, and three-outcome roadmap.
3. Establish checks that fail usefully. Validate types, formatting, Go behavior, one browser
   flow, accessibility, documentation links, and narrow surface-to-doc mappings. Canary the
   documentation guard and required-check logic. Pin workflow actions to current commit IDs
   and group dependency updates. Prepare reviewable GitHub settings and issue bodies locally.

## Review focus

Check that unknown API routes cannot return the browser page, listener configuration stays
local, a stopped backend produces an honest browser error, documentation links resolve in
the built preview, and the docs gate rejects a missing update while permitting a justified
internal refactor. Do not treat planned artifact capabilities as implemented features.

## Completion evidence

Run the documented build and check commands, boot the packaged executable, and inspect the
real browser flow. Record remaining platform or hosted-CI checks honestly. GitHub settings,
issues, the Project, and publication require the bootstrap skill's final go-ahead.
