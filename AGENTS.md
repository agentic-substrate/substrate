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
| `ACP_DENIED_SCOPE`, `ACP_NEEDS_REVIEW` | `SUBSTRATE_DENIED_SCOPE`, `SUBSTRATE_NEEDS_REVIEW` |
| `acp_audit`, `acp_tool_latency_seconds` | `substrate_audit`, `substrate_tool_latency_seconds` |
| database `acp`, buckets `acp-sessions` / `acp-backups` | `substrate`, `substrate-sessions` / `substrate-backups` |
| module `acp/` | `github.com/jacorbello/substrate` |

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
7. **`/readyz` checks Postgres only** (EDD R27). It currently returns **503 with
   `reason: store not configured`**, which is correct — there is no store yet. *Failure mode:*
   making Git or the skills repo part of readiness takes the whole service out of rotation for
   an outage it is designed to survive. When the store lands, flip both the handler and the
   assertion in `scripts/smoke.sh` in the same commit.
8. **Hooks must exit in under 2 seconds**, with a 1.5s server timeout and a fallback to the
   local cache or outbox. *Failure mode:* a slow server stalls every harness on every machine,
   and it looks like the harness is broken, not Substrate.
9. **Rendering must be byte-deterministic** — sorted keys, LF endings, no timestamps outside the
   footer, and the footer is excluded from the drift hash. *Failure mode:* every adapter cycle
   reports drift and the review queue fills with noise until people stop reading it.
10. **Outbox writes carry a `client_id` (UUIDv7) and the server keeps an `ingest_receipt`.**
    *Failure mode:* a retried batch after a network blip duplicates memories, and the duplicates
    then corroborate each other into promotion.

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
