# Contributing

**Last reviewed:** 2026-10-03. Re-read when the development workflow changes.

Substrate is an open source project. Code quality, security, and maintainability guide review.
Start by reading [AGENTS.md](AGENTS.md), the [development guide](docs/development.md), and the
relevant product or architecture decision. Discuss a substantial change in an issue before
building a competing architecture.

Choose the least code that satisfies the agreed behavior. Split files and functions by clear
responsibility when that makes the code easier to understand. Avoid unused interfaces,
speculative packages, dependencies for trivial tasks, and comments that repeat the code.
Write documentation in complete sentences that explain behavior, purpose, and limits to a
developer who did not participate in the original discussion.

Use tests to protect complex logic, security boundaries, asynchronous behavior, and other
functionality that can regress. TDD is encouraged for that work: observe a meaningful failure
before implementing the fix, then confirm it passes. Tests that assert a constant, mirror a
straightforward implementation, or exist only to inflate coverage add maintenance cost.
Configuration and simple presentation changes can use focused build, lint, or manual evidence
instead. Describe that evidence in the pull request; there is no coverage threshold.

Run `npm ci`, install Chromium with `npx playwright install --with-deps chromium`, and run
`make check`. Missing prerequisites are failures to fix, not reasons to silently skip tests.
For a branch with commits, also run `npm run check:docs-required -- --base origin/main`.
Review the prose against the changed behavior; a changed file alone does not prove the prose
is correct. Preview the docs with `npm run docs:dev`.

Keep pull requests focused and use Conventional Commit titles such as `feat:`, `fix:`, or
`docs:`. Include a `### Verified` section with exact commands and the environment used. For UI
changes, report keyboard behavior, error and loading states, reflow, and accessibility checks.
Automated accessibility checks cannot establish WCAG conformance; use manual checks for what
the tools cannot cover. Name any unverified platform or client instead of implying support.

Do not include secrets, private artifacts, or harvested conversation logs in a contribution.
Use synthetic fixtures with realistic failure cases. Follow [SECURITY.md](SECURITY.md) for
private vulnerability reports. Contributions are provided under the repository's Apache 2.0
license, with no additional contributor agreement.
