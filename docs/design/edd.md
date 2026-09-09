# Engineering Design Document — Agent Control Plane

| | |
|---|---|
| **Status** | Reviewed (two rounds) — approved for Phase 1 implementation; all round-2 blockers resolved in text (§18.2) |
| **Version** | 1.1 |
| **Date** | 2026-09-05 |
| **Author** | Jeremy |
| **Language / runtime** | Go 1.23+ (core service, adapter, CLI); Python 3.12 sidecar (embeddings, LLM jobs) |
| **Companion docs** | `agent-control-plane-prd.md` v2, `agent-control-plane-design.md` v2 |
| **Scope of this EDD** | Phase 1 in implementation detail; Phases 2–3 at component level; Phases 4–7 as constraints on Phase 1 decisions |

---

## 1. Context and goals

This document specifies how to build the Phase 1 system defined in the PRD: one authoritative store for instructions, preferences, memory, and skill state; an MCP server exposing it to Claude Code, Codex, and Cursor; a per-machine adapter that renders local config as a cache; and a one-time reconciliation of drifted machines. It also fixes the decisions Phase 1 must not foreclose: symbol-level staleness, trust-weighted contradiction handling, session offload, tasks/leases, and multi-team OIDC.

Phase 1 exit criterion (from the PRD): identical generated config on WSL and the Mac Mini; import conflicts resolved through the review queue; a memory written on one machine is searchable from the other within 60 seconds.

### 1.1 Non-functional targets

| Property | Target | Rationale |
|---|---|---|
| `context.get` p95 latency | ≤ 400 ms (keyword) / ≤ 800 ms (with embeddings) | Runs on every session start and inside hooks |
| `memory.search` p95 | ≤ 300 ms | Interactive tool call |
| Availability | Best-effort single node; adapters must survive ≥ 24 h server outage without data loss | Homelab, no HA |
| Scale (Phase 1) | ≤ 5 humans, ≤ 20 concurrent agents, ≤ 100k memories, ≤ 10k instructions | Guides index and pool choices |
| Scale ceiling before redesign | ~50 agents, ~1M memories | Postgres-only coordination stays viable well past Phase 1 |
| Data durability | Nightly base backup + WAL to MinIO; RPO ≤ 24 h Phase 1, ≤ 5 min by Phase 4 | |
| Security | No cross-team read possible even with a service-layer bug (RLS); no plaintext transcripts at rest | |

---

## 2. Architecture overview

```
                 Tailscale → Traefik v3 + cert-manager (TLS, auth passthrough)
                                       │
        ┌──────────────────────────────┴──────────────────────────────┐
        │                        cp-server (Go)                       │
        │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────────┐  │
        │  │ MCP      │ │ REST     │ │ Compiler │ │ Jobs           │  │
        │  │ /mcp     │ │ /v1/*    │ │          │ │ (cron in-proc) │  │
        │  └────┬─────┘ └────┬─────┘ └────┬─────┘ └───────┬────────┘  │
        │       └────────────┴──────┬─────┴───────────────┘           │
        │                     domain packages                          │
        │   scope · instruction · preference · memory · skill · review │
        │   audit · identity · policy · render · gitwatch              │
        │                           │                                  │
        │                    store (pgx + sqlc)                        │
        └───────────────────────────┬──────────────────────────────────┘
                                    │
      ┌─────────────┬───────────────┼────────────────┬─────────────────┐
  PostgreSQL 16    Git (skills)   MinIO           Ollama (T4)      jobs-sidecar (Py)
  CloudNativePG    bare repo /    transcripts,    embeddings —     summarization, LLM
  on Longhorn      Gitea          backups         called DIRECT    labeling (Phase 2+)
  pgvector, RLS                                   from Go in Ph.1

  Clients: cp-adapter (Go daemon, per machine) · cp CLI (Go) · harnesses via MCP + hooks
```

### 2.1 Repository layout (single Go module, mono-repo)

```
acp/
  cmd/
    cp-server/       main: MCP + REST + jobs
    cp-adapter/      per-machine daemon
    cp/              CLI (import, review, offload, doctor)
  internal/
    scope/           chain resolution, path parsing
    instruction/     resolve-by-key, propose
    preference/
    memory/          write policy, search, supersede, feedback
    skill/           Postgres state + git checkout mgmt
    compiler/        context pack builder + explain
    render/          AGENTS.md / CLAUDE.md / .mdc templates
    identity/        principals, tokens, (later) OIDC
    policy/          write/promotion/contradiction rules
    review/
    audit/           trigger-backed; helpers to read history
    gitwatch/        staleness (file-level now, tree-sitter later)
    embed/           client for sidecar/Ollama
    store/           sqlc-generated queries, migrations (goose)
    mcpx/            MCP tool registration, auth middleware
    outbox/          adapter-side SQLite queue
  migrations/        NNNN_*.sql (goose)
  skills/            (separate git repo; not in module)
  deploy/            k8s manifests, NGINX conf, systemd/launchd units
  sidecar/           Python embed + jobs service
```

### 2.2 Key libraries

| Concern | Choice | Note |
|---|---|---|
| MCP | `github.com/modelcontextprotocol/go-sdk/mcp` v1.7+ | Official SDK; streamable HTTP transport with typed tool handlers. Stream resumption stays disabled (default) |
| Postgres | `pgx/v5` + `sqlc` | Typed queries; no ORM. Connection pool 10–20 |
| Migrations | `goose` | SQL migrations checked in; run at server start with advisory lock |
| Vectors | `pgvector` ≥ 0.7 | HNSW index; `vector(768)` for nomic-embed-text, switchable |
| Full text | `tsvector` + `pg_trgm` | Trigram for identifiers, tsvector for prose |
| Git | `go-git` for reads; shell out to `git` for clone/pull/push in adapter | go-git handles bare-repo inspection without a checkout |
| Tokenizer | `tiktoken-go` (cl100k as proxy) | Budget accounting; exact harness tokenizer is unavailable, 10% headroom applied |
| Config | `koanf` (YAML + env) | |
| Logging/metrics | `slog` + OpenTelemetry (OTLP → existing collector) | |
| Adapter local store | `modernc.org/sqlite` (pure Go) | No CGO on WSL/macOS/pods |
| Templates | `text/template` | Deterministic rendering; golden-file tests |
| CLI | `cobra` | |

---

## 3. Data model (Phase 1 DDL)

Conventions: UUIDv7 primary keys (`gen_uuid_v7()` via `pg_uuidv7` or app-generated); `timestamptz` everywhere; every mutable table has `created_at`, `updated_at`, `created_by`; enums as Postgres `ENUM` types; soft state via `status`, never `DELETE` on domain tables.

### 3.1 Identity and scopes

```sql
CREATE TYPE visibility     AS ENUM ('owner','team','org','global');
CREATE TYPE principal_kind AS ENUM ('user','agent');
CREATE TYPE trust_level    AS ENUM ('human_admin','human','agent_interactive','agent_autonomous');

CREATE TABLE principal (
  id            uuid PRIMARY KEY,
  kind          principal_kind NOT NULL,
  display_name  text NOT NULL,
  trust         trust_level NOT NULL,
  minted_by     uuid REFERENCES principal(id),          -- required when kind='agent'
  disabled_at   timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  CHECK (kind <> 'agent' OR minted_by IS NOT NULL)
);

CREATE TABLE org  (id uuid PRIMARY KEY, name text UNIQUE NOT NULL);
CREATE TABLE team (id uuid PRIMARY KEY, org_id uuid NOT NULL REFERENCES org(id), name text NOT NULL, UNIQUE(org_id,name));

CREATE TYPE member_role AS ENUM ('member','lead','admin');
CREATE TABLE membership (
  principal_id uuid NOT NULL REFERENCES principal(id),
  team_id      uuid NOT NULL REFERENCES team(id),
  role         member_role NOT NULL,
  PRIMARY KEY (principal_id, team_id)
);

CREATE TYPE scope_kind AS ENUM ('global','org','team','project','repo','branch','task','session','user');
CREATE TABLE scope (
  id         uuid PRIMARY KEY,
  kind       scope_kind NOT NULL,
  parent_id  uuid REFERENCES scope(id),
  key        text NOT NULL,                 -- human key: 'plotlens', 'api', 'feature/x'; NOT used in ltree
  team_id    uuid REFERENCES team(id),      -- denormalized owning team for project+ scopes (RLS speed)
  depth      smallint NOT NULL,             -- 0=global … 7=session; user=0 on its own root
  path       ltree NOT NULL,                -- labels are 'k' || replace(id::text,'-','') per ancestor; never derived from key
  UNIQUE NULLS NOT DISTINCT (parent_id, kind, key)   -- PG16: roots (parent_id NULL) are unique too
);
CREATE INDEX scope_path_gist ON scope USING gist (path);
CREATE UNIQUE INDEX scope_single_global ON scope ((true)) WHERE kind='global';

-- Git remote → repo scope binding. Adapter sends the normalized remote; server resolves here.
CREATE TABLE repo_remote (
  remote_norm   text PRIMARY KEY,           -- 'github.com/corbello/plotlens-api' (scheme/user/.git stripped, lowercased)
  repo_scope_id uuid NOT NULL REFERENCES scope(id),
  created_by    uuid NOT NULL REFERENCES principal(id),
  created_at    timestamptz NOT NULL DEFAULT now()
);
-- Unknown remotes are NOT auto-created as scopes; the adapter surfaces them and `cp repo bind` creates the row.

-- Physical checkout identity. Applicability stays branch-scoped; coordination knows which checkout observed what.
CREATE TABLE workspace (
  id              uuid PRIMARY KEY,
  repo_scope_id   uuid NOT NULL REFERENCES scope(id),
  branch_scope_id uuid NOT NULL REFERENCES scope(id),
  principal_id    uuid NOT NULL REFERENCES principal(id),
  machine         text NOT NULL,
  worktree_path   text NOT NULL,
  git_sha         text,
  dirty           boolean NOT NULL DEFAULT false,
  last_seen_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (machine, worktree_path)
);
-- memory.source carries workspace_id from Phase 1 so later phases can detect collisions vs collaboration.

CREATE TABLE project_grant (
  project_scope_id uuid NOT NULL REFERENCES scope(id),
  team_id          uuid NOT NULL REFERENCES team(id),
  role             member_role NOT NULL,
  PRIMARY KEY (project_scope_id, team_id)
);

-- Administrative authority (deterministic):
--   org/global admin  := principal.trust = 'human_admin'            (sets acp.is_admin = true)
--   team admin/lead   := membership.role IN ('admin','lead')         (team-scoped powers only)
-- membership.role='admin' never grants org/global power; trust='human_admin' is the only path.

CREATE TABLE api_token (               -- Phase 1 auth; replaced by OIDC in Phase 7
  id           uuid PRIMARY KEY,
  principal_id uuid NOT NULL REFERENCES principal(id),
  machine      text NOT NULL,
  token_hash   bytea NOT NULL UNIQUE,   -- sha256 of random 32 bytes
  scopes       text[] NOT NULL,         -- capability strings, e.g. 'memory:write','instruction:propose'
  expires_at   timestamptz,
  revoked_at   timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now()
);
```

Scope chain invariant enforced by trigger: `parent.kind` must be the immediate predecessor of `child.kind` in `global→org→team→project→repo→branch→task→session`; `user` scopes have no parent and no children. `path` is maintained by the same trigger from the parent's path plus the row's own UUID label, so human keys may contain `/`, `.`, or spaces without affecting ltree.

### 3.2 Visibility (shared columns)

Every content table carries:

```sql
scope_id    uuid NOT NULL REFERENCES scope(id),
visibility  visibility NOT NULL,           -- ENUM ('owner','team','org','global')
owner_id    uuid NOT NULL REFERENCES principal(id),
```

### 3.3 Instructions and preferences

```sql
CREATE TYPE instruction_kind   AS ENUM ('rule','constraint','convention');
CREATE TYPE instruction_status AS ENUM ('active','proposed','retired');

CREATE TABLE instruction (
  id uuid PRIMARY KEY,
  scope_id uuid NOT NULL REFERENCES scope(id),
  visibility visibility NOT NULL, owner_id uuid NOT NULL REFERENCES principal(id),
  kind instruction_kind NOT NULL,
  key  text NOT NULL,                       -- 'python.version', 'deploy.branch_policy'
  body text NOT NULL,
  status instruction_status NOT NULL DEFAULT 'active',
  superseded_by uuid REFERENCES instruction(id),
  created_by uuid NOT NULL REFERENCES principal(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX instruction_active_key ON instruction (scope_id, key) WHERE status='active';

CREATE TABLE preference (
  id uuid PRIMARY KEY,
  scope_id uuid NOT NULL REFERENCES scope(id),   -- user, team, or org scope only (CHECK via trigger)
  visibility visibility NOT NULL, owner_id uuid NOT NULL REFERENCES principal(id),
  key text NOT NULL, body text NOT NULL,
  status instruction_status NOT NULL DEFAULT 'active',
  created_by uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX preference_active_key ON preference (scope_id, key) WHERE status='active';
```

### 3.4 Memory

```sql
CREATE TYPE memory_tier   AS ENUM ('working','episodic','semantic');
CREATE TYPE memory_kind   AS ENUM ('fact','decision','incident','lesson','observation');
CREATE TYPE memory_status AS ENUM ('confirmed','probable','unverified','conflicted','deprecated','superseded');
CREATE TYPE verification_type AS ENUM ('code','human','agent_inference');

CREATE TABLE memory (
  id uuid PRIMARY KEY,
  scope_id uuid NOT NULL REFERENCES scope(id),
  visibility visibility NOT NULL, owner_id uuid NOT NULL REFERENCES principal(id),
  tier memory_tier NOT NULL, kind memory_kind NOT NULL,
  title text NOT NULL, body text NOT NULL,
  identifiers text[] NOT NULL DEFAULT '{}',      -- extracted paths, symbols, error strings, issue ids
  embedding vector(768),
  fts tsvector GENERATED ALWAYS AS (to_tsvector('english', title || ' ' || body)) STORED,
  status memory_status NOT NULL DEFAULT 'unverified',
  superseded_by uuid REFERENCES memory(id),
  source jsonb NOT NULL,                          -- {agent, machine, project, task, commit, session}
  verification jsonb NOT NULL,                    -- {type, repo, commit, symbols[], config_keys[], schemas[]} | {type:'human', principal, at} | {type:'agent_inference', agent, session}
  verification_type verification_type GENERATED ALWAYS AS ((verification->>'type')::verification_type) STORED,
  valid_from_commit text, last_verified_at timestamptz, stale_reason text,
  retrieved_count int NOT NULL DEFAULT 0, useful_count int NOT NULL DEFAULT 0, incorrect_count int NOT NULL DEFAULT 0,
  created_by uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (jsonb_typeof(source)='object' AND source ? 'machine'),
  CHECK (verification ? 'type')
);
CREATE INDEX memory_embedding_hnsw ON memory USING hnsw (embedding vector_cosine_ops) WITH (m=16, ef_construction=64);
CREATE INDEX memory_fts ON memory USING gin (fts);
CREATE INDEX memory_identifiers_trgm ON memory USING gin (array_to_string(identifiers,' ') gin_trgm_ops);
CREATE INDEX memory_default_read ON memory (scope_id, tier, status) WHERE tier='semantic' AND status IN ('confirmed','probable');

-- Server-side idempotency for outbox drains and hook writes. Receipt + subject are created in ONE transaction;
-- a retry with a known client_id returns the original subject_id and writes nothing.
CREATE TABLE ingest_receipt (
  client_id    uuid PRIMARY KEY,
  principal_id uuid NOT NULL REFERENCES principal(id),
  subject_type text NOT NULL,                -- 'memory' | 'checkpoint' | 'review_item'
  subject_id   uuid NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE edge_relation AS ENUM ('supersedes','contradicts','caused_by','relates_to','uses_skill','derived_from');
CREATE TABLE memory_edge (
  from_id uuid NOT NULL REFERENCES memory(id), to_id uuid NOT NULL REFERENCES memory(id),
  relation edge_relation NOT NULL, created_by uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (from_id, to_id, relation)
);

CREATE TABLE memory_feedback (
  id uuid PRIMARY KEY, memory_id uuid NOT NULL REFERENCES memory(id),
  principal_id uuid NOT NULL REFERENCES principal(id), trust trust_level NOT NULL,
  useful boolean, incorrect boolean, reason text, session_id text,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (useful IS NOT NULL OR incorrect IS NOT NULL)
);
```

Feedback aggregates (`useful_count`, `incorrect_count`) are updated by trigger with trust weights: `human_admin`=3, `human`=2, `agent_interactive`=1, `agent_autonomous`=0.25.

### 3.5 Skills

```sql
CREATE TYPE approval AS ENUM ('proposed','approved','rejected');
CREATE TABLE skill (
  id uuid PRIMARY KEY, name text NOT NULL,                     -- 'team/plotlens/validation-regression'
  scope_id uuid NOT NULL REFERENCES scope(id),
  visibility visibility NOT NULL, owner_id uuid NOT NULL,
  description text NOT NULL, tags text[] NOT NULL DEFAULT '{}', embedding vector(768),
  active_version_id uuid,                                      -- FK added after skill_version exists
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (name)
);
CREATE TABLE skill_version (
  id uuid PRIMARY KEY, skill_id uuid NOT NULL REFERENCES skill(id),
  semver text NOT NULL, git_sha text NOT NULL, git_path text NOT NULL,   -- 'skills/team/plotlens/validation-regression'
  author_id uuid NOT NULL, reason text, source_task text,
  approval approval NOT NULL DEFAULT 'proposed',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (skill_id, semver)
);
ALTER TABLE skill ADD FOREIGN KEY (active_version_id) REFERENCES skill_version(id);
```

Invariant (trigger): `active_version_id` may only point at a version with `approval='approved'`.

### 3.6 Review and audit

```sql
CREATE TYPE review_kind   AS ENUM ('promotion','instruction_change','skill_proposal','contradiction','drift_proposal','import_conflict');
CREATE TYPE review_status AS ENUM ('open','approved','rejected','withdrawn');
CREATE TABLE review_item (
  id uuid PRIMARY KEY, kind review_kind NOT NULL,
  scope_id uuid NOT NULL REFERENCES scope(id), team_id uuid REFERENCES team(id),
  payload jsonb NOT NULL,                       -- kind-specific; always includes subject ids + proposed diff
  proposed_by uuid NOT NULL REFERENCES principal(id),
  status review_status NOT NULL DEFAULT 'open',
  decided_by uuid REFERENCES principal(id), decided_at timestamptz, reason text,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX review_open ON review_item (team_id, kind) WHERE status='open';

CREATE TABLE audit (
  id bigserial PRIMARY KEY, at timestamptz NOT NULL DEFAULT now(),
  actor_id uuid, action text NOT NULL,           -- 'memory.write','instruction.retire','skill.activate',…
  subject_type text NOT NULL, subject_id uuid NOT NULL, scope_id uuid,
  before jsonb, after jsonb, reason text, request_id text
);
CREATE INDEX audit_subject ON audit (subject_type, subject_id, at DESC);
CREATE INDEX audit_scope_at ON audit (scope_id, at DESC);
```

Audit rows are written by `AFTER INSERT OR UPDATE` triggers on every domain table, reading `current_setting('acp.actor_id')` and `current_setting('acp.request_id')` that the service sets per transaction. A `pg_notify('acp_audit', json)` fires from the same trigger; the server's broadcast loop and pack-cache invalidation subscribe to it. The audit table is `INSERT`-only at the role level (`REVOKE UPDATE, DELETE`).

### 3.7 Row-level security

The service connects as role `acp_app` with RLS enforced (`FORCE ROW LEVEL SECURITY` on all content tables). Per request the server sets:

```sql
SET LOCAL acp.actor_id = '<principal uuid>';
SET LOCAL acp.team_ids = '{<team uuids>}';
SET LOCAL acp.org_id   = '<org uuid>';
SET LOCAL acp.granted_project_ids = '{<project scope uuids via project_grant for the caller's teams>}';
SET LOCAL acp.is_admin = 'false';   -- true only when principal.trust = 'human_admin'
```

Policy shape (identical on `instruction`, `preference`, `memory`, `skill`; `review_item` uses `team_id`):

```sql
-- Applicability (scope ancestry) and authorization (below) are computed independently.
-- Decision: visibility='team' means the OWNING team OR any team holding a project_grant on the
-- enclosing project (option B). Grants are resolved once per request into acp.granted_project_ids.
CREATE POLICY content_read ON memory FOR SELECT USING (
     visibility = 'global'
  OR (visibility = 'org'   AND scope_org(scope_id) = current_setting('acp.org_id')::uuid)
  OR (visibility = 'team'  AND (
        scope_team(scope_id) = ANY (current_setting('acp.team_ids')::uuid[])
     OR scope_project(scope_id) = ANY (current_setting('acp.granted_project_ids')::uuid[])))
  OR (visibility = 'owner' AND owner_id = current_setting('acp.actor_id')::uuid)
  OR current_setting('acp.is_admin')::boolean
);
CREATE POLICY content_write ON memory FOR INSERT WITH CHECK (
  owner_id = current_setting('acp.actor_id')::uuid AND scope_writable(scope_id, current_setting('acp.actor_id')::uuid)
);
```

`scope_org`, `scope_team`, `scope_project`, `scope_writable` are `STABLE` SQL functions over `scope.team_id`/`scope.path` and `membership`; `scope_writable` also honors `project_grant.role >= 'member'` for team-scope writes on granted projects. RLS tests (§14) attempt reads and writes as every role combination and are a CI gate.

---

## 4. API design

### 4.1 MCP tools (`/mcp`, streamable HTTP)

Tool handlers are typed with the Go SDK (`mcp.AddTool(server, &mcp.Tool{...}, handler)`); input/output structs carry `jsonschema` tags so schemas are generated, not hand-written. Tool names use dots as in the PRD; the SDK permits `[a-zA-Z0-9_.-]`.

| Tool | Input | Output | Phase |
|---|---|---|---|
| `context.get` | `{task_id?, repo?, branch?, files?[], budget_tokens?}` | `{pack_id, markdown, sections{name,tokens}[], items{id,type,status}[]}` | 1 (keyword), 2 (ranked) |
| `context.explain` | `{pack_id, item_id?}` | `{items{id, included, reasons[], scores{}}[]}` | 2 |
| `memory.search` | `{query, scope?, tier?, status?[], limit?}` | `{results{id,title,snippet,status,scope,score}[]}` | 1 |
| `memory.write` | `{kind, title, body, scope, visibility?, identifiers?[], verification, source?}` | `{id, status}` | 1 |
| `memory.supersede` | `{old_id, body, reason}` | `{id}` | 1 |
| `memory.feedback` | `{id, useful?, incorrect?, reason?}` | `{ok}` | 2 |
| `instruction.propose` | `{scope, kind, key, body, reason}` | `{review_id}` | 1 |
| `skill.find` | `{query, limit?}` | `{skills{name,description,scope}[]}` | 1 |
| `skill.load` | `{name}` | `{markdown, files[]}` | 1 |
| `skill.propose` | `{name, scope, description, files{path,content}[], reason}` | `{review_id, pr_url}` | 5 |
| `task.*`, `handoff.*` | — | — | 6 |

Tool descriptions are written for the model: short, imperative, with when-to-use guidance ("Call `memory.feedback` after using a retrieved memory that proved wrong; include the commit that changed it if known").

Error mapping: policy denials return `isError: true` with a machine-readable code in `content[0].text` (`ACP_DENIED_SCOPE`, `ACP_DENIED_VISIBILITY`, `ACP_NEEDS_REVIEW`) so hooks can branch without parsing prose.

### 4.2 REST (`/v1`, for adapter and CLI)

JSON over HTTPS, same bearer auth. Not exposed to harnesses.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/render?machine=&repos=a,b` | Effective instruction+preference set rendered per target: `{targets{path,content,sha256}[]}` |
| `GET` | `/v1/skills/manifest?repos=` | Skills the machine should link: `{skills{name,git_path,git_sha}[]}` |
| `POST` | `/v1/memory/batch` | Outbox drain: array of `memory.write` payloads, each with a required `client_id`; server checks `ingest_receipt` and returns the original `subject_id` for known ids (200, `duplicate:true`) |
| `GET` | `/v1/memory/cache?repos=&since=` | Delta of confirmed/probable memories for offline cache |
| `POST` | `/v1/review` | Create drift/import review items |
| `GET` | `/v1/review?team=&status=open` | List |
| `POST` | `/v1/review/{id}/decide` | Approve/reject with reason |
| `GET` | `/v1/events` | SSE of `acp_audit` notifications (adapter triggers re-render on instruction/skill changes) |
| `POST` | `/v1/import` | Reconciliation upload (§9) |
| `GET` | `/healthz`, `/readyz`, `/v1/health/git` | Liveness; readiness checks Postgres only (core state). Git/skills-repo reachability is reported on `/v1/health/git` and as a metric — a Git outage must not stop routing |

### 4.3 Authentication and authorization

- **Phase 1:** `Authorization: Bearer <token>`; server hashes with SHA-256, looks up `api_token`, loads principal + memberships, sets RLS GUCs. Tokens are per (principal, machine), 32 random bytes, shown once, stored hashed. `cp token mint --for agent --parent <user> --scopes memory:write,...` creates agent principals bound to a user.
- **Capability check** happens in Go (`policy` package) before the SQL; RLS is the backstop, not the primary check, so error messages stay meaningful.
- **Phase 7:** OIDC bearer JWTs validated against the provider's JWKS; claims → principal + team memberships (synced on first sight). The `api_token` path remains for agents.
- NGINX terminates TLS (Tailscale cert or internal CA), passes `Authorization` through, and rate-limits `/mcp` per token (60 rpm default).

---

## 5. Context compiler

### 5.1 Inputs and resolution

```go
type Request struct {
    Principal   identity.Principal
    Scope       scope.Path      // resolved from repo/branch/task; falls back to project
    TaskID      *uuid.UUID
    Files       []string        // touched/relevant paths from the hook
    Budget      Budget          // per-section token caps
}
```

Scope resolution: the adapter/hook supplies `repo` (git remote URL normalized) and `branch`; the server maps remote → `repo` scope, branch → existing `branch` scope or creates one on first write (branch scopes are cheap and pruned when the branch is deleted).

### 5.2 Algorithm

```
pack := new Pack
chain := scope.Ancestors(req.Scope)                 // session … global, most specific first

// 1. instructions: deterministic, first active per key wins
seen := set{}
for s in chain: for ins in ActiveInstructions(s): if !seen[ins.Key] { pack.Instructions += ins; seen.add(ins.Key) }

// 2. preferences: user > team(s) > org, minus keys claimed above
for s in [userScope, teamScopes..., orgScope]: for p in ActivePreferences(s):
    if seen[p.Key] { pack.Suppressed += (p.Key, byInstruction) } else if !prefSeen[p.Key] { pack.Preferences += p }

// 3. mandatory: open handoff for task, active broadcasts for chain, open review items on chain (count + titles only)

// 4. candidates (Phase 1: FTS + trigram on files/task text; Phase 2: + vector + ranking)
cands := memory.Search(chain, req.Files, taskText, limit=200)  // RLS filters visibility; default read = semantic & confirmed/probable
skills := skill.Find(taskText, chain, limit=20)

// 5. score (Phase 2) — Phase 1 uses ts_rank + identifier hit bonus only
score = relevance * recency(halflife=60d) * statusW{confirmed:1.0, probable:0.8, stale:*0.6} * specificity(1+0.1*depth) * feedback(floor=0.5)

// 6. pack under budgets: sections 1–3 are never trimmed; if they alone exceed budget, return error ACP_BUDGET_TOO_SMALL
//    memory: greedy by score until section cap; skills: index lines only
// 7. render markdown with per-item footer; 8. persist context_pack row + explain rows (Phase 2)
```

Budgets (defaults, overridable per request): instructions 1,500 · preferences 500 · mandatory 1,500 · memory 6,000 · skill index 2,000 · task 3,000. Token counts use a **conservative estimator**, not the harness tokenizer (Claude's is not public): `estimate = max(tiktoken_cl100k(text), bytes/3)` × 1.15. Acceptance is stated against this estimator (PRD CTX-2 amended). Phase 2 may calibrate the multiplier per harness using Anthropic's token-count endpoint on sampled packs.

### 5.3 Caching

`context_pack(id, principal_id, scope_id, task_id, git_sha, inputs_hash, markdown, created_at)`. Cache hit requires identical `inputs_hash` (scope path + files + budget) and no `acp_audit` notification touching the chain since `created_at`. Phase 1 may skip caching; the table exists so `explain` has an anchor.

---

## 6. Rendering (`render` package)

One canonical intermediate (`EffectiveConfig{Instructions, Preferences, Suppressed, Skills}`) rendered by target:

| Target | Path | Notes |
|---|---|---|
| Codex / Cursor / generic | `<repo>/AGENTS.md`, `~/.codex/AGENTS.md` | Canonical markdown: `## Rules` (grouped by key prefix), `## Conventions`, `## Preferences`, footer with `sha256` and generated-at |
| Claude Code | `<repo>/CLAUDE.md` = `@AGENTS.md` + `## Claude-specific` block (hooks reminders); `~/.claude/CLAUDE.md` same shape | Uses Claude's `@import` |
| Cursor global | `~/.cursor/rules/acp.mdc` | Frontmatter `alwaysApply: true`; same body |
| Skills | `~/.agents/skills/<name>/` → symlinks into `~/.claude/skills/`, `~/.codex/skills/`, `~/.cursor/skills/` | Directory names must satisfy Agent Skills naming |

Rendering is byte-deterministic: sorted keys, LF endings, no timestamps except in the footer comment, which is excluded from the drift hash. Golden-file tests cover every target.

---

## 7. cp-adapter

Single static binary; runs as `systemd --user` on WSL/Linux, `launchd` agent on macOS, sidecar container in pods.

### 7.1 State

`~/.acp/adapter.sqlite`:

```sql
CREATE TABLE managed_file (path TEXT PRIMARY KEY, target TEXT, sha256 TEXT, rendered_at INTEGER);
CREATE TABLE outbox (id INTEGER PRIMARY KEY, client_id TEXT UNIQUE, payload TEXT, created_at INTEGER, attempts INTEGER DEFAULT 0, last_error TEXT);
CREATE TABLE memory_cache (id TEXT PRIMARY KEY, scope_path TEXT, title TEXT, body TEXT, identifiers TEXT, status TEXT, updated_at INTEGER);
CREATE VIRTUAL TABLE memory_cache_fts USING fts5(title, body, identifiers, content='memory_cache', content_rowid='rowid');
CREATE TABLE skill_link (name TEXT PRIMARY KEY, git_sha TEXT, linked_paths TEXT);
```

### 7.2 Loops

| Loop | Trigger | Behavior |
|---|---|---|
| `render` | start · 5 min · SSE event on instruction/preference/skill · `cp adapter sync` | `GET /v1/render`; for each target: if file exists and its content hash ≠ `managed_file.sha256` → **drift**: `POST /v1/review` with unified diff (`kind=drift_proposal`) then overwrite; else write atomically (temp + rename). Repos discovered by scanning configured roots two levels deep (`/work/<project>/<repo>`, i.e. `/work/*/*`, plus any extra roots in adapter config) for `.git` and reading `origin`; each discovered checkout upserts a `workspace` row. |
| `skills` | same | `git fetch` skills repo (bare mirror under `~/.acp/skills.git`), `GET /v1/skills/manifest`, `git worktree`/export each approved `git_sha` into `~/.agents/skills/<name>`, refresh symlinks, remove links for skills no longer in manifest. |
| `outbox` | hook write · 60 s | Batch ≤ 50 rows → `POST /v1/memory/batch`; delete on 2xx; exponential backoff (max 10 min) on failure; `client_id` = UUIDv7 for idempotency. |
| `cache` | 15 min | `GET /v1/memory/cache?since=` delta into `memory_cache`; used by local `context.get` fallback when server unreachable (adapter exposes a stdio MCP shim `cp-adapter mcp` that proxies to the server and falls back to cache). |
| `health` | 60 s | Heartbeat to `/v1/presence` (Phase 7); Phase 1 logs only. |

### 7.3 Hook shims

`cp hook <event>` reads the harness's JSON on stdin and:
- **Phase 1 (CAP-1a-capture, shipped):** `substrate-adapter hook posttooluse` appends a compact observation to `outbox` (tool, files, exit status, ≤ 500 chars). It is installed by merging a `PostToolUse` entry into `~/.claude/settings.json` — a user-owned file, so the merge is idempotent, preserves unrelated keys, copies the pre-existing file once to `*.pre-substrate`, and is undone by `substrate adapter uninstall`. The payload names the checkout's **repo key** (the normalized `origin` remote) rather than a scope; the server resolves the key to the chain it bound and never returns that chain, exactly as `POST /v1/review` does (R18, #58). A cwd with no remote, an unparseable remote, or one the server has not bound is logged and skipped — there is no `global:` fallback, because no agent token may write there. This is the minimum capture needed to prove cross-machine sync (PRD SYNC-2).
- **Phase 2 (CAP-1a-context, deferred):** `SessionStart` → `context.get` via server (or cache) → prints pack to stdout for injection. Deferred because no client path to `context.get` exists outside MCP and the offline cache holds memories, not instruction packs; Claude Code already auto-loads the rendered `CLAUDE.md`, so the outstanding value is retrieved memories and mandatory items.
- **Phase 2:** `Stop` / `PreCompact` → server-side summarization to `episodic/unverified` plus a checkpoint (PRD CAP-1b, CONT-1). Not implemented in Phase 1 binaries.

Hooks must exit in < 2 s; any server call uses a 1.5 s timeout and falls back to outbox/cache.

---

## 8. cp-server internals

### 8.1 Process model

One binary, one process: HTTP mux with `/mcp` (SDK `StreamableHTTPHandler`), `/v1`, `/healthz`; in-process cron (`robfig/cron`) for jobs guarded by `pg_try_advisory_lock` so a second replica would not double-run. Graceful shutdown drains in-flight requests (30 s).

### 8.2 Request lifecycle

```
auth middleware → principal + memberships → context.WithValue
  → tool/REST handler → policy.Check(action, scope, principal)
  → store.Tx(ctx, func(tx){ SET LOCAL acp.*; queries })   // one transaction per request
  → audit rows via triggers (actor from GUC) → pg_notify
```

`request_id` is generated per request, logged, and stored in `audit.request_id`.

### 8.3 Write policy (Phase 1 subset)

| Action | Rule |
|---|---|
| `memory.write` by `agent_*` | forced `status=unverified`, `tier` ∈ {working, episodic, semantic} as requested, `verification.type` must be `agent_inference` or `code` |
| `memory.write` by `human*` | may set `probable`; `confirmed` only at user/team scope if `lead`/owner; org/global → review item instead |
| `memory.supersede` | allowed on rows the caller can write; creates edge + sets `superseded_by`; audit `reason` required |
| `instruction.propose` | always review; auto-approve only for user-scope preferences |
| `skill` activation | admin/lead of the skill's team |

### 8.4 Embeddings

**Phase 1:** `embed.Client` calls Ollama's `/api/embed` directly from Go (`nomic-embed-text`, 768-d; batch ≤ 64; 5 s deadline; retry ×2). No Python in the Phase 1 request path. **Phase 2+:** the Python jobs sidecar owns summarization and LLM labeling; embeddings may move behind it only if batching needs justify it. On failure the memory row is stored with `embedding NULL` and a backfill job retries. Search degrades to FTS/trigram when Ollama is down (`memory.search` never fails due to embeddings).

### 8.5 Git watcher (Phase 2, designed now)

Runs on a schedule and on push webhooks from the repos' host. For each memory with `verification.type='code'`, compare `valid_from_commit..HEAD` for the referenced repo: Phase 2 marks `stale_reason='file_changed'` when any commit touches a path in `identifiers`; Phase 4 parses referenced symbols with tree-sitter and flags only on body/signature change or removal. Stale flags are never cleared automatically except when a human or a `code` re-verification updates `last_verified_at`.

---

## 9. Reconciliation (`cp import`)

```
cp import scan  [--roots /work,~]        → inventory.json (files, hashes, sizes, detected type, implied scope)
cp import plan  inventory.json           → plan.json  (blocks classified, hash-dedup, conflicts)
cp import apply plan.json --machine wsl  → POST /v1/import; creates instructions/preferences (status=proposed unless identical to existing active), skill import branches, memory rows (episodic/unverified), review items (import_conflict)
cp adapter install                       → replaces local files with rendered ones, installs adapter, renames old stores to *.pre-acp
```

Block classification: markdown headings and bullets are split into blocks; a block is a `preference` if it matches an allowlist of style keys (indentation, verbosity, language for comments) or comes from a user-global file and contains first-person phrasing; otherwise `instruction`. Keys are derived by a small heuristic + LLM-assisted labeling (sidecar, Phase 2) with human confirmation in the review UI. Memorix stores are read via `memorix transfer export --format json`.

Ordering: most-trusted machine first (its identical blocks become `active`); subsequent machines only add proposals and conflicts.

---

## 10. Session offload (Phase 3 design constraints)

Phase 1 must:
- Standardize the mount root (`/work/<project>/<repo>`) in adapter config and pod images now, so cwd slugs match later.
- Reserve MinIO bucket `acp-sessions` with SSE (server-side encryption) and an `owner`-only policy.
- Write checkpoints on `Stop`/`PreCompact` (Tier 1) from Phase 2 onward.

`cp offload` itself (commit `wip/<session>`, upload transcript, provision pod, `--resume`) is specified in the design doc §7 and implemented in Phase 3 under a build tag so it does not affect Phase 1 binaries.

---

## 11. Deployment

| Component | Where | How |
|---|---|---|
| Postgres 16 + pgvector + pg_trgm + ltree | **CloudNativePG** cluster (1 instance Phase 1) with PVC on **Longhorn** | Dedicated cluster `acp-pg`, database `acp`; roles `acp_migrate` (DDL), `acp_app` (RLS-enforced DML). Not coupled to any host |
| cp-server | k8s Deployment, 1 replica | Image built with `ko`; config via ConfigMap + Secret; `/readyz` probe |
| embed-sidecar | k8s Deployment on the T4 node (`nodeSelector: gpu=t4`) | Talks to Ollama on the same node |
| Ollama | Existing | `nomic-embed-text` pulled at startup |
| MinIO | Existing | Buckets: `acp-sessions`, `acp-backups` |
| Skills repo | Bare repo on the Postgres host or Gitea if present | cp-server has a deploy key with push for proposal branches |
| Traefik v3 + cert-manager | Existing ingress/cert stack | `IngressRoute` for `acp.<tailnet-domain>`; TLS via cert-manager; `/mcp` and `/v1` → cp-server; per-token rate limit via Traefik `RateLimit` middleware keyed on a request header derived from the token hash |
| Tailscale | Existing | Only path to the service; no public exposure |
| Backups | CloudNativePG **barman-cloud** to MinIO bucket `acp-backups`: continuous WAL archiving; base backup daily 03:00; retention 30 days; SSE-S3 encryption; PITR enabled | Restore procedure: `Cluster` with `bootstrap.recovery` from the object store (documented runbook). Weekly CronJob restores to a scratch cluster and asserts row counts on `memory`, `instruction`, `audit` |

Adapter distribution: GitHub Releases of static binaries; `cp adapter install` writes the systemd/launchd unit.

---

## 12. Observability

- **Logs:** `slog` JSON with `request_id`, `principal_id`, `tool`, `scope_path`, latency; hook shims log to `~/.acp/hooks.log` with rotation.
- **Metrics (OTel → existing collector):** `acp_tool_latency_seconds{tool}`, `acp_pack_tokens{section}`, `acp_memory_status_total{status}`, `acp_outbox_depth`, `acp_render_drift_total`, `acp_embed_failures_total`, `acp_review_open{kind}`.
- **Traces:** one span per request; child spans for compile stages and DB.
- **Alerts (Phase 1 minimum):** outbox depth > 500 on any machine for > 30 min; `readyz` failing > 5 min; drift proposals > 10/day (indicates someone bypassing the adapter).

---

## 13. Security

| Threat | Control |
|---|---|
| Cross-team read via service bug | RLS with `FORCE`; CI leak tests; `acp_app` has no `BYPASSRLS` |
| Token theft | Hashed at rest; per-machine; revocable; short TTL for agent tokens (24 h, adapter refreshes); NGINX rate limit |
| Prompt injection via stored memory | **Layered mitigation, not a boundary.** (1) Agent observations enter as `unverified` and are excluded from default packs; (2) only corroboration or human action promotes them; (3) instructions are a separate deterministic table — no agent path turns a memory into a directive; (4) memory is rendered in an explicitly non-directive section inside fenced blocks with a preamble; (5) `body` capped at 4 KB, control chars stripped. The fence is the weakest layer; (1)–(3) are the real control |
| Low-trust agent poisoning | Agents write `unverified` only; feedback trust-weighted; contradiction policy (Phase 4) never downgrades `confirmed` |
| Transcript exposure | MinIO SSE, `owner` policy, never indexed; `cp offload` refuses if the bucket policy check fails |
| Secrets in memory | Write path runs a secret-pattern scanner (AWS keys, PEM blocks, JWTs); matches are rejected with `ACP_SECRET_DETECTED` |
| Supply chain | `govulncheck` in CI; pinned module versions; images built from distroless |

---

## 14. Testing strategy

| Layer | Approach |
|---|---|
| Store | `sqlc` compile-time query checks; integration tests against a Postgres testcontainer with migrations applied |
| RLS | Table-driven tests: for each (visibility × caller role × membership) attempt SELECT/INSERT/UPDATE; expected-denied cases assert zero rows and no error leak. Runs as `acp_app`, never superuser |
| Scope chain | Property tests: random valid chains resolve; invalid parent kinds rejected by trigger |
| Instruction resolution | Golden tests: given fixtures at multiple depths, assert the effective set and suppression notes |
| Compiler | Budget tests (never exceeds; never trims sections 1–3); determinism tests (same inputs → same bytes) |
| Render | Golden files per target; drift-hash excludes footer |
| Adapter | Fake server (httptest); outbox tests with injected failures; symlink tests on Linux and macOS runners; WSL path handling |
| MCP | Conformance: SDK client against the server for every tool; schema snapshot test so tool schemas do not change silently |
| Import | Fixture corpus of real `CLAUDE.md`/`AGENTS.md`/skills from the two machines (sanitized); assert dedupe and conflict counts |
| E2E (Phase 1 exit) | Two adapter containers + server: write memory on A, assert searchable on B ≤ 60 s; change instruction, assert both rendered files match ≤ 5 min |

---

## 15. Rollout plan

1. **Week 1 — schema + server skeleton.** Migrations, RLS, auth, `memory.write/search/supersede`, `instruction`/`preference` resolution, `context.get` (keyword), `/v1/render`. Deploy to k8s behind Tailscale. Mint tokens for WSL and Mac Mini.
2. **Week 2 — adapter + render.** Golden renders; adapter render/skills/outbox loops; Claude Code + Codex + Cursor pointed at `/mcp`; hook shims for `SessionStart` and `PostToolUse`.
3. **Week 3 — import.** Run `cp import` on the Mac Mini (most trusted), then WSL; resolve conflicts via `cp review`; cut both machines over. Keep Memorix/legacy stores read-only.
4. **Week 4 — soak.** Watch drift and outbox metrics; fix renders; tune search. Phase 1 exit check.

Rollback: adapter `cp adapter uninstall` puts back `*.pre-acp` files; server data is additive and can be left in place. There is no `--restore` flag: restoring is the only behaviour, so it is not optional.

---

## 16. Failure modes and recovery

| Failure | Behavior | Recovery |
|---|---|---|
| Server down | Adapter serves cached config (already on disk), `context.get` from local FTS cache, outbox queues | Auto on reconnect |
| Postgres down | `/readyz` fails; server returns 503; adapters treat as "server down" | Restart; restore from backup if needed |
| Embed sidecar down | Writes store `embedding NULL`; search is FTS-only | Backfill job |
| Skills repo unreachable | Adapter keeps last linked versions | Auto |
| Drift storm (mass hand edits) | Review items pile up; alert | Human triage; possibly a missing instruction key |
| Bad instruction rendered everywhere | Audit `before` image → `cp review revert <audit id>` → broadcast → re-render ≤ 5 min | |
| Token leaked | `cp token revoke`; audit shows usage by `request_id` | |

---

## 17. Alternatives considered

| Decision | Alternative | Why not |
|---|---|---|
| Postgres for everything incl. vectors | Qdrant/Graphiti | One system to back up and secure; RLS applies to vectors too; scale ceiling is far above Phase 1 |
| Official Go MCP SDK | mark3labs/mcp-go | Official SDK tracks spec versions and has typed tools; fewer surprises with Claude Code/Codex clients |
| Bearer tokens Phase 1 | OIDC now | Single user; OIDC adds a dependency before the core is proven; schema is OIDC-ready |
| Audit via triggers | App-level audit writes | Triggers cannot be skipped by a code path; GUC-based actor keeps them cheap |
| Adapter renders files | Harness reads MCP resources for instructions | Harnesses load `CLAUDE.md`/`AGENTS.md` before any tool call; files are the only universal injection point |
| `git` shell-out in adapter | pure go-git for everything | Worktree/export and auth are simpler and faster via the system git; go-git used only for read-only inspection |
| tiktoken proxy for budgets | Exact harness tokenizers | Not exposed; 10 % margin covers variance |

---

## 18. Review record

Reviews were conducted against v0.9 of this document with four lenses. All findings are resolved in the text above; the table records what changed.

| # | Lens | Finding | Severity | Resolution |
|---|---|---|---|---|
| R1 | Security | RLS policies referenced `scope_team()` but `scope` had no denormalized team, making the function a recursive ltree walk on every row | High | Added `scope.team_id` and `scope.path` (ltree); `scope_team`/`scope_org` are `STABLE` lookups on the row; policies unchanged in shape |
| R2 | Security | Memory bodies rendered inline in the pack could carry injected directives to the model | High | §13: memory rendered as quoted data with preamble; only instructions render as directives; body cap 4 KB; control chars stripped |
| R3 | Security | Agent tokens had no TTL; a leaked worker token was permanent | Medium | 24 h TTL for `agent` principals with adapter refresh; `cp token revoke` |
| R4 | Data | `verification` jsonb allowed `type` to drift from a queryable column | Medium | Added generated column `verification_type`; CHECK on presence of `type` |
| R5 | Data | Unique `(scope_id,key)` on `instruction` blocked keeping retired history | Medium | Partial unique index `WHERE status='active'` |
| R6 | Data | `active_version_id` could point at an unapproved skill version | Medium | Trigger invariant; `skill.propose` never activates |
| R7 | Data | No idempotency on outbox drain; a retried batch could duplicate memories | High | `client_id` UUIDv7 unique per payload; `/v1/memory/batch` upserts on `client_id` |
| R8 | Ops | Cron jobs would double-run if a second replica ever started | Low | `pg_try_advisory_lock` around every job |
| R9 | Ops | Hook shims blocking on a slow server would stall the harness | High | 1.5 s server timeout, 2 s hard exit, cache/outbox fallback |
| R10 | Ops | No restore test for backups | Medium | Weekly restore CronJob to a scratch database with a row-count assertion |
| R11 | Go | CGO SQLite in the adapter complicates macOS/WSL/pod builds | Medium | `modernc.org/sqlite` (pure Go); `ko` for server images |
| R12 | Go | Tool names with dots — confirm SDK/clients accept them | Low | SDK allows `[a-zA-Z0-9_.-]`; schema snapshot test guards; fallback to underscores is a one-line change |
| R13 | Product | Phase 1 `context.get` without ranking risks flooding packs with low-value keyword hits | Medium | Phase 1 caps memory section at 20 items, orders by `ts_rank` + identifier-hit bonus + recency; full ranking in Phase 2 |
| R14 | Product | Drift detection would flag the footer timestamp as a change | Low | Footer excluded from the drift hash |
| R15 | Product | Import classification of "preference vs instruction" is heuristic and will misfile | Medium | Everything imported lands as `proposed`; review UI lets the reviewer flip kind before approval |

### 18.2 Round 2 (external review of v1.0)

| # | Severity | Finding | Resolution in v1.1 |
|---|---|---|---|
| R16 | Blocker | `visibility` type referenced but never created | `CREATE TYPE visibility` added at top of §3.1 |
| R17 | Blocker | R7 claimed outbox idempotency but no server-side key existed | `ingest_receipt` table (§3.4); receipt + subject created atomically; `/v1/memory/batch` returns original id on duplicate |
| R18 | High | Git remote → repo scope mapping had no data model | `repo_remote` table; unknown remotes surfaced, bound via `cp repo bind`, never auto-created |
| R19 | High | `project_grant` absent from the RLS read policy; docs implied option B, SQL implemented A | Option B adopted explicitly; `acp.granted_project_ids` GUC; `scope_project()`; write policy honors grants |
| R20 | High | Hook capture split inconsistently between Phase 1 and 2 | Phase 1 = `PostToolUse`→outbox (CAP-1a-capture); Phase 2 = `SessionStart`→`context.get` (CAP-1a-context) and `Stop`/`PreCompact` summarization + checkpoints (CAP-1b). CAP-1a was split in #61: `PostToolUse` was nearly built, while `SessionStart` needs a `context.get` client path and an offline instruction-pack fallback that do not exist. PRD amended |
| R21 | High | Embedding sidecar shown as Phase 2+ but on the Phase 1 path | Phase 1 calls Ollama directly from Go; Python sidecar is Phase 2 jobs only |
| R22 | High | Budget acceptance referenced a harness tokenizer that isn't available | Conservative estimator with 15 % headroom; PRD CTX-2 acceptance rewritten |
| R23 | Medium | ltree path encoding unspecified; keys contain `/` | Path labels are UUID-derived; keys stored separately |
| R24 | Medium | Root scopes not unique under `UNIQUE(parent_id,…)` with NULL parent | `UNIQUE NULLS NOT DISTINCT` + single-global partial index |
| R25 | Medium | Three overlapping notions of "admin" | Deterministic: `trust='human_admin'` ⇒ org/global admin; `membership.role` ⇒ team-scoped only |
| R26 | Medium | Repo discovery one level too shallow | `/work/*/*` plus configured roots; upserts `workspace` |
| R27 | Medium | `/readyz` depended on Git | Readiness = Postgres only; Git health on `/v1/health/git` + metric |
| R28 | Medium | NGINX assumed; homelab runs Traefik v3 + cert-manager; Longhorn available | Traefik `IngressRoute` + cert-manager; CloudNativePG on Longhorn |
| R29 | Medium | Backups under-specified | CloudNativePG barman-cloud → MinIO; schedule, retention, encryption, PITR, restore runbook, weekly restore test |
| R30 | Low | Branch vs physical workspace conflated | `workspace` table added now; memory `source.workspace_id` populated from Phase 1 |
| R31 | Low | Prompt-injection control overstated as a boundary | Reworded as layered mitigation; status gating and the instruction/memory split named as the real control |

Noted for a later phase (not changed): native skill-registry delivery mode in the adapter (publish/sync to harnesses that expose a versioned skill API) alongside filesystem materialization.

**Deferred (tracked, not blocking Phase 1):**
- D1: pack-cache invalidation granularity (per-chain vs per-scope) — decide in Phase 2 with real audit volume.
- D2: whether `branch` scopes should be auto-pruned on branch deletion or retained as history — default retain, revisit in Phase 4.
- D3: tree-sitter grammars to bundle for symbol-level staleness (Python, TypeScript, Go first).

**Sign-off:** v1.1 approved for Phase 1 implementation as specified in §§3–9, §11, §14–15. Any further change to §3 DDL or §4 contracts requires a new review round.

---

## 19. Open items

| Item | Owner | Needed by |
|---|---|---|
| Confirm homelab ingress/storage as stated in round-2 review (Traefik v3 + cert-manager, Longhorn, CloudNativePG acceptable) | Jeremy | Week 1 |
| Confirm Ollama model: `nomic-embed-text` (768-d) vs `qwen3-embedding` (1024-d); DDL `vector(N)` follows | Jeremy | Week 1 |
| Confirm canonical root `/work/<project>/<repo>` on WSL and Mac Mini | Jeremy | Week 2 |
| Skills repo host: bare repo vs Gitea | Jeremy | Week 2 |
| Existing OTel collector endpoint | Jeremy | Week 1 |
| Initial instruction key taxonomy (e.g. `lang.python.version`, `deploy.branch_policy`, `style.indent`) | Jeremy | Week 3 (import) |
