#!/usr/bin/env bash
# Wrapper so `scripts/restore-assert.sh` stays the operator-facing path.
# The CronJob mounts deploy/restore/restore-assert.sh (kustomize cannot load
# files outside deploy/).
set -euo pipefail
exec "$(dirname "$0")/../deploy/restore/restore-assert.sh" "$@"
