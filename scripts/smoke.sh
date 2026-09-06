#!/usr/bin/env bash
# First-run smoke test: build the real binary, boot it against Postgres, and
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

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cid=""
pid=""
cleanup() {
  if [[ -n "$pid" ]]; then kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi
  if [[ -n "$cid" ]]; then docker stop "$cid" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

# /readyz is Postgres-only. Prefer SUBSTRATE_DSN; otherwise boot a throwaway
# Postgres 16 with pgvector (official image, or testdata/pgvector if Hub is down).
if [[ -z "${SUBSTRATE_DSN:-}" ]]; then
  command -v docker >/dev/null || {
    echo "smoke: docker not installed. Fix: install docker or set SUBSTRATE_DSN." >&2
    exit 1
  }
  image=""
  if docker image inspect pgvector/pgvector:pg16 >/dev/null 2>&1; then
    image=pgvector/pgvector:pg16
  elif docker image inspect substrate-test-pgvector:16 >/dev/null 2>&1; then
    image=substrate-test-pgvector:16
  elif docker pull pgvector/pgvector:pg16 >/dev/null; then
    image=pgvector/pgvector:pg16
  else
    echo "smoke: pulling pgvector/pgvector:pg16 failed; building testdata/pgvector" >&2
    docker build -t substrate-test-pgvector:16 "$ROOT/testdata/pgvector" || {
      echo "smoke: could not pull pgvector/pgvector:pg16 or build testdata/pgvector." >&2
      echo "       Fix: install Docker, start the daemon, and allow an image pull or build." >&2
      exit 1
    }
    image=substrate-test-pgvector:16
  fi
  cid=$(docker run -d --rm \
    -e POSTGRES_PASSWORD=smoke \
    -e POSTGRES_USER=postgres \
    -e POSTGRES_DB=substrate \
    -p 127.0.0.1::5432 \
    "$image") || {
    echo "smoke: docker run failed for $image. Fix: start the Docker daemon." >&2
    exit 1
  }
  hostport=$(docker port "$cid" 5432/tcp | head -1 | sed 's/.*://')
  if [[ -z "$hostport" ]]; then
    echo "smoke: could not read the published postgres port for $cid" >&2
    exit 1
  fi
  ready=0
  for _ in $(seq 1 50); do
    if docker exec "$cid" pg_isready -U postgres >/dev/null 2>&1; then ready=1; break; fi
    sleep 0.2
  done
  if [[ "$ready" -ne 1 ]]; then
    echo "smoke: postgres in $image never became ready" >&2
    exit 1
  fi
  SUBSTRATE_DSN="postgres://postgres:smoke@127.0.0.1:${hostport}/substrate?sslmode=disable"
fi

"$BIN" -addr "127.0.0.1:$PORT" -dsn "$SUBSTRATE_DSN" &
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
if [[ "$code" != "200" ]]; then
  echo "smoke: /readyz returned $code, expected 200 (Postgres reachable after migrate)." >&2
  exit 1
fi
echo "smoke: /readyz -> 200 ok"
echo "smoke: OK"
