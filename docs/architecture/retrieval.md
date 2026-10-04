# Context retrieval

**Last reviewed:** 2026-10-03 · **Re-read cadence:** at each retrieval or storage decision

**Status:** the agreed initial retrieval direction combines ranked text, exact identifiers
and scope, and explicit semantic associations without neural inference. Learned embeddings
are an optional candidate subject to relevance and resource evaluation, not a mandatory
dependency or local default. Useful hybrid recall and battery-conscious interactive work
remain product goals. SQLite is selected for local nodes and the initial single coordinator.
The Go driver, scoring/index details, resource budgets, and any optional vector backend or
model/provider remain open.

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
