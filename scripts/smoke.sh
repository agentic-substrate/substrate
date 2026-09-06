#!/usr/bin/env bash
# First-run smoke test: build the real binary, boot it with no database, and
# prove /healthz answers 200 while /readyz is 503. A missing Postgres must not
# kill the process (EDD §16). With-database readiness is covered by store
# integration tests, not this smoke.
#
# Fails loudly. It never skips: a missing dependency is a failure with a message
# naming the fix, not a silent pass.
set -euo pipefail

BIN=${BIN:-bin}/substrate-server
PORT=${PORT:-18080}

if [[ ! -x "$BIN" ]]; then
  echo "smoke: $BIN not built. Fix: run 'make build' first." >&2
  exit 1
fi
for cmd in curl; do
  command -v "$cmd" >/dev/null || { echo "smoke: '$cmd' not installed. Fix: install $cmd." >&2; exit 1; }
done

pid=""
cleanup() {
  if [[ -n "$pid" ]]; then kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi
}
trap cleanup EXIT

# Force an empty DSN so a developer-exported SUBSTRATE_DSN cannot turn this
# into a Docker-backed smoke. The process must stay up without Postgres.
unset SUBSTRATE_DSN
"$BIN" -addr "127.0.0.1:$PORT" -dsn "" &
pid=$!

for _ in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then break; fi
  sleep 0.2
done

body=$(curl -fsS "http://127.0.0.1:$PORT/healthz") || {
  echo "smoke: server never answered /healthz on port $PORT" >&2; exit 1; }
echo "smoke: /healthz -> $body"
grep -q '"status":"ok"' <<<"$body" || { echo "smoke: /healthz body missing status=ok" >&2; exit 1; }

code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/readyz")
if [[ "$code" != "503" ]]; then
  echo "smoke: /readyz returned $code, expected 503 with no database (EDD §16)." >&2
  exit 1
fi
echo "smoke: /readyz -> 503 not_ready"
echo "smoke: OK"
