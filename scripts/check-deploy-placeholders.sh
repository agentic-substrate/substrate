#!/usr/bin/env bash
# Two questions the secret scanner cannot answer, because a placeholder that
# looks like a real value is not a secret — it is a manifest that applies
# silently wrong.
#
#   --committed   (default, what CI runs)
#                 The tree in git is still fully un-substituted. Every image
#                 reference and every sensitive value is a <bracketed>
#                 placeholder or an empty string, and every placeholder in
#                 deploy/ is named in the runbook so there is no eighth file
#                 the operator has to find by grepping.
#
#   --substituted (what the operator runs after step 2, before apply)
#                 Nothing is left to substitute. No <placeholder> survives,
#                 every image is pinned by digest rather than by a tag, and no
#                 sensitive value is still empty.
#
# The failure this exists to stop: `imageName: substrate-pg:16-pgvector` and
# `image: ko.local/substrate-server:latest` read as already-substituted values,
# so the secret scanner and every manifest test wave them through, and
# `kubectl apply` accepts them — then the pods sit in ImagePullBackOff, or
# worse pull something that happens to exist. `password: ""` is the same class:
# it applies as a literal empty password and the scanner explicitly allows it.
#
# Usage: check-deploy-placeholders.sh [--committed|--substituted] [dir]
set -euo pipefail

MODE=committed
ROOT=deploy
for arg in "$@"; do
  case "$arg" in
    --committed) MODE=committed ;;
    --substituted) MODE=substituted ;;
    -*) echo "check-deploy-placeholders: unknown flag $arg" >&2; exit 2 ;;
    *) ROOT=$arg ;;
  esac
done

if [[ ! -d "$ROOT" ]]; then
  echo "check-deploy-placeholders: $ROOT is not a directory" >&2
  exit 2
fi

RUNBOOK=docs/ops/runbook.md

# Values that must never ship as a literal.
SENSITIVE_KEY='(password|token|ACCESS_KEY_ID|ACCESS_SECRET_KEY|POSTGRES_PASSWORD|SUBSTRATE_DSN)'
# Image references. `imageName` is CNPG's spelling.
IMAGE_KEY='(image|imageName)'

findings=0
report() {
  echo "$1" >&2
  findings=$((findings + 1))
}

trim() {
  local v=$1
  v=${v%%#*}
  v=${v#"${v%%[![:space:]]*}"}
  v=${v%"${v##*[![:space:]]}"}
  if [[ "$v" == \"*\" && ${#v} -ge 2 ]]; then
    v=${v#\"}; v=${v%\"}
  elif [[ "$v" == \'*\' && ${#v} -ge 2 ]]; then
    v=${v#\'}; v=${v%\'}
  fi
  printf '%s' "$v"
}

is_placeholder() { [[ "$1" == *\<*\>* ]]; }

declare -A seen_placeholders=()

files=()
while IFS= read -r -d '' f; do
  grep -qI '' "$f" 2>/dev/null || continue
  files+=("$f")
done < <(find "$ROOT" -type f -print0 | sort -z)

for f in "${files[@]}"; do
  lineno=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    lineno=$((lineno + 1))
    # Comments explain the placeholders; they are not manifest values.
    stripped=${line#"${line%%[![:space:]]*}"}
    is_comment=0
    [[ "$stripped" == \#* ]] && is_comment=1

    # Inventory every <placeholder> token for the runbook cross-check. YAML
    # only: shell scripts are executed, not substituted, and their `<empty>`
    # style placeholders in message strings are not operator work items.
    if [[ "$f" == *.yaml || "$f" == *.yml ]] && [[ "$line" =~ \<([a-z0-9][a-z0-9-]*)\> ]]; then
      seen_placeholders["${BASH_REMATCH[1]}"]=1
    fi

    (( is_comment )) && continue

    if [[ "$line" =~ ^[[:space:]]*-?[[:space:]]*$IMAGE_KEY:[[:space:]]*(.*)$ ]]; then
      val=$(trim "${BASH_REMATCH[2]}")
      case "$MODE" in
        committed)
          if ! is_placeholder "$val"; then
            report "$f:$lineno: image is not a <placeholder>: '$val'
  A tag-shaped literal here is indistinguishable from a substituted value, so
  nothing catches it before apply. Write it as <some-image-digest>."
          fi
          ;;
        substituted)
          if is_placeholder "$val"; then
            report "$f:$lineno: image still contains a placeholder: '$val'"
          elif [[ "$val" != *@sha256:* ]]; then
            report "$f:$lineno: image is not pinned by digest: '$val'
  Use repo/name@sha256:<64 hex>. A tag is mutable: the weekly restore job would
  silently gain a new kubectl (or a new shell) between one Sunday and the next."
          fi
          ;;
      esac
    fi

    if [[ "$line" =~ ^[[:space:]]*$SENSITIVE_KEY:[[:space:]]*(.*)$ ]]; then
      val=$(trim "${BASH_REMATCH[2]}")
      case "$MODE" in
        committed)
          if [[ -n "$val" ]] && ! is_placeholder "$val"; then
            report "$f:$lineno: sensitive value is a literal: use \"\" or a <placeholder>"
          fi
          ;;
        substituted)
          if [[ -z "$val" ]]; then
            report "$f:$lineno: sensitive value is still empty.
  An empty string applies as a literal empty credential; it is not 'unset'."
          elif is_placeholder "$val"; then
            report "$f:$lineno: sensitive value still contains a placeholder"
          fi
          ;;
      esac
    fi
  done <"$f"
done

if [[ "$MODE" == substituted ]]; then
  # Nothing anywhere may still say <...>, including hostnames and namespaces.
  for f in "${files[@]}"; do
    lineno=0
    while IFS= read -r line || [[ -n "$line" ]]; do
      lineno=$((lineno + 1))
      stripped=${line#"${line%%[![:space:]]*}"}
      [[ "$stripped" == \#* ]] && continue
      if [[ "$line" =~ \<[a-z0-9][a-z0-9-]*\> ]]; then
        report "$f:$lineno: unsubstituted placeholder
  $line"
      fi
    done <"$f"
  done
fi

if [[ "$MODE" == committed && -f "$RUNBOOK" ]]; then
  for ph in "${!seen_placeholders[@]}"; do
    if ! grep -qF -- "<$ph>" "$RUNBOOK"; then
      report "$RUNBOOK: placeholder <$ph> appears in $ROOT but is not named in the runbook.
  The operator substitutes by hand and there is no values file; a placeholder
  the checklist does not mention is one nobody fills."
    fi
  done
fi

if (( findings > 0 )); then
  echo "check-deploy-placeholders: FAIL ($findings finding(s) under $ROOT, mode=$MODE)" >&2
  exit 1
fi

echo "check-deploy-placeholders: OK $ROOT (mode=$MODE)"
