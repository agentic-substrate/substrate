# Contributing

Thanks for looking. This is a pre-alpha homelab project, so the most useful contributions right
now are issues: a harness that does not fit the adapter model, a scope-chain case that does not
resolve, a failure mode the runbook misses.

## Before you write code

- Read [AGENTS.md](AGENTS.md). It is short, and its **Gotchas** section is where the
  non-obvious invariants live.
- Read [`docs/product/non-goals.md`](docs/product/non-goals.md). Scope growth is the failure
  mode this project is most exposed to.
- For anything beyond a small fix, open an issue first. `docs/design/edd.md` §3 (DDL) and §4
  (contracts) are frozen — changing them requires a new review round, not a PR.

## The loop

```sh
make check     # fmt, vet, lint, race tests, govulncheck — everything CI's `go` job runs
make build     # static binaries into ./bin
make smoke     # boots the real server against empty state and hits /healthz and /readyz
make test      # tests alone; coverage is printed, never gated

scripts/check-docs.sh origin/main   # the docs-currency gate CI also runs
```

Go 1.26.6+ — the floor in `go.mod`, set by `govulncheck`: earlier 1.26 patches carry reachable
stdlib CVEs and CI fails on them. On this project's dev machine Go lives at `/usr/local/go/bin` and the helper tools at
`~/go/bin`; add both to `PATH` if `go` is not found. `golangci-lint` is needed for `make lint`;
`govulncheck` is fetched on demand by `make vuln`.

## Tests

Behavioural changes need a test, **and the test must have been seen to fail before the fix**.
Write the test, watch it fail for the right reason, then fix. A regression test that has never
failed is decoration — it proves nothing about the bug it claims to cover.

**Fail loudly, never skip.** If a test needs infrastructure it cannot reach, that is a failure
whose message names the fix, not a `t.Skip`. CI asserts that no test skipped.

## Commits and PRs

- [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `docs:`,
  `refactor:`, `test:`, `ci:`, `chore:`. Cite requirement IDs where they apply — `feat(scope):
  chain resolution (SCOPE-1)`.
- One logical change per PR. The repo is squash-merge only, so the PR title becomes the commit
  message on `main`.
- Fill in the `### Verified` section of the PR template with the **exact commands you ran** and
  where you ran them. This is what makes overclaiming visible, so do not paraphrase.
- If you change a surface listed in AGENTS.md's docs-currency table, change its doc in the same
  PR — or put `Docs-not-needed: <reason, 30+ characters>` in the PR body.

## CI

One workflow, `ci.yml`. A single required check named **`gate`** aggregates every job; that is
the only status branch protection knows about, so job names can change without touching repo
settings. `main` is protected and PRs land through a merge queue, which re-runs the gate against
the queued merge commit.

If the gate is red, read the failing job's log rather than its status — a check can go red for a
reason unrelated to what it is meant to catch.

## Licence

By contributing you agree your contributions are licensed under [Apache-2.0](LICENSE).
