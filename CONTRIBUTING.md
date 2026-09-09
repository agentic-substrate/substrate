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
make smoke     # boots the real server with no database; /healthz 200 and /readyz 503
make test      # tests alone (`-p 1`); coverage is printed, never gated
sqlc generate  # or `make sqlc` — regenerate internal/store after changing migrations/ or queries.sql
make ko-build  # distroless substrate-server via ko --local; does not push. LOCAL INSPECTION
               # ONLY: ko.local/substrate-server:latest is a mutable tag, which the
               # --substituted gate rejects and a containerd node cannot pull.
make ko-push KO_DOCKER_REPO=<registry>   # builds, PUSHES, and prints repo/name@sha256:… —
                                         # the digest that goes in deployment.yaml

scripts/check-docs.sh origin/main   # the docs-currency gate CI also runs
scripts/check-cli-commands.sh       # asserts docs/ops/setup.md's command map matches `substrate --help`,
                                    # and that every `substrate …` invocation in a fenced block under
                                    # README.md or docs/ names a command the binary actually exposes
                                    # (CI gate, `docs` job — it builds cmd/substrate itself)
                                    # `make check` also needs bash, zsh and fish on PATH: the
                                    # completion tests syntax-check the generated scripts with the
                                    # real shells, and a missing one fails naming the install command
scripts/check-deploy-secrets.sh     # grep-based: no credential, token, or tailnet name under deploy/
                                    # (CI gate — it runs on every PR)
scripts/restore-assert.sh N N N     # weekly restore job: zero rows on memory/instruction/audit is a failure

# Placeholder state of deploy/. CI runs --committed; the operator runs --substituted
# after filling values in, before `kubectl apply`.
scripts/check-deploy-placeholders.sh --committed deploy     # nothing is substituted yet, and every
                                                            # placeholder is named in the runbook
scripts/check-deploy-placeholders.sh --substituted deploy   # nothing is LEFT to substitute, images
                                                            # are digest-pinned, no secret is empty.
                                                            # This is expected to exit 0 on a
                                                            # correctly-substituted tree; a
                                                            # mandatory gate that cannot pass is a
                                                            # step people learn to skip.
```

`check-deploy-secrets.sh` and `check-deploy-placeholders.sh` answer different questions and
neither subsumes the other. The secret scanner catches a value that should never have been
written down. The placeholder checker catches a value that *looks* filled in but is not —
`imageName: substrate-pg:16-pgvector` reads as a real tag, applies cleanly, and lands in
`ImagePullBackOff`; `password: ""` applies as a literal empty password, which the secret
scanner explicitly allows because empty is the correct *committed* state.

`--substituted` only looks at what `kubectl apply -k deploy/` actually applies. `deploy/minio/*`
are operator-side `mc` templates and `deploy/ingress/certificate.yaml` is deliberately not in
kustomize resources, so both are out of scope there (they are still fully scanned in
`--committed`). This is not a convenience: "substituting" `deploy/minio/buckets.sh` means writing
live MinIO access and secret keys into a file in the working tree, which is exactly what
`check-deploy-secrets.sh` exists to prevent. Shell scripts are skipped in that mode too — they
are executed, not substituted, and `<empty>` inside an error message is not an operator work item.

Both scanners also track container `env:` pairs (`- name: POSTGRES_PASSWORD` on one line,
`value: …` on the next). A key-anchored regex cannot see that shape, because the key on the line
carrying the secret is `value`.

The restore scripts under `deploy/restore/` are **POSIX sh, not bash**, and that is enforced by
a test that executes them under `dash`. They run in a kubectl image (`rancher/kubectl`,
`alpine/k8s`) which is busybox-only; a `[[ ]]` or `(( ))` there is a parse error that fires
*after* the row counts were collected, so the operator ends up debugging the shell instead of
the backup.


Go 1.26.6+ — the floor in `go.mod`, set by `govulncheck`: earlier 1.26 patches carry reachable
stdlib CVEs and CI fails on them. On this project's dev machine Go lives at `/usr/local/go/bin` and the helper tools at
`~/go/bin`; add both to `PATH` if `go` is not found. `golangci-lint` is needed for `make lint`;
`govulncheck` is fetched on demand by `make vuln`. `sqlc` is needed when changing `migrations/`
or `internal/store/queries.sql`; generated files are committed.

`make smoke` boots the server with no database and asserts `/healthz` 200 and `/readyz` 503
(EDD §16: a Postgres outage must not kill the process). Store integration tests boot Postgres 16
with pgvector via Docker. They prefer `pgvector/pgvector:pg16`, and fall back to building
`testdata/pgvector` from `postgres:16-alpine` if that pull fails. `make test` and CI's `go test`
pass `-p 1` so packages that each boot a testcontainer do not stampede the Docker daemon.

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
settings. `main` is protected: linear history, squash-only, no force-push, no deletion, and the
`gate` check required.

PRs land through a **merge queue** (squash, all-green grouping), which re-runs the gate against
the queued merge commit rather than trusting the result from your branch. That is what makes the
gate's `if: !cancelled()` safe: cancelling a run cannot get a change past the queue's own re-run.

Merge queue requires an organization-owned repository, which is why this repo lives under
`agentic-substrate` rather than a personal account.

If the gate is red, read the failing job's log rather than its status — a check can go red for a
reason unrelated to what it is meant to catch.

## Licence

By contributing you agree your contributions are licensed under [Apache-2.0](LICENSE).
