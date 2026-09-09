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

# --- every invocation the docs tell an operator to run ---------------------
# The command map above covers one block in one file. The runbook's rollback
# commands live in fenced blocks in another, and a rename that leaves them
# stale is the failure this gate exists for (#114): the runbook is what someone
# reads during an incident, and #104's rename left it wrong with CI green.
#
# Only FENCED blocks are scanned. Prose names old commands on purpose --
# runbook.md explains that `substrate import cutover` is now `adapter install`
# -- and a gate failing on documented history would teach people to delete the
# history rather than fix the command.
invocation_pairs=$(
  for f in README.md $(find docs -name '*.md' | sort); do
    awk -v file="$f" '
      /^[[:space:]]*```/ { fence = !fence; next }
      !fence { next }
      {
        line = $0
        sub(/^[[:space:]]*\$[[:space:]]+/, "", line)      # a leading shell prompt
        # The substrate binary itself, never substrate-adapter or -server.
        if (match(line, /(^|[[:space:]]|\/)substrate([[:space:]]|$)/) == 0) next
        rest = substr(line, RSTART + RLENGTH)
        n = split(rest, tok, /[[:space:]]+/)
        verb = ""; sub_ = ""
        for (i = 1; i <= n; i++) {
          if (tok[i] == "" || tok[i] ~ /^-/) continue
          if (verb == "") { verb = tok[i]; continue }
          sub_ = tok[i]; break
        }
        if (verb == "" || verb !~ /^[a-z][a-z-]*$/) next
        if (sub_ !~ /^[a-z][a-z-]*$/) sub_ = ""
        flags = ""
        for (i = 1; i <= n; i++) {
          f_ = tok[i]
          sub(/=.*$/, "", f_)
          # A multi-character single-dash flag is always wrong under cobra: it
          # parses as a run of shorthands. This is the #104 shape (-root, -commit).
          if (f_ ~ /^-[a-z][a-z-]+$/ || f_ ~ /^--[a-z][a-z-]*$/) flags = flags " " f_
        }
        print (sub_ == "" ? verb : verb " " sub_) "\t" file "\t" flags
      }' "$f"
  done | sort -u
)

if [[ -n "$invocation_pairs" ]]; then
  unknown=$(
    while IFS=$'\t' read -r pair file flags; do
      [[ -n "$pair" ]] || continue
      if ! grep -qxF "$pair" <<<"$runtime_pairs"; then
        echo "  substrate $pair   ($file)"
        continue
      fi
      # The verb exists; now the flags it is shown with. `--help` lists both
      # its own and its inherited flags, which is exactly the set a reader may
      # legitimately pass.
      # shellcheck disable=SC2086  # $pair is "verb sub" and must word-split
      known=$("$BIN" $pair --help 2>&1 | grep -oE '(^|[[:space:]])--[a-z][a-z-]*' | tr -d ' ' | sort -u)
      for fl in $flags; do
        [[ -n "$fl" ]] || continue
        if [[ "$fl" != --* ]]; then
          echo "  substrate $pair $fl   ($file) -- single-dash flag; cobra reads it as shorthands"
          continue
        fi
        grep -qxF -- "$fl" <<<"$known" || echo "  substrate $pair $fl   ($file) -- no such flag"
      done
    done <<<"$invocation_pairs" | sort -u
  )
  if [[ -n "$unknown" ]]; then
    echo "check-cli-commands: the docs show commands or flags substrate does not expose:" >&2
    echo "$unknown" >&2
    echo "  Fix the invocation, or move the reference out of a fenced block if it" >&2
    echo "  is deliberately naming a command that no longer exists." >&2
    status=1
  fi
fi

if [[ $status -eq 0 ]]; then
  echo "check-cli-commands: command map in $DOC matches substrate --help ($(echo "$runtime_pairs" | wc -l | tr -d ' ') commands)"
  echo "check-cli-commands: $(echo "$invocation_pairs" | grep -c .) documented invocation(s) across README.md and docs/ all resolve"
fi
exit $status
