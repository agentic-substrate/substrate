# AGENTS.md

Substrate is a self-hosted control plane that serves one authoritative context — instructions,
preferences, memory, and skill state — to any AI coding harness on any machine. Local config
files are a **cache**, never a source.

Read `docs/product/non-goals.md` before proposing anything that grows the scope.

## Verification — run these, don't guess

```sh
make check     # gofmt, go vet, golangci-lint, go test -race, govulncheck. Everything CI's go job runs.
make build     # static binaries into ./bin
make smoke     # boots the real server against empty state and hits it
make test      # tests alone, with coverage printed (not gated)
scripts/check-docs.sh origin/main   # the docs-currency gate
```

Go lives at `/usr/local/go/bin` and the helper tools at `~/go/bin`; add both to `PATH` if `go`
is not found. Do not claim work is done without `make check` passing on the actual diff.

## Layout

`cmd/` holds the three binaries: `substrate-server` (MCP `/mcp` + REST `/v1` + jobs),
`substrate-adapter` (per-machine daemon), `substrate` (operator CLI). Domain logic lives under
`internal/`, one package per concern, and `migrations/` holds goose SQL. The full intended
package list is in `docs/design/edd.md` §2.1 — packages are created as their phase lands, not
up front.

## The two source documents

`docs/design/prd.md` and `docs/design/edd.md` are **reviewed, signed-off records**, not working
notes. The EDD's §3 DDL and §4 contracts are frozen: changing them requires a new review round,
so raise it rather than editing them mid-task. Requirement IDs (`MEM-7`, `CTX-2`, `SCOPE-1`)
are the vocabulary — cite them in commits, PRs, and issues.

## Naming: the docs say `acp`, the code says `substrate`

The design docs were written under the working name "Agent Control Plane" and use `acp`
throughout. The project was renamed before first commit. When implementing from those docs,
translate:

| In the docs | In the code |
|---|---|
| `cp-server`, `cp-adapter`, `cp` | `substrate-server`, `substrate-adapter`, `substrate` |
| `~/.acp/` | `~/.substrate/` |
| `ACP_DENIED_SCOPE`, `ACP_DENIED_VISIBILITY`, `ACP_NEEDS_REVIEW`, `ACP_BUDGET_TOO_SMALL`, `ACP_SECRET_DETECTED` | `SUBSTRATE_DENIED_SCOPE`, `SUBSTRATE_DENIED_VISIBILITY`, `SUBSTRATE_NEEDS_REVIEW`, `SUBSTRATE_BUDGET_TOO_SMALL`, `SUBSTRATE_SECRET_DETECTED` |
| `acp_audit`, `acp_tool_latency_seconds` | `substrate_audit`, `substrate_tool_latency_seconds` |
| database `acp`, buckets `acp-sessions` / `acp-backups` | `substrate`, `substrate-sessions` / `substrate-backups` |
| module `acp/` | `github.com/agentic-substrate/substrate` |
| GUCs `acp.actor_id`, `acp.team_ids`, `acp.is_admin`, … | `substrate.actor_id`, `substrate.team_ids`, `substrate.is_admin`, … |
| roles `acp_app`, `acp_migrate` | `substrate_app`, `substrate_migrate` |

**Never introduce a new `acp` identifier.** These strings end up in error messages, backup
paths, and Kubernetes object names, where renaming them later is a migration.

## Gotchas

Numbered so they can be cited. Each states its failure mode, because the failure mode is what
makes a novel instance of the same trap recognizable.

1. **`user` is not a level of the scope chain.** Applicability resolves along
   `global → org → team → project → repo → branch → task → session`; preferences resolve
   `user > team > org` as an orthogonal overlay. *Failure mode:* treating `user` as a chain
   level makes a personal preference silently override a project instruction, which INST-3
   exists to prevent.
2. **`Path.Ancestors()` returns most-specific-first, and the compiler depends on it.**
   Instruction resolution takes the *first* active instruction per key. *Failure mode:* reverse
   the order and `python.version` resolves to the global value everywhere, with no error.
3. **Instructions are never scored, ranked, or trimmed.** They are a separate deterministic
   table, not memory. *Failure mode:* routing an instruction through retrieval makes a mandatory
   rule present only sometimes, which is worse than absent — the agent learns not to trust it.
4. **Agents write `unverified`, always.** No agent code path may set `confirmed`, and
   `memory.feedback` never changes `status` on its own (MEM-6). *Failure mode:* fifty
   low-trust votes silently demote a human-confirmed fact, and the store becomes confidently wrong.
5. **Memory renders as quoted data with a preamble; only instructions render as directives.**
   *Failure mode:* a stored memory body carrying "ignore previous instructions" becomes an
   instruction. The fence is the weakest layer of this control — status gating and the
   instruction/memory split are the real one, so never weaken those to simplify rendering.
6. **Never `DELETE` from a domain table.** Supersession sets `superseded_by` and keeps an edge;
   retirement sets `status`. *Failure mode:* GOV-2 ("why do agents believe Y") becomes
   unanswerable, and it fails silently — the query just returns fewer rows.
7. **`/readyz` checks Postgres only** (EDD R27). `/healthz` is 200 whenever the process is
   alive. `/readyz` returns 503 until migrations have applied and the pool pings; a Postgres
   outage must not exit the process. Git and the skills repo are not part of readiness.
   *Failure mode:* exiting on a store error crashloops in Kubernetes, so the readiness probe
   never gets a process to probe, and a brief database blip becomes CrashLoopBackOff. Making
   Git or the skills repo part of readiness takes the whole service out of rotation for an
   outage it is designed to survive.
8. **Hooks must exit in under 2 seconds**, with a 1.5s server timeout and a fallback to the
   local cache or outbox. *Failure mode:* a slow server stalls every harness on every machine,
   and it looks like the harness is broken, not Substrate.
9. **Rendering must be byte-deterministic** — sorted keys, LF endings, no timestamps outside the
   footer, and the footer is excluded from the drift hash. *Failure mode:* every adapter cycle
   reports drift and the review queue fills with noise until people stop reading it.
10. **Outbox writes carry a `client_id` (UUIDv7) and the server keeps an `ingest_receipt`.**
    *Failure mode:* a retried batch after a network blip duplicates memories, and the duplicates
    then corroborate each other into promotion.
11. **RLS session settings are `substrate.*` on both sides.** The middleware `SET LOCAL` and the
    SQL policies must spell the same names. *Failure mode:* a typo (`acp.actor_id` in a policy,
    `substrate.actor_id` in Go) makes every policy match nothing — reads come back empty, writes
    are refused, and no error names the cause.
12. **The shared Postgres test fixture is `internal/pgtest`.** `pgtest.StartMigrated` is a
    per-process `sync.Once` singleton and `pgtest.EnsureGlobal` seeds the global scope once;
    both `t.Fatalf` when Docker is missing and never `t.Skip`. Several packages still carry an
    older private copy — port them to `pgtest` rather than adding a new copy. *Failure mode:* a
    fixture that starts a container per test multiplies an already slow suite, and one that
    `t.Skip`s turns a missing Docker daemon into a green run that tested nothing. `pgtest` is a
    non-test package, so its testcontainers → Docker → `x/crypto/ssh` tree is permanently in
    `govulncheck`'s scope: a scanner hit there is a dependency bump, not a code bug, and must not
    be "fixed" with a build tag — tagging it would force the tag onto every importing `_test.go`
    and break bare `go test ./...`.
13. **Close an enumeration oracle by making the answers equal, not by making both denials.**
    `scope` carries no RLS and resolves any repo key for anyone, so the only refusal for a
    foreign repo is `42501` at the INSERT; a read that never writes skips it and the status
    code alone tells an attacker which repo keys this plane binds. The read paths therefore
    do not refuse: `resolveReadableRepoPaths` gates on `scope_readable` and, for a key the
    caller may not read *or* a key bound nowhere, returns nothing so the handler falls back to
    the global chain and answers the byte-identical no-`?repos=` body. Writes still refuse --
    `h.repoScope` keeps `scope_writable` and the masked 403. *Failure mode:* refusing on the
    read path also kills the oracle but silently takes `visibility='global'` content from the
    readers it was published for; "both are 403" passes any test lacking a positive control.

14. **A user token never expires; an agent token dies at 24h.** `identity.Lookup` TTL-caps
    `kind='agent'` only (`lookup.go`), and `api_token.expires_at` is nullable, so
    `admin create-user` writes a NULL expiry deliberately. *Failure mode:* treating
    `~/.config/substrate/config.json` as a short-lived cache — it is a long-lived credential, so
    it is written 0600 via temp-file-plus-rename and **refused on load** when `mode&0077 != 0`.
    Relaxing that check to a warning silently accepts a leaked token forever.
15. **`internal/cli` has no package-level command, flag or client.** Every command is
    `newXCmd(Deps) *cobra.Command` with its flags in a closure struct, and `Deps` carries stdout,
    stderr, stdin, env, getwd, HTTP client, installer, config dir and clock. *Failure mode:* a
    package-level `rootCmd` or flag var makes two roots share state, which `-race` reports as an
    intermittent failure in an unrelated test rather than as the aliasing bug it is.

16. **A bootstrap command cannot use `store.Tx`.** `Tx` and `TxReadOnly` return `ErrNoPrincipal`
    when `identity.FromContext(ctx)` is nil, and nothing in `cmd/` ever puts a principal there —
    `substrate admin create-user` is the command that creates the first one, so by definition it
    has none. It uses `store.TxBootstrap` (no principal, no session GUCs, still one atomic
    transaction); `token mint` uses `st.Pool()` for the same reason. *Failure mode:* the command
    opens the pool, runs migrations, then dies with the bare string `store: no principal on
    context` having written zero rows — and it stays green in CI, because a test that injects a
    fake `DBTX` straight into the domain function never reaches `store.Tx` at all. A bootstrap
    path must be tested end-to-end against a real Postgres.

17. **The `substrate` CLI has no `-dry-run`/`-commit` pair; a preview is a verb.** `import
    scan`/`plan` and `adapter status` are the read-only steps, and `import apply`, `review
    approve|reject`, `adapter install|uninstall` all write. The wire protocol still carries
    `dry_run` and `commit` — only the CLI surface lost them. *Failure mode:* a banner on stdout
    with exit code 0 means a script that forgot `-commit` cannot tell a preview from a write,
    so it reports success for work that never happened. Re-adding a preview flag to a writing
    verb reintroduces exactly that.
18. **A command that writes echoes its resolved scope and the source that resolved it, then
    confirms.** Off a TTY it must refuse without `--yes`. *Failure mode:* scope now falls back
    to the checkout's git remote, and `h.repoScope` deliberately never returns the resolved
    chain, so an unconfirmed apply in the wrong checkout writes at a chain nobody saw and
    nobody can read back.

## Conventions

- **Errors:** wrap with `%w` and check with `errors.Is`/`As`. Policy denials return a
  machine-readable code so hooks can branch without parsing prose.
- **Logging:** `slog`, JSON, with `request_id`, `principal_id`, `tool`, `scope_path`.
- **SQL:** `sqlc`-generated queries over `pgx/v5`. No ORM. One transaction per request; audit
  rows are written by triggers in that same transaction, never by application code.
- **Comments** explain why, not what. The `Gotchas` above are the model.
- Keep new files in the idiom of the ones beside them.

## Tests

Behavioural changes need a test, **and the test must have been seen to fail before the fix**. A
regression test that has never failed is decoration. State in the PR's `### Verified` section
the exact commands you ran and where.

**Fail loudly, never skip.** If a test needs infrastructure it does not have, that is a failure
with a message naming the fix — not `t.Skip`. CI asserts that nothing skipped.

## Docs currency

If you change a surface, change its doc in the same PR:

| Surface | Doc |
|---|---|
| `cmd/**` | `README.md` |
| `Makefile`, `scripts/**`, `.github/workflows/**` | `CONTRIBUTING.md` |
| `migrations/**`, `deploy/**` | `docs/ops/runbook.md` |

`scripts/check-docs.sh` enforces this in CI. To skip, put `Docs-not-needed: <reason, 30+ chars>`
in the PR body — a real reason, not "n/a".

## Accessibility

No UI exists yet. The dashboard is a Later item; when it lands, this section gains a WCAG 2.2 AA
rule. Do not add UI without adding that section in the same PR.
