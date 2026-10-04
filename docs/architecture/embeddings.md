# Embedding deployment and processing

**Last reviewed:** 2026-10-03 · **Re-read cadence:** at each embedding, model, processing, or packaging decision

**Status:** this is a conditional model-based retrieval candidate, not a mandatory component
or agreed default. The [retrieval comparison](retrieval.md) must establish whether embeddings
justify their relevance and resource costs against methods without neural inference.
SQLite, useful hybrid recall, and mandatory artifact boundaries remain agreed. Provider,
runtime, model/profile, vector engine, resource budgets, and packaging remain open.
This document describes design, not implemented behavior.

## Deployment options

Embedding generation turns artifact or query text into vectors. It is separate from storing
those vectors and from the generation model used by a coding harness.

| Direction | Benefit | Cost or limitation |
|---|---|---|
| Local runtime | Can encode new queries while disconnected; no hosted account required. | Model/runtime acquisition, local disk and memory, computation, startup and updates. |
| Organization-managed remote endpoint | Central administration and resource sharing within an approved processing boundary. | Network and service availability; each device still needs a compatible local encoder for offline semantics. |
| Hosted provider | Avoids local model operation; useful for constrained devices. | Connectivity, credentials, usage charges, and permission to transfer text to the provider. |
| Supported local and remote paths | Fits Personal use and different organization policies. | Several configuration/readiness paths and explicit compatibility management. |

If evaluation justifies learned retrieval, local and approved remote endpoints are deployment
candidates. A local Personal profile would support disconnected query encoding but remains
subject to measured laptop cost; it is not selected as the default. Work uses only its
authority-approved processing profile. Organizations can require
an internal endpoint, permit local execution, or approve an external provider; a Personal
choice cannot widen Work permissions. Support for both means selectable governed paths;
it does not require dual indexing or permit silent remote fallback.

Local inference need not be compiled into the Go application. A separately managed runtime
with downloaded weights can expose an embedding API to a Go client. Existing examples include
[Ollama embeddings](https://docs.ollama.com/capabilities/embeddings) and
[TEI CPU operation](https://huggingface.co/docs/text-embeddings-inference/local_cpu).
These demonstrate possible deployment patterns; neither runtime is selected. Embedded bindings
remain another packaging candidate. The backend and embedding client remain Go.

## Processing authorization

Authorize artifact text and query text before each provider call. Reading or retaining a
Work replica does not grant permission to send it to a provider. A query can contain Work
information and follows the active session's processing boundary. Dependencies and combined
content must satisfy all applicable source restrictions.

The approved profile identifies the execution boundary, endpoint/provider where applicable,
model compatibility, and permitted data handling. Node preferences can select only permitted
profiles; an artifact can impose stricter conditions. Govern indexing jobs, embeddings,
runtime caches, logs, and backups consistently with their source context.

A loopback address does not prove local execution. Some runtimes expose both local and cloud
models through the same API; an approved local-only profile must enforce that behavior.
See [Ollama local-only configuration](https://docs.ollama.com/faq#how-do-i-disable-ollama-cloud-features).
Provider authentication is distinct from Substrate membership and artifact permissions.

If selected neural processing alone is unavailable or prohibited, independently permitted
text and structured retrieval may continue with an explanation. An expired artifact authorization
blocks affected retrieval; provider availability cannot override it. Never redirect text to
another provider solely because the configured destination fails.

## Compatible encoders and disconnected use

Stored document vectors cannot encode new query text. Offline semantics needs an available,
approved local query encoder compatible with the collection's document vectors.
[Ollama guidance](https://docs.ollama.com/capabilities/embeddings) recommends the same model
for indexing and querying. Declared compatible model families can be considered only with
an explicit evaluated compatibility contract; matching dimensions or model names alone is
insufficient.

Record model revision/digest or provider revision, tokenizer and query/document transforms,
dimensions, pooling/normalization, and source revision as appropriate to the provider.
Changing provider or profile requires verified compatibility or a separate embedding rebuild;
incompatible vectors cannot share a search as if their scores were comparable. A hosted-only
model cannot be assumed to have a compatible downloadable local encoder.

## Durability, growth, and readiness

Persist artifacts and pending indexing work in SQLite before invoking the runtime. Failure
does not discard the artifact or accept a shared proposal. Retry the recorded profile and
source revision; stale jobs cannot overwrite newer versions. Keep model calls outside write
transactions. Lexical coverage should follow committed content while semantic work proceeds.

Prioritize interactive queries over background embedding. Show indexing and model acquisition
progress, pending work, disk use, and resumable rebuilds. Distinguish regenerating embeddings
from rebuilding a search index. Chunking and truncation need explicit source/version mapping;
an API's default truncation must not silently omit important artifact content. See
[embedding API controls](https://docs.ollama.com/api/embed).

An available model and functioning compatible index are required before claiming readiness
of an optional neural retrieval component. Setup should manage or guide runtime acquisition
and verify the selected profile. First-time setup of that component without network access
requires a preloaded model/runtime package. Its absence does not inherently make a deliberate
non-neural retrieval mode unready; the product's recall criteria must decide that independently.

Resource policy must govern model acquisition, document enrichment, and query encoding.
On laptops, an optional component cannot automatically start processing each prompt or keep
a model resident merely because it is installed. Scheduled charging/idle work and explicit
invocations are candidates; neither eliminates energy cost. See the [retrieval resource proposal](retrieval.md).

CPU-capable local operation is the recommended baseline to evaluate, with GPU use optional
where supported. No minimum RAM, download size, or latency is promised before representative
hardware and artifact benchmarks. Existing runtime hardware support establishes feasibility,
not Substrate performance. Exact model and packaging decisions follow this deployment choice.

See [retrieval](retrieval.md), [artifact decisions](artifacts.md), [sharing](sharing.md),
[offline grants](offline.md), and [deployment](deployment.md).
