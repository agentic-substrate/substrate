#!/usr/bin/env bash
# Assert a PITR restore produced data. A restore of zero rows is a failure —
# that is the whole point of the weekly job (EDD R10 / issue #27).
#
# Usage: restore-assert.sh <memory_count> <instruction_count> <audit_count>
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: restore-assert.sh <memory_count> <instruction_count> <audit_count>" >&2
  exit 2
fi

memory=$1
instruction=$2
audit=$3

is_uint() {
  [[ "$1" =~ ^[0-9]+$ ]]
}

fail=0
check() {
  local name=$1 val=$2
  if ! is_uint "$val"; then
    echo "restore-assert: ${name} count is not an integer: ${val:-<empty>}" >&2
    fail=1
    return
  fi
  if (( val <= 0 )); then
    echo "restore-assert: ${name} restored 0 rows — a silent empty restore is a failure, not a backup" >&2
    fail=1
  fi
}

check memory "$memory"
check instruction "$instruction"
check audit "$audit"

if (( fail != 0 )); then
  echo "restore-assert: FAIL memory=${memory} instruction=${instruction} audit=${audit}" >&2
  exit 1
fi

echo "restore-assert: OK memory=${memory} instruction=${instruction} audit=${audit}"
