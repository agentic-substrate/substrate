#!/usr/bin/env bash
# Docs-currency gate for the CLI's command map. docs/ops/setup.md is the
# operator reference for the `substrate` binary, and it carries a fenced
# "command map" block (between `<!-- command-map:start -->` and `:end`) that
# lists every `substrate <verb>` and its subverbs. This script builds the real
# binary, walks its `--help` output, and fails if either list holds a command
# the other does not — so a renamed verb (like the `import cutover` ->
# `adapter install` rename #97 tracks) cannot leave a stale reference with CI
# green, the way it left the runbook stale before this script existed (#102).
#
# Both levels are compared: the top-level verbs, and each verb's subcommands.
# `help` is cobra's own built-in and is excluded on both sides; it is not part
# of substrate's documented surface.
#
# Usage: scripts/check-cli-commands.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

DOC=docs/ops/setup.md

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
BIN="$TMP/substrate"
go build -o "$BIN" ./cmd/substrate

# --- what the reference documents ------------------------------------------
# Each line is `substrate <verb> [sub|sub|sub] [# comment]`. Emits one
# "verb" or "verb sub" pair per line.
map=$(awk '/<!-- command-map:start -->/{f=1;next}/<!-- command-map:end -->/{f=0}f' "$DOC" \
  | sed 's/#.*//' \
  | grep -E '^substrate +[a-z]' || true)

if [[ -z "$map" ]]; then
  echo "check-cli-commands: no command map found in $DOC (expected between" >&2
  echo "  <!-- command-map:start --> and <!-- command-map:end -->)." >&2
  exit 1
fi

doc_pairs=$(echo "$map" | awk '{
  verb = $2
  print verb
  if (NF >= 3) { n = split($3, subs, "|"); for (i = 1; i <= n; i++) if (subs[i] != "") print verb " " subs[i] }
}' | sort -u)

# --- what the binary actually exposes --------------------------------------
# The subverbs of a command: its child commands, or -- for a leaf that takes a
# fixed argument set instead (`completion bash|zsh|fish`) -- its ValidArgs, read
# out of cobra's own completion machinery so the reference is held to the same
# list the shell completions offer.
help_children() {
  local kids
  kids=$("$BIN" "$@" --help 2>&1 \
    | awk '/^Available Commands:/{f=1;next}/^$/{f=0}f{print $1}' \
    | grep -Ev '^(help)?$' || true)
  if [[ -n "$kids" || $# -eq 0 ]]; then
    echo "$kids"
    return
  fi
  "$BIN" __complete "$@" "" 2>/dev/null | grep -Ev '^:|^Completion ended' || true
}

runtime_verbs=$(help_children)
if [[ -z "$runtime_verbs" ]]; then
  echo "check-cli-commands: substrate --help exposed no command tree at all." >&2
  echo "  Either the build is broken or NewRoot registers nothing; fix that before this gate." >&2
  exit 1
fi

runtime_pairs=$(
  for verb in $runtime_verbs; do
    echo "$verb"
    for sub in $(help_children "$verb"); do echo "$verb $sub"; done
  done | sort -u
)

missing_from_help=$(comm -23 <(echo "$doc_pairs") <(echo "$runtime_pairs"))
missing_from_readme=$(comm -13 <(echo "$doc_pairs") <(echo "$runtime_pairs"))

status=0
if [[ -n "$missing_from_help" ]]; then
  echo "check-cli-commands: the command map in $DOC documents commands substrate does not expose:" >&2
  echo "$missing_from_help" | sed 's/^/  substrate /' >&2
  status=1
fi
if [[ -n "$missing_from_readme" ]]; then
  echo "check-cli-commands: substrate exposes commands the command map in $DOC does not document:" >&2
  echo "$missing_from_readme" | sed 's/^/  substrate /' >&2
  status=1
fi

if [[ $status -eq 0 ]]; then
  echo "check-cli-commands: command map in $DOC matches substrate --help ($(echo "$runtime_pairs" | wc -l | tr -d ' ') commands)"
fi
exit $status
