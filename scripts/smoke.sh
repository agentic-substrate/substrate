#!/usr/bin/env bash
# First-run smoke test: build the real binary, boot it against empty state, and
# prove it answers. This catches the class of bug unit tests structurally cannot
# — a binary that compiles and tests green but cannot start.
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

"$BIN" -addr "127.0.0.1:$PORT" &
pid=$!
trap 'kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true' EXIT

for _ in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then break; fi
  sleep 0.2
done

body=$(curl -fsS "http://127.0.0.1:$PORT/healthz") || {
  echo "smoke: server never answered /healthz on port $PORT" >&2; exit 1; }
echo "smoke: /healthz -> $body"
grep -q '"status":"ok"' <<<"$body" || { echo "smoke: /healthz body missing status=ok" >&2; exit 1; }

# Readiness is Postgres-only and there is no store yet, so 503 is the CORRECT
# answer today. When the store lands, flip this expectation to 200 in the same
# commit — a smoke test asserting the wrong readiness is worse than none.
code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/readyz")
if [[ "$code" != "503" ]]; then
  echo "smoke: /readyz returned $code, expected 503 (no store configured yet)." >&2
  echo "       If you wired up Postgres, update this assertion to 200." >&2
  exit 1
fi
echo "smoke: /readyz -> 503 not_ready (expected: no store configured)"
echo "smoke: OK"
