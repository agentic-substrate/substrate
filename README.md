# Substrate

Substrate is an open source foundation for carrying permitted memories, skills, and agent
definitions between coding harnesses. When a session changes tools or projects, useful context
should survive without leaking Work artifacts into Personal projects.

This checkout is the bootstrap foundation. It builds a Go executable with an embedded
Vite/React status page. Artifact storage, retrieval, MCP tools, and synchronization are roadmap
work; the status page does not claim those capabilities are available.

## Try the foundation

Development requires Go 1.27.1, Node.js 24.15.0, npm 11.12.1, and Make on Linux or WSL 2.
Keep the checkout in the Linux filesystem when using WSL.

```sh
npm ci
make build
./bin/substrate serve
```

Open <http://127.0.0.1:9842>. Stop the process with Ctrl+C. The only configuration currently
available is the `serve -listen` flag, which accepts a numeric loopback address. The executable
needs neither Node.js nor a model runtime to run.

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
Tailscale, PostgreSQL, and pgvector are not requirements. The SQLite driver and other runtime
libraries will be selected when implementing their contracts.

Read the [roadmap](ROADMAP.md), [product framing](docs/product/vision.md), and
[architecture decisions](docs/architecture/index.md). The whole project is licensed under
[Apache 2.0](LICENSE). There is no separate paid edition or required hosted service.
