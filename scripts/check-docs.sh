#!/usr/bin/env bash
# Docs-currency gate. Converts "keep the docs up to date" — which has no failure
# condition — into a lookup: if a PR touches a surface, it must touch that
# surface's doc, or state a real reason.
#
# Escape hatch: put `Docs-not-needed: <reason>` in the PR body or any commit
# message on the branch. The reason must be at least 30 characters, because a
# free skip is always taken and a written justification is only taken when true.
#
# Usage: scripts/check-docs.sh [base-ref]   (default: origin/main)
set -euo pipefail

BASE=${1:-origin/main}
git rev-parse --verify --quiet "$BASE" >/dev/null || {
  echo "check-docs: base ref '$BASE' not found. Fix: git fetch origin main." >&2; exit 1; }

changed=$(git diff --name-only "$BASE"...HEAD)
[[ -n "$changed" ]] || { echo "check-docs: no changes."; exit 0; }

# surface-glob<TAB>required-doc<TAB>why
PAIRS=$(cat <<'EOF'
cmd/*	README.md	the commands and quickstart README documents
scripts/*	CONTRIBUTING.md	the verification commands contributors run
Makefile	CONTRIBUTING.md	the verification commands contributors run
.github/workflows/*	CONTRIBUTING.md	what CI requires of a PR
migrations/*	docs/ops/runbook.md	the migration and restore procedure
deploy/*	docs/ops/runbook.md	how the system is deployed and recovered
EOF
)

matches() { case "$1" in $2) return 0;; *) return 1;; esac; }

fail=0
while IFS=$'\t' read -r glob doc why; do
  [[ -n "$glob" ]] || continue
  hit=""
  while read -r f; do
    [[ -n "$f" ]] || continue
    if matches "$f" "$glob*" || matches "$f" "$glob"; then hit=$f; break; fi
  done <<<"$changed"
  [[ -n "$hit" ]] || continue
  if ! grep -qxF "$doc" <<<"$changed"; then
    echo "check-docs: '$hit' changed but '$doc' did not — it documents $why." >&2
    fail=1
  fi
done <<<"$PAIRS"

if [[ $fail -eq 0 ]]; then echo "check-docs: OK"; exit 0; fi

# Escape hatch, checked only once something actually failed.
reasons=$( { git log --format=%B "$BASE"..HEAD; printf '%s\n' "${PR_BODY:-}"; } \
  | sed -n 's/^[[:space:]]*Docs-not-needed:[[:space:]]*//p' )
while read -r r; do
  [[ -n "$r" ]] || continue
  if [[ ${#r} -ge 30 ]]; then
    echo "check-docs: waived — Docs-not-needed: $r"
    exit 0
  fi
  echo "check-docs: 'Docs-not-needed: $r' is only ${#r} characters; 30 are required." >&2
done <<<"$reasons"

echo "check-docs: FAILED. Update the doc, or add to the PR body:" >&2
echo "  Docs-not-needed: <at least 30 characters saying why this genuinely needs no doc change>" >&2
exit 1
