# Substrate

Substrate is an open source, self-hosted context layer beneath coding harnesses. Its goal is
for useful knowledge and working practices to survive a change of tool, session, or machine,
while people retain control over where that context belongs and who can use it.

Memories, skills, and agent definitions are **artifacts**. Together with applicable
instructions, working preferences, and future task checkpoints, they should let the next
session build on what earlier work learned instead of starting with another project briefing.

## The result we are working toward

A developer can switch harnesses, continue on another laptop, or join an existing repository
and receive the permitted context needed for that work. A lesson can become a verified memory
or a reviewed procedure, with its source and history preserved. As code and circumstances
change, outdated or conflicting knowledge should be visible rather than quietly treated as fact.

Work and Personal remain separate even on the same machine. A useful coding practice can
cross that boundary only through an authorized, reviewed publication of a separate version.
An organization can prohibit outside-project use. People and administrators can inspect what
is stored locally, what is synchronized, which version applies, and why a session receives it.
Joining devices and managing access should reduce setup work instead of creating another system
that needs constant attention.

The [vision](docs/product/vision.md) describes the full learning and continuity loop. The
[first release contract](docs/product/first-release.md) defines the initial evidence we need.

## Available today

This checkout builds a Go executable with an embedded Vite/React status and session-context
page. Trusted CLI setup binds sessions to explicit spaces and repositories. The CLI can save,
inspect, and retire local memory observations with durable revisions and retry receipts.
Retrieval, approved Git version selection, MCP tools, and synchronization remain roadmap work.

## Try the foundation

Development requires Go 1.27.1, Node.js 24.15.0, npm 11.12.1, and Make on Linux or WSL 2.
Keep the checkout in the Linux filesystem when using WSL.

```sh
npm ci
make build
./bin/substrate serve
```

Open <http://127.0.0.1:9842>. Stop the process with Ctrl+C. The `serve -listen` flag accepts
a numeric loopback address. Use the getting started guide for trusted setup, state-directory
selection, and scoped capture commands. The executable needs neither Node.js nor a model
runtime to run.

```sh
./bin/substrate serve -listen 127.0.0.1:9843
./bin/substrate version
```

See the [getting started guide](docs/getting-started.md) for the current command and HTTP
contract, [development guide](docs/development.md) for checks and previews, and
[CONTRIBUTING.md](CONTRIBUTING.md) for contribution expectations.

## Direction

Start with one owner and multiple harnesses in one Linux environment or WSL distro. Build
explicit artifact permissions and repository bindings before distributing data. Multiple WSL
instances are the next deployment step; multiple machines, teams, and organizations follow.
The [first release contract](docs/product/first-release.md) describes the acceptance evidence.

The selected direction is Go, SQLite on each node, CLI plus MCP, and an embedded Vite/React
administrative interface. Initial retrieval uses text and explicit associations. Embeddings,
Tailscale, PostgreSQL, and pgvector are not requirements. Local storage uses the pinned
cgo-free SQLite driver documented in the [artifact contract](docs/architecture/artifacts.md#selected-local-persistence-contract);
other runtime libraries will be selected when implementing their contracts.

Read the [roadmap](ROADMAP.md), [public Project](https://github.com/orgs/agentic-substrate/projects/2), and
[architecture decisions](docs/architecture/index.md). The whole project is licensed under
[Apache 2.0](LICENSE). There is no separate paid edition or required hosted service.
