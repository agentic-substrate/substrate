#!/bin/sh
# Assert a PITR restore produced data. A restore of zero rows is a failure —
# that is the whole point of the weekly job (EDD R10 / issue #27).
#
# Usage: restore-assert.sh <memory_count> <instruction_count> <audit_count>
#
# POSIX sh only, and that is load-bearing: this is invoked as "$ASSERT" from
# restore-test.sh (#!/bin/sh) inside a kubectl image. rancher/kubectl and
# alpine/k8s are busybox-only, so a bash script with [[ ]], =~ and (( )) dies
# with a syntax error AFTER the counts were collected — and the operator then
# debugs the backups instead of the shell.
set -eu

if [ $# -ne 3 ]; then
  echo "usage: restore-assert.sh <memory_count> <instruction_count> <audit_count>" >&2
  exit 2
fi

memory=$1
instruction=$2
audit=$3

# Unsigned decimal, at least one digit, nothing else. Implemented with a case
# glob rather than a regex because busybox sh has no [[ =~ ]]: reject if the
# value is empty, or contains any character outside 0-9.
is_uint() {
  case "$1" in
    "") return 1 ;;
    *[!0-9]*) return 1 ;;
    *) return 0 ;;
  esac
}

# True when the value is all zeros ("0", "00", ...). Avoids arithmetic on a
# string that may have a leading zero, which some shells read as octal.
is_zero() {
  case "$1" in
    *[!0]*) return 1 ;;
    *) return 0 ;;
  esac
}

fail=0
check() {
  name=$1
  val=$2
  if ! is_uint "$val"; then
    echo "restore-assert: ${name} count is not an integer: ${val:-<empty>}" >&2
    fail=1
    return
  fi
  if is_zero "$val"; then
    echo "restore-assert: ${name} restored 0 rows — a silent empty restore is a failure, not a backup" >&2
    fail=1
  fi
}

check memory "$memory"
check instruction "$instruction"
check audit "$audit"

if [ "$fail" -ne 0 ]; then
  echo "restore-assert: FAIL memory=${memory} instruction=${instruction} audit=${audit}" >&2
  exit 1
fi

echo "restore-assert: OK memory=${memory} instruction=${instruction} audit=${audit}"
