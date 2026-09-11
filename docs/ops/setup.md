# Setup and reference

Current pre-alpha commands and integration behavior. For the broader product direction, see
the [vision](../product/vision.md); for deployment and recovery, see the [runbook](runbook.md).

Requires Go 1.26.6+ (the floor in `go.mod`; earlier 1.26 patches carry stdlib CVEs that `govulncheck` fails CI on).

```sh
git clone https://github.com/agentic-substrate/substrate.git && cd substrate

make build     # binaries into ./bin
make smoke     # boots the real server against empty state and hits it
make check     # everything CI runs: fmt, vet, lint, race tests, govulncheck
```

## Start the server

Configure Postgres and the application role using the [runbook](runbook.md#which-role-goes-in-substrate_dsn).
Replace placeholders below with your own connection string and provisioned user UUID.
Run the server in one terminal:

```sh
./bin/substrate-server -addr :8080 -dsn 'postgres://…'
```

In another terminal, check health, create the first user, and save its credential. Set
`SUBSTRATE_DSN` to the connection string for the administrative operation. `create-user`
creates the org, team, principal, membership, and token in one transaction and prints that
token once.

```sh
curl localhost:8080/healthz
curl localhost:8080/readyz

./bin/substrate admin create-user --dsn "$SUBSTRATE_DSN" \
    --name ada --org acme --team platform --admin > token.txt

# The token is read from stdin, never argv, and stored in a mode-0600 file.
./bin/substrate auth login --server http://localhost:8080 < token.txt

# Save a default working scope and inspect which source won resolution.
./bin/substrate context use --org acme --team platform
./bin/substrate context show
./bin/substrate doctor

./bin/substrate token mint --for agent --parent <user-uuid> --machine wsl \
    --scopes memory:write --dsn "$SUBSTRATE_DSN"
```

Save the agent token printed by `token mint` as `TOKEN` and set `SUBSTRATE_URL` to the server
URL for the commands below. User tokens do not expire; agent tokens expire after 24 hours.
Revoke either kind with `substrate token revoke` when it is no longer needed.

### Operator command tree

Every command is a Cobra command; `-h` works on them and their subcommands, and all flags
take the `--flag` form.

This is the whole surface, and CI holds it to the binary: `scripts/check-cli-commands.sh`
builds `cmd/substrate` and compares the block below against its `--help` tree in both
directions, so a renamed or added verb cannot leave this page stale.

<!-- command-map:start -->
```
substrate                          # command map, exit 0, mutates nothing
substrate auth       login|status
substrate admin      create-user            # requires the DSN
substrate token      mint|revoke            # requires the DSN
substrate context    show|use
substrate import     scan|plan|apply
substrate review     list|approve|reject
substrate adapter    install|status|uninstall
substrate doctor
substrate completion bash|zsh|fish
```
<!-- command-map:end -->

`substrate-adapter` and `substrate-server` are separate binaries and stay on stdlib `flag`;
they are not subcommands of `substrate`.

| Command | Purpose |
|---|---|
| `substrate admin create-user` | Create or reuse the named org and team, then create a user, membership, and token. `--admin` grants `human_admin` trust and team-admin membership; the default is a `human` member. |
| `substrate auth login` | Read a token from stdin, validate it against the server, and write `config.json` mode 0600. |
| `substrate auth status` | Report the stored server and whether a token is present, without printing it. |
| `substrate context use` | Save an org, team, and optional project as the default scope. |
| `substrate context show` | Print the resolved scope and which source supplied it. |
| `substrate doctor` | Check configuration, credentials, server reachability, and scope, with a suggested fix for each failure. |
| `substrate import scan` / `plan` / `apply` | Inventory harness files, merge inventories into a reviewable plan, then send that plan to `POST /v1/import`. `scan` and `plan` write nothing; `apply` commits, and requires `--inventory` for every inventory the plan was built from. |
| `substrate review list` / `approve` / `reject` | List queued review items and record one decision. The decision is the verb, so there is no `--decision`; `--reason` is required on `reject`. |
| `substrate adapter status` / `install` / `uninstall` | Preview, apply, and roll back the per-machine adapter. `status` writes nothing; `install` and `uninstall` commit. |
| `substrate token mint` / `revoke` | Mint or revoke a principal token straight against Postgres. Requires the DSN. |
| `substrate completion` | Write a bash, zsh, or fish completion script to stdout, generated from this binary's own command tree so it cannot drift from the verbs above. |

These commands accept `--json` for machine-readable output. Explicit `--org`, `--team`, and
`--project` flags override the saved scope for one invocation.

Scope resolution uses the first available source: explicit flags; the saved context file; the
checkout's `origin` remote, sent to the server as a repo key; otherwise an error naming the next
command to run. There is no implicit global scope.

`${XDG_CONFIG_HOME:-~/.config}/substrate/` contains `config.json` and `context.json`. Both use
crash-safe atomic writes and mode 0600. Substrate refuses a credential file with any group or
other permission bit set. If `os.UserConfigDir()` fails, commands stop rather than guessing a
home directory. `substrate` with no arguments prints help; `substrate --version` prints the
version; an unknown command exits non-zero with a suggestion.

Shell completions are generated from that same command tree, so they never drift from it:

```sh
source <(./bin/substrate completion bash)
./bin/substrate completion zsh  > "${fpath[1]}/_substrate"
./bin/substrate completion fish > ~/.config/fish/completions/substrate.fish
```

## Import, review, and cut over

These examples use `wsl` as the trusted machine. `scan` and `plan` are read-only; `apply`,
`review approve`/`reject`, and the `adapter` verbs commit, and each echoes what it is about to
do and asks for confirmation first — there is no separate preview flag. The scan and plan output
paths must be outside scanned roots. Add repeatable
`--exclude` patterns and optional `--memorix-json /abs/path/memorix.json` to the scan as needed;
read the [exclusion guidance](#exclude-the-corpus-that-should-never-be-imported) before scanning.

```sh
./bin/substrate import scan --root /abs/machine-root --hostname wsl \
    --out /abs/path/inventory.json
./bin/substrate import plan --out /abs/path/plan.json /abs/path/inventory.json
./bin/substrate import apply --machine wsl --trusted wsl \
    --inventory /abs/path/inventory.json \
    --server "$SUBSTRATE_URL" --token "$TOKEN" \
    --scope 'global:/org:acme/team:core/project:plotlens' \
    /abs/path/plan.json

./bin/substrate review list --server "$SUBSTRATE_URL" --token "$TOKEN"
./bin/substrate review approve <id> --reason 'keep wsl' \
    --hostname wsl --as-kind instruction \
    --server "$SUBSTRATE_URL" --token "$TOKEN"
./bin/substrate review reject <id> --reason 'superseded by the mac copy' \
    --server "$SUBSTRATE_URL" --token "$TOKEN"

./bin/substrate adapter status --root /abs/home --root /work \
    --server "$SUBSTRATE_URL" --token "$TOKEN" --machine wsl
./bin/substrate adapter install --root /abs/home --root /work \
    --server "$SUBSTRATE_URL" --token "$TOKEN" --machine wsl
./bin/substrate adapter uninstall --root /abs/home --root /work [--force]
```

The decision is the verb: `review approve` and `review reject`, with no `--decision` flag.
`--reason` is required on `reject`. Cutting a machine over is `adapter install`, which displaces
harness files with rendered context and installs the unit in one step; `adapter status`
classifies what it would displace without writing. `adapter uninstall` restores every displaced
`*.pre-substrate` file and removes the unit — `--force` discards live edits that no longer match
the hash `install` wrote, which is the drift-recovery path in
[the runbook](runbook.md).

Point any MCP client at `POST /mcp` with that bearer token. Phase 1 exposes `context.get`, `memory.write`, `memory.search`, and `memory.supersede`. Agents always write `unverified`; supersede never deletes.

## Harness integration details

Rendering, skill linking, and `PostToolUse` capture ship today; `SessionStart` context injection and
continuity are designed and phased, not shipped.

The `PostToolUse` hook is installed by merging an entry into `~/.claude/settings.json` — a user-owned
file Substrate merges into rather than owns: the merge is idempotent, unrelated keys are
preserved, the pre-existing file is copied once to `settings.json.pre-substrate`, and `substrate
adapter uninstall` removes the entry surgically — everything else you have written since
stays, and the `.pre-substrate` copy is kept as your archive rather than written back over your
edits.

The shim (`substrate-adapter hook posttooluse`) reads the hook JSON on stdin, derives its scope from
the checkout's git remote, exits within 2 seconds, and always exits 0 — a capture failure never
fails the tool call, and a cwd whose remote is not bound is logged and skipped rather than filed
anywhere else.

Skills use the [Agent Skills](https://agentskills.io) `SKILL.md` format — Git holds skill content,
Substrate holds skill state (`skill_version.git_sha` is the only link). The adapter materializes the
active approved `git_sha` into `~/.agents/skills/<name>` and refreshes harness symlinks; a skill
whose scope or visibility does not match this machine is omitted from `GET /v1/skills/manifest` and
is not linked. `active_version_id` can only point at an approved version (Postgres trigger, R6).

## Server and API reference

`-dsn` (or `SUBSTRATE_DSN`) is the Postgres connection string. The server listens first. If a
DSN is set, it applies goose migrations under an advisory lock and retries in the background
rather than exiting. `/healthz` is **200** whenever the process is alive. `/readyz` returns
**200** only after migrations have applied and the pool can ping Postgres; otherwise **503**.
Without a DSN, `/readyz` is 503 `store not configured`. `/readyz` does not check Git or the
skills repo (EDD R27).

`-ollama` (or `SUBSTRATE_OLLAMA_URL`) points at Ollama for embeddings — `nomic-embed-text`, 768
dimensions, called directly from Go with no Python in the request path (EDD R21). Leave it empty
and retrieval is keyword-only, which is a supported configuration rather than a degraded one:
`memory.search` must never fail because embeddings are unavailable (MEM-5). When it is set, a
write whose embed call fails still succeeds with `embedding NULL` and is picked up by the
backfill pass. Semantic similarity is capped below the score of an exact identifier hit, so a
vector neighbour can never outrank a typed filename or symbol (MEM-3).

`-otlp` (or `SUBSTRATE_OTLP_ENDPOINT`) is the OTLP HTTP collector. Leave it empty and export is
disabled, which is a supported configuration the same way empty `-ollama` is keyword-only: the
collector being down, or never configured, must not fail a request. When set, the process pushes
metrics and traces best-effort; a failure is logged once, not per request. On SIGTERM, shutdown
waits up to 5s for the collector — a hang delays process exit, not in-flight requests. Metric names use the
`substrate_*` prefix. The adapter reports `substrate_outbox_depth` per
machine, including zero — a machine that stopped reporting drops the series rather than looking
like an empty queue. Phase 1 alert rules live in [`deploy/alerts.yaml`](../../deploy/alerts.yaml).

`POST /mcp` is the streamable-HTTP MCP endpoint (EDD §4.1). Bearer auth is required;
unauthenticated requests are rejected before the MCP handler runs. Rate limiting is Traefik's
job, not the process. `context.get` compiles instructions, preferences, mandatory items,
keyword-retrieved memories, and a skill index under the documented conservative token estimator;
instructions, preferences, and mandatory items are never trimmed.

`/v1` is the adapter and CLI REST surface (EDD §4.2), behind the same bearer auth as `/mcp` and
not exposed to harnesses. It serves `GET /v1/render`, `GET /v1/skills/manifest`,
`POST /v1/memory/batch`, `GET /v1/memory/cache`, review create/list/decide, `POST /v1/import`,
`GET /v1/events`
(SSE wake-ups when `substrate_audit` fires; the payload carries no audit metadata), and
`GET /v1/health/git`. `/v1/memory/batch` is the idempotency boundary: `ingest_receipt` and the
memory row are created in one transaction, and a replay with a known `client_id` returns the
original id with `duplicate: true`. Render `sha256` values are `render.DriftHash` of the content
(footer excluded). `POST /v1/import` consumes a `plan.json` (SYNC-5, EDD §9). The most-trusted
machine is applied first; only its non-conflict blocks that are not already byte-identical to
an active row become `active`. The first successful trusted commit stores a per-scope
marker; a later `-trusted` that names a different host is rejected, including when the
later host uses a different token. Every later machine can only add `proposed` rows and
`import_conflict` review items — it cannot promote anything to `active` or modify a row the
trusted machine established. Each conflict payload carries the applying `hostname` and the
pair sides' hostnames. Imported memory is always `episodic`/`unverified` even when the source
claimed `confirmed` (Gotcha 4). A replay of the same machine+plan is idempotent via
`ingest_receipt` keyed on `(machine, scope, plan-hash)`; an optional `client_id` is an
alias of that key, never a second write. `dry_run` is the default: the server classifies and returns the planned set
grouped by hostname without opening a write transaction. Writes require `"commit": true`.

Tokens are printed once and stored only as a SHA-256 hash. `token mint --for agent` is the only
accepted mint kind; user principals are created through `substrate admin create-user`.

### Exclude the corpus that should never be imported

`-exclude` takes a glob matched against each file's path relative to its root; `**` spans
separators, and the flag repeats. **Set it before the first import.** A home directory holds far
more `AGENTS.md`/`CLAUDE.md` files than it has real configuration: git worktree checkouts each
carry a copy of their repo's file, skill eval fixtures carry deliberately synthetic ones, and a
dependency directory like `.nvm` carries someone else's project entirely.

Measured on one real home (2026-09-08), the difference between scanning everything and scanning
only the primary checkouts:

| | Files | Blocks | Conflicts | Slots | Slots >1 block |
|---|---|---|---|---|---|
| No excludes | 56 | 861 | 79 | 301 | 136 |
| Excludes below | 18 | 459 | 22 | 126 | 69 |

Conflicts are the population a human has to key by hand, so the 79 → 22 drop is the number that
matters. Most of the excluded "conflicts" were worktree copies of one file disagreeing with each
other — noise that reads exactly like a real disagreement between two machines.

**Judge an exclude set by the file list and the conflict count, never the block count.** Blocks
dedupe by content hash and are attributed to the *first* file the walk reaches
(`BuildPlan`'s `byHash` map), so excluding a worktree copy hands its blocks back to the primary
checkout rather than deleting them. Widening the set above from six patterns to fourteen — a strict
superset — took blocks from 271 *up* to 459 while conflicts fell from 71 to 22. More exclusions,
more blocks, better corpus.

```sh
./bin/substrate import scan --root "$HOME" --hostname wsl --out /abs/path/inventory.json \
    --exclude '.nvm/**' \
    --exclude '**/node_modules/**' \
    --exclude '**/eval-*/**' \
    --exclude '**/fixtures/**' \
    --exclude 'worktrees/**' \
    --exclude '**/worktrees/**' \
    --exclude '**/.worktrees/**' \
    --exclude '**/.git-worktrees/**' \
    --exclude 'repos/*-worktrees/**' \
    --exclude 'repos/.wt/**' --exclude 'repos/.wt-*/**' \
    --exclude 'repos/*-wt/**' --exclude 'repos/*-wt-*/**' --exclude 'repos/wt-*/**'
```

### Verify the survivors by hand — the glob list will not catch everything

There is no universal worktree convention, and a missed one is **silent**: the copy is scanned,
its blocks dedupe against the primary checkout's, and the `rel` that survives is whichever file
the walk reached first.

On the home measured above, the exclude list still let two worktrees through — `plotlens-trivy-cve`
and `parley-shots-tshirt` — because they are named after their feature branch and carry no `wt` or
`worktree` token for a glob to match. Assume yours has some too. A worktree's `.git` is a *file*
containing a `gitdir:` pointer, not a directory, which is what distinguishes it from a primary
checkout:

```sh
jq -r '.files[].rel' /abs/path/inventory.json | sort -u        # what the scan kept
jq -r '.files[].path' /abs/path/inventory.json | while read -r f; do
    d=$(dirname "$f")
    [ -f "$d/.git" ] && echo "worktree copy, exclude it: $f"
done
```

Do that before `import apply`. Nothing downstream will tell you a worktree copy got imported.

### The plan is checked against the inventory

`import apply` takes `--inventory` (repeatable) naming every `inventory.json` the plan was
built from, and admits only blocks that inventory contains. `import plan` records the
inventory's digest in `plan.json`; apply re-derives the same set from the inventory files and
refuses, by name, any block, conflict side, or memory the scan did not observe — before it
sends anything. That is what makes "the operator supplied it" mean "the scanner saw it on
disk": on the trusted machine an imported instruction becomes **active**, which is to say a
directive rendered to agents, so a block nobody scanned must never get there.

This binds the plan to the scan, not to the disk at the moment of apply. An attacker who can
also rewrite `inventory.json`, edit the harness files before the scan runs, or call
`POST /v1/import` directly with a matching pair is not stopped by it — they control the scan
product itself. Keep both artifacts under the same trust as the machine they describe, and
regenerate rather than hand-edit a plan.

`substrate import scan` and `substrate import plan` are read-only (SYNC-5, EDD §9). Scan
requires `--root` (repeatable), `--hostname`, and `--out`; plan requires `--out` and one or
more inventory files. `-root` and `-out` must be absolute paths — there is no `$HOME`
default, and the command will not guess one. `-out` is rejected if it falls inside any
`-root` (scan) or equals any inventoried `Path` (plan), and the written file is always
mode `0600`, even when it already existed. Scan never writes, moves, or modifies
anything under the scanned roots; it only writes the file named by `-out`. It inventories
`.claude/CLAUDE.md`, `.codex/AGENTS.md`, `.cursor/rules/`, and any `AGENTS.md` /
`CLAUDE.md` found under those roots. Symlinks are not followed. Memorix is read only from
an operator-exported JSON file passed as `-memorix-json`; scan never execs `memorix`.
If that flag is omitted the source is skipped with a logged reason and the rest of the
scan still succeeds. An unreadable directory under a root is recorded in `skipped` and the
walk continues, so one permission error cannot abort a scan of a real home. Dependency and
plugin caches are never inventoried — a `CLAUDE.md` in the Go module cache or an `AGENTS.md`
in a vendored crate belongs to its upstream author, not to this machine. `--exclude <glob>`
(repeatable) drops anything matching a glob against the root-relative path, where `**` spans
separators; a pattern that is not a valid glob is an error rather than a filter that
silently matches nothing. Plan splits markdown into blocks, **dedupes by content hash**, then
classifies each unique block as `instruction` or `preference`. Classification is heuristic
and will misfile (R15); confidence is never certainty. Identical blocks from two machines
appear once in `plan.json`; differing blocks at the same heading from distinct hostnames
or paths are a conflict pair tagged with hostname. Extra hashes from a single file stay
in `blocks`. `substrate import apply` POSTs that plan to `/v1/import`. It
requires `--machine`, `--trusted` (the most-trusted hostname; apply it first),
`--server` (or `SUBSTRATE_URL`), `--token` (or `SUBSTRATE_TOKEN`), `--scope`, and
one `plan.json`. It echoes the resolved scope and its source, prints counts and
identities of the rows that will become active, proposed, conflict, and memory
grouped by hostname, and then asks for confirmation before writing. Each printed
row carries `scope=` and `visibility=`, so the destination is visible before you
confirm rather than after. Off a TTY — in a script or CI — it refuses without
`--yes`. Imported instructions are filed at the `--scope` leaf and are
team-visible; imported preferences are filed at the team scope with
`visibility=owner`, so a personal `~/.claude/CLAUDE.md` does not become readable
by the rest of the team on import.

`substrate adapter install` replaces local harness files with `GET /v1/render`
output, renames displaced files and the `.memorix` store to `*.pre-substrate`,
and installs the adapter unit (systemd `--user` on Linux/WSL, launchd on macOS)
with its roots set to the roots it displaced, so the daemon never renders over a
tree that has no `*.pre-substrate` backup; with no `--root` that is `/work`
(CONT-4). `--root` is repeatable and absolute, with no `$HOME` default; omit it
to default to `/work`. It also requires `--server` (or `SUBSTRATE_URL`),
`--token` (or `SUBSTRATE_TOKEN`), and `--machine`. Pass
`--root "$HOME" --root /work` so home harness files and checkout files under the
mount root are both displaced. Passing only `--root "$HOME"` does not add
`/work`. It echoes the roots and every file it will displace — each target's
current sha256 and the sha256 replacing it, every `*.pre-substrate` rename, and
the unit to be installed — then confirms; off a TTY it refuses without `--yes`.
`substrate adapter status` runs the same classification and writes nothing, which
is the way to inspect a machine without committing to it. An existing
`*.pre-substrate` path is an error — that file is the one copy of an earlier
install and is never overwritten. Nothing in this flow deletes (Gotcha 6);
originals are renamed. Rendered replacements are compared with
`render.DriftHash` (footer excluded).

`substrate adapter uninstall` is the rollback. `--root` is absolute and defaults
to `/work` when omitted, exactly as `install` does, so a rollback cannot walk a
different tree than the install it inverts. Pass the same roots `install` used
(`--root "$HOME" --root /work`); it does not discover extra trees. It renames
`*.pre-substrate` back over the live paths, removes generated files that still
match the written `DriftHash`, and uninstalls the unit. Live edits made since
install are refused unless `--force` is set (printed as `DISCARD live edits`).
It echoes every file it will restore or remove, then confirms. Run it before
blaming the server: server data is additive and can be left in place.

`substrate review list`, `substrate review approve` and `substrate review reject` are the CLI path through the
import queue (SYNC-5, R15). List calls `GET /v1/review` (default `status=open`) and
prints each item with its id, kind, slot, and both sides tagged by hostname so a
reviewer can choose from the listing. Long bodies are cut at a fixed rune budget
with an explicit `truncated` marker; a side whose hostname is missing is not
decidable in practice. `review approve` and `review reject` POST
`/v1/review/{id}/decide` — the decision is the verb, so there is no `--decision`
flag, and `--reason` is required on `reject`. Each echoes the item and confirms
before writing; off a TTY it refuses without `--yes`. For an `import_conflict`,
`--hostname` selects the winning machine and `--as-kind instruction|preference`
flips a heuristic misfile before approval (R15). A team lead or `human_admin`
decides; the proposer cannot self-approve.

`substrate-adapter` is the per-machine daemon (EDD §5). With no `-server` it reports its
version. With `-server` it pulls `GET /v1/render` on start, every 5 minutes, and on a
`GET /v1/events` SSE wake-up; writes managed files atomically; and posts a `drift_proposal`
(unified diff) *before* restoring a hand-edited file, at the scope of the target being
restored. A rejected or failed proposal degrades that one target — the local file is left
untouched and the loop continues. Comparisons use `render.DriftHash`
(footer excluded). Repo discovery scans `-roots` for any real directory holding a `.git`, up to
two levels below a root, so both the flat `<root>/<repo>` and the nested
`<root>/<project>/<repo>` layouts are found. A symlink under a root is deliberately not a
discovery candidate and is not descended into, so a symlink farm pointing at checkouts
elsewhere is invisible to the daemon; that is what makes the scan loop-free without a cycle
guard. Symlinks *inside* a discovered checkout are unaffected. A checkout's scope comes from
its git remote, never from where it sits on disk, so the same repo gets the same scope under
either layout. A checkout with no remote, an unparseable one, or one the server does not
bind to a repo scope is logged and skipped, and reported in the sync result
(`UnknownRemotes` / `UnscopedRemotes`) so an unmanaged machine is never silently healthy:
unknown remotes are never auto-created as scopes and their checkouts are left untouched.
A drift proposal about a file in a checkout names that checkout's repo key; the server
resolves the key to the chain it bound, so no client is ever told, nor gets to choose,
another team's org/team/project naming. Home-scoped targets such as `~/.claude/CLAUDE.md` have no repo,
so their proposals use `-scope`; without one, drift is reported but the file is not restored.

Each checkout is rendered independently. The adapter requests `GET /v1/render` for each known
remote and writes that response only to checkouts sharing that remote (SCOPE-1, INST-4). Home
targets such as `~/.codex/AGENTS.md`, `~/.claude/CLAUDE.md`, and
`~/.cursor/rules/substrate.mdc` come from a separate request without a repo and contain only
machine-wide context. Branch scope is not part of rendering yet; resolution stops at `repo`.

`-scope` has **no default**. It applies only to targets that have no repo of their own —
`~/.claude/CLAUDE.md` and outbox observations — and unset means those targets are skipped
and logged. It used to default to `global:`, which was worse than having no default: an
agent token may not write at global or org scope (`internal/policy`), so the two guards
that check for an unset scope never fired, and the work was filed and refused by the server
instead of being skipped locally.
Render targets are confined to the paths `render.Specs()` declares, under `-home` or a
discovered checkout, with symlinks resolved. `-home` is required with `-server` so the binary
cannot guess `$HOME` and overwrite pre-cutover harness files. State lives in
`<home>/.substrate/adapter.sqlite` (override with `-state`). The outbox drains
hook-captured observations to `POST /v1/memory/batch` in batches of at most 50, with a
UUIDv7 `client_id` assigned at enqueue and never regenerated on retry; a replayed id is
success (`duplicate: true`). The offline cache is an FTS5 delta of confirmed/probable
memory for remotes present on the machine, pulled from `GET /v1/memory/cache`. Hook-path
calls use a 1.5s server timeout and fall back to the outbox (writes) or the cache (reads)
so they return in under 2s. The skills loop runs on the same tick as render: `GET
/v1/skills/manifest`, `git fetch` of `-skills-repo` into `<home>/.substrate/skills.git`,
export of each approved `git_sha` into `<home>/.agents/skills/<name>`, and symlinks into
`.claude/skills`, `.codex/skills`, and `.cursor/skills`. A skills-repo outage keeps the last
linked versions and does not affect `/readyz`.

```sh
./bin/substrate-adapter -server https://cp.example -token "$TOKEN" \
  -home "$HOME" -roots /work -machine wsl -skills-repo git@github.com:org/skills.git
```

**Configuration:** `-addr` (listen address), `-dsn` / `SUBSTRATE_DSN`, `-ollama` /
`SUBSTRATE_OLLAMA_URL` (empty means keyword-only), `-otlp` / `SUBSTRATE_OTLP_ENDPOINT`
(OTLP HTTP collector; empty disables export), `-skills-repo` / `SUBSTRATE_SKILLS_REPO`
(skills git remote; probed by `GET /v1/health/git`, never by `/readyz`). Adapter flags:
`-server`, `-token` / `SUBSTRATE_TOKEN`, `-machine`, `-home`, `-state`, `-roots`, `-interval`
(default 5m), `-scope`, `-otlp` / `SUBSTRATE_OTLP_ENDPOINT`, `-skills-repo` /
`SUBSTRATE_SKILLS_REPO` (bare mirror
under `<home>/.substrate/skills.git`; empty skips linking). The rest of the
intended surface is specified in [`docs/design/edd.md`](../design/edd.md) §7 and §12 and gets
documented in this guide as it lands.
