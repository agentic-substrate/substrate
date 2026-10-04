# Getting started

**Last reviewed:** 2026-10-03. Re-read whenever the CLI, HTTP contract, or visible UI changes.

## Prerequisites and build

Use Linux or WSL 2 with Go 1.27.1, Node.js 24.15.0, npm 11.12.1, and Make. In WSL, put the
checkout in the Linux filesystem. Node.js is needed to build the web assets and documentation;
the resulting executable contains its UI and does not need Node.js or a model runtime.

```sh
npm ci
make build
./bin/substrate serve
```

Open <http://127.0.0.1:9842>. The page requests the local status endpoint and distinguishes
loading, connection success, and connection failure. The Refresh status button retries the
request and works by keyboard. Its successful state means the bootstrap endpoint is reachable;
it does not mean artifact storage or synchronization exists.

## Commands and configuration

`substrate serve` runs in the foreground. Its default listener is `127.0.0.1:9842`. Change it
with `-listen`, for example `./bin/substrate serve -listen 127.0.0.1:9843`. Only numeric IPv4
or IPv6 loopback addresses are accepted. There is no authentication or remote deployment
contract in this scaffold. Stop it with Ctrl+C or SIGTERM; shutdown waits up to five seconds
for active requests.

`substrate version` prints the bootstrap version. Unsupported commands and invalid flags
return a nonzero exit code. This checkout does not install services, create a data directory,
or expose a configuration file.

## HTTP contract

`GET /api/status` returns JSON with `stage` equal to `bootstrap`, with caching disabled.
The UI uses this value to identify the available foundation. Unknown API and MCP routes return
404, including paths with encoded separators. Files under `/assets/` must exist; a missing asset returns 404 rather than HTML.
Client navigation without a file extension falls back to the embedded page. Unsupported
methods on the page routes return 405. The server applies a restrictive content policy and
does not load external scripts or fonts.

No artifact, MCP, database, pairing, or synchronization endpoint exists yet. See the
[first release contract](product/first-release.md) for their required behavior and
[development guide](development.md) for checks that validate this checkout.
