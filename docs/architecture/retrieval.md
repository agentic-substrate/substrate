# Context retrieval

**Last reviewed:** 2026-10-05 · **Re-read cadence:** at each retrieval or storage decision

**Status:** the agreed initial retrieval direction combines ranked text, exact identifiers
and scope, and explicit semantic associations without neural inference. Learned embeddings
are an optional candidate subject to relevance and resource evaluation, not a mandatory
dependency or local default. Useful hybrid recall and battery-conscious interactive work
remain product goals. SQLite is selected for local nodes and the initial single coordinator.
The [local persistence contract](artifacts.md#selected-local-persistence-contract) selects
the pinned Go driver and FTS5 initialization. The local lexical scoring/index contract is selected below. Resource targets and any
optional vector backend or model/provider remain open.

Artifacts means memories, skills, and agent definitions. The agreed
[artifact contract](artifacts.md) defines eligibility before ranking, effective versions,
search/review modes, and explanations. Organization boundaries apply even when the same
person belongs to several spaces. Similarity and scope specificity cannot grant access.

## Product constraints

Instructions and preferences resolve deterministically by scope. Search ranking applies to
optional context such as memories; it cannot remove required instructions or grant access.
Only permitted context held locally can be searched during disconnection.

Permission, scope, and status rules apply to every retrieval path, including memory, skill,
and agent-definition discovery. The [sharing requirements](sharing.md) cover work/personal
separation, group restrictions, and permitted replicas. Unverified observations remain
distinguishable from verified knowledge regardless of their similarity score.
The [repository requirements](repositories.md) add repository, subdirectory, branch, and
task applicability. Resolve local paths through a trusted repository binding before selecting
context. A path or repository identifier supplied by an agent cannot enlarge session access.

## Agreed initial retrieval direction

Use a baseline without neural inference: combine ranked text with explicit, authorized
semantic associations and session context. Validate its useful recall and resource cost
before introducing a model dependency. This selects the initial direction, not every scoring
or indexing detail, and does not claim that arbitrary paraphrases are solved.

| Channel | Useful behavior | Limitation |
|---|---|---|
| BM25, phrases, and prefixes | Find relevant words, commands, errors, and descriptions. | Substantially different wording may not match. |
| Exact IDs, symbols, and paths | Find artifacts explicitly associated with code or repository entities. | Associations need to be recorded. |
| Scoped aliases, tags, and taxonomy | Connect known terms such as SSO and single sign-on, or approved related topics. | Needs maintenance; ambiguous expansions can hurt relevance. |
| Trigram candidates and bounded edit-distance matching | Find partial identifiers and spelling mistakes. | Character similarity does not establish conceptual similarity. |
| Explicit relationships | Find supporting evidence, dependencies, provenance, and authorized successor versions. | Requires recorded edges and bounded traversal. |
| Optional learned embeddings | Suggest broader paraphrases and conceptual matches. | Adds inference/indexing work and does not guarantee relevance. |

[SQLite FTS5](https://www.sqlite.org/fts5.html) provides ranked text, phrase/prefix queries,
synonym hooks, and trigram substring indexing. Trigrams alone do not implement typo correction;
edit-distance matching is a separate candidate. [SKOS](https://www.w3.org/TR/skos-reference/)
provides a precedent for explicit labels and concept relations without neural inference.
Such associations can live in SQLite tables; a separate graph service is unnecessary.

Classical [TF-IDF vector scoring](https://nlp.stanford.edu/IR-book/html/htmledition/queries-as-vectors-1.html)
also needs no neural encoder, but retains vocabulary-matching limits.
[Latent semantic indexing](https://nlp.stanford.edu/IR-book/html/htmledition/latent-semantic-indexing-1.html)
derives associations through matrix decomposition rather than a neural model. It still fits
a statistical representation, needs recomputation as the corpus changes, and has processing
cost. It is a comparison candidate, not a model-free or automatically energy-efficient solution.
Any corpus-derived representation inherits its sources' isolation and processing restrictions.

An existing harness can supply a query, file, symbol, or topic in its ordinary retrieval call.
Substrate need not invoke another model to interpret every prompt. Taxonomy, aliases,
relationships, and metadata are governed content: their creation, visibility, and expansion
must follow the artifact contract. Applicability can improve relevance only after authorization.
Discovery cannot silently activate a skill or choose an unapproved definition.

Curated semantic associations cover known concepts; they do not provide general learned
paraphrase recall. The [BEIR benchmark](https://arxiv.org/abs/2104.08663) found BM25 a robust
baseline across its evaluated tasks, not proof of Substrate quality. The first-release recall
criteria must be evaluated on actual artifact queries before declaring this combination adequate.

## Resource-aware operation

Battery cost is a first-class evaluation criterion, distinct from permission to process data.
The following resource direction is agreed; exact budgets and power integration remain open:

- Run retrieval on a user request, an explicit harness tool call, or an authorized context
  refresh. Do not encode or index every keystroke or prompt merely because it was observed.
- Save authorized artifacts durably, then update indexes for changed artifact versions.
  Coalesce superseded jobs and avoid reprocessing unchanged text or entire conversations.
- Use the inexpensive evaluated baseline for ordinary interactive retrieval. Enabling an
  optional model must explicitly define whether automatic per-turn searches may invoke it.
- Defer bulk enrichment, model acquisition, and major rebuilds on battery by default;
  offer charging/idle scheduling, manual operation, and a deliberate run-now option.
  Idle operation still consumes energy. Bound non-model maintenance as well.
- Reuse eligible unchanged representations with versioned, scope-aware caches, while
  rechecking current authorization. A cache cannot extend an offline grant.
- Show mode, coverage, deferred work, and active optional inference separately. A deliberate
  non-neural mode is normal operation, not inherently a degraded or unready product.

Background scheduling precedents include [Windows energy guidance](https://learn.microsoft.com/en-us/windows/apps/develop/performance/optimize-background-activity).
A resource pause cannot discard saved work, authorize a remote fallback, or bypass Work access.
Battery, permission, and provider states have different meanings.

## Conditional embedding path

If evaluation justifies model-based retrieval, [embedding deployment research](embeddings.md)
compares local and approved remote options. It no longer establishes a presumed default.
Model/library/runtime choices follow the relevance and resource decision.

Document embedding would process changed saved artifact versions as scheduled enrichment.
Query encoding would run once per uncached search using a compatible approved profile;
it would not need to follow every chat token. A harness that searches each turn could still
cause inference each turn, so invocation policy and budgets are essential.

Persist content and pending work before calling a provider. Embedding failures must not lose
the underlying artifact; derived indexes can be rebuilt. Record source revision and encoder
compatibility, and invalidate stale representations. Existing vectors cannot encode new
query text without a compatible query encoder. Any selected provider still follows processing,
placement, and offline authorization policy.

Embedded vector candidates include [modernc SQLite vec](https://pkg.go.dev/modernc.org/sqlite/vec)
and [ncruces Vec1](https://pkg.go.dev/github.com/ncruces/go-sqlite3/ext/vec1).
Exact search is a baseline to measure before approximate indexing. Their existence does not
justify a mandatory vector dependency; packaging, filtering, recovery, and platform builds
require verification if that path is selected. PostgreSQL/pgvector remains a future coordinator option.

## Validation and optional enhancement

Compare the agreed baseline with optional embeddings on identifiers, aliases, typos,
unrecorded paraphrases, conceptual questions, conflicting and superseded memories, team/private
scopes, sibling repositories, and branch-specific observations. Include memory, skill, and
agent-definition discovery, not just generic document similarity.

Measure useful recall/ranking, distracting results, latency, CPU time, peak memory, wakeups,
incremental and bulk indexing, and actual energy on representative devices where host tooling
permits. Compare identical workloads and cold/warm states against idle baselines, including
WSL host activity. No battery-hour, wattage, or energy-saving claims are established yet.
Check authorization and newly written content, plus disconnected and deferred-work behavior.
Require worthwhile measured recall improvement before adding a model dependency.

Evaluate filtering and candidate selection together. A restricted group's eligible memories
must not disappear because an approximate index selected other groups' results first.
Unauthorized content must never reach a reranker or be exposed through result metadata.
Refreshing or removing a memory must invalidate its derived representations.

Filtering visible rows does not necessarily isolate all ranking inputs. FTS5's BM25 formula
uses table-wide corpus statistics, so a shared table can let ineligible content influence
eligible scores. This is an inference from the [FTS5 ranking definition](https://www.sqlite.org/fts5.html#the_bm25_function).
Evaluate corpus/statistics partitioning wherever the required segmentation forbids that
influence, rather than treating a final row filter as the whole retrieval boundary.

Research: [SQLite FTS5](https://www.sqlite.org/fts5.html),
[BEIR retrieval benchmark](https://arxiv.org/abs/2104.08663),
[sqlite-vec](https://github.com/asg017/sqlite-vec),
[SQLite Vec1](https://sqlite.org/vec1/doc/trunk/doc/vec1.md),
[pgvector filtering and hybrid search](https://github.com/pgvector/pgvector), and
[local embedding API example](https://docs.ollama.com/api/embed).

## Selected local lexical implementation

The initial index stores relational token positions for each current artifact revision in
SQLite. Unicode letters, numbers, underscores, and hyphens form lowercase tokens. Search
uses up to 32 query terms and optional final prefixes of at least three Unicode characters, with
100 points per matched query term, up to ten frequency points per term, 20 points for an
ordered contiguous multi-term phrase, and 1000 points for an exact artifact/revision ID,
qualified source identity, or explicitly recorded identifier, search alias, or topic.
Artifact ID breaks equal scores deterministically. No table-wide corpus statistics, model,
vector backend, or remote service participates. Scope, active lifecycle, current head, and
approved Git selection are checked before candidate scoring. Adding hidden Work documents
cannot change Personal results, scores, or coverage. An ambiguous source alias retains
qualified authorized alternatives; ranking does not select or activate that alias.

Associations belong to immutable revisions and their retry fingerprints. Each of identifiers,
search aliases, topics, and related artifact IDs permits up to 32 distinct UTF-8 labels of
256 bytes. Related endpoints must be within the contributing session's scope. Related reads
and `related_to` searches check both endpoints' current eligibility; retired or inaccessible
endpoints disappear. Relationships traverse one explicitly recorded hop and never infer
external or transitive access. Search aliases are separate from registered source delivery
aliases: use search results' exact artifact/revision IDs for a subsequent current read.

Schema version 3 adds revision associations, a coalesced per-artifact index queue, token
positions, and a revision checkpoint. Version 5 adds durable installation pause and per-job
bulk/failure state while preserving content, approvals, bundles, publication records, retry
receipts, and the pending operation ledger. Incremental work always loads the current committed
head; stale/history-only jobs whose checkpoint matches consume their queue without rewriting
postings. Explicit full rebuilds force regeneration of matching checkpoints. History-only writes keep
existing bulk intent, including artifacts that have not yet been indexed; callers record actual
head changes before promoting queued bulk to incremental work.

`Store.IndexNext` processes one artifact in a cancellable transaction. It atomically replaces
postings, records the checkpoint, and consumes its own queue entry. Cancellation rolls back
unfinished derived work without creating a failure. A real failure rolls back derived changes,
then records only a generic failure on the durable job. Failed jobs remain queued but cannot
block healthy incremental work, and stay failed through later edits and restart until explicit
retry. Saved content, receipts, and previous derived state remain intact. Current exact reads
work while paused, and stale postings cannot deliver old or retired content. Exact
ID/association lookup can find current unindexed content; ordinary lexical recall reports
incomplete coverage until indexed.

`Store.IndexBatch` bounds attempts to 100 artifact identities, including failures, with one
transaction per artifact. Each revision indexes at most 32,768 token positions across content
and explicit associations/dependencies; `index.limited` reports permitted current revisions
with truncated lexical coverage. Complete content remains saved and readable. `index.eligible`,
`indexed`, and `pending` count only the current eligible session scope. Search returns at most
100 results and 400-rune excerpts. This bounds index work without promising recall for text
beyond that cap. Unrecorded paraphrases, spelling errors, and inferred relationships are
unsupported. A paused capture remains pending-local/unverified evidence.

The foreground node has one worker and one buffered wake signal with a 25-millisecond
coalescing delay. Startup, successful artifact mutations, and explicit maintenance controls
can wake it; search, content reads, publication review, and status do not. It sleeps without
an idle polling ticker. All automatic and explicit indexing uses this worker, so concurrency
is one artifact transaction. Installation pause is durable; startup preserves it unless an
explicit boolean override is supplied. Pause acknowledgements wait for the worker's current
artifact transaction, including during an explicit batch. Pause/status/rebuild/retry controls
are serviced between artifacts; additional run/resume batches wait on a separate unbuffered
channel and cannot recursively start indexing. Explicit index/resume attempts at most 100 incremental jobs before
responding; background work can continue afterward. Failures count toward the attempt bound.

Full rebuilds queue discretionary bulk work. Automatic processing never consumes bulk or failed
jobs. Explicit run attempts at most 100 incremental/bulk jobs while unpaused, and remaining
bulk stays deferred across restart. Retry clears generic failures explicitly and wakes healthy
incremental work. Publication drafts enqueue no artifact work; successful publication uses the
same committed artifact queue as capture.

The existing scoped browser inventory reports lexical mode, durable installation pause,
disjoint queued/deferred/failed counts, and eligible/indexed/pending/limited coverage across
the full owner/space/repository, independently of its bounded artifact list. Healthy incremental
jobs remain queued while paused; healthy bulk remains deferred while paused. Failed jobs
count only as failed. Coarse state prioritizes failed, queued, deferred, then ready, while every
count remains visible. Queue readiness is separate from empty or truncated coverage. Hidden
Work jobs, failures, identifiers, and activity never change Personal scoped counts or coverage;
the deliberately exposed installation pause is labeled separately. No global active job,
progress, or identifier appears. Trusted local CLI status covers installation queue counts and
omits scoped coverage rather than representing unknown coverage as zero.

[Measured foreground evidence](../development.md#foreground-maintenance-evidence) records
resource and lifecycle observations, not a battery guarantee. Charging detection, optional
inference, service installation, and host energy budgets remain unimplemented. Re-read this
contract after scoring, eligibility, schema, queue, scheduling, coverage, or inference changes.
