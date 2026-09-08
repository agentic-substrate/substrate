#!/usr/bin/env bash
# Fail if deploy/ (or a supplied tree) contains a credential, a token, or a
# real tailnet hostname. Placeholders like <tailnet> and empty Secret values
# are the only allowed form.
#
# Usage: check-deploy-secrets.sh [dir]
set -euo pipefail

ROOT=${1:-deploy}
if [[ ! -d "$ROOT" ]]; then
  echo "check-deploy-secrets: $ROOT is not a directory" >&2
  exit 2
fi

# Keys whose values must be empty or an obvious <placeholder>.
VALUE_KEY='(password|token|ACCESS_KEY_ID|ACCESS_SECRET_KEY|POSTGRES_PASSWORD|SUBSTRATE_DSN)'

findings=0

# Strip a matching YAML scalar's surrounding quotes.
unquote() {
  local v=$1
  if [[ "$v" == \"*\" && "$v" == *\" ]]; then
    v=${v#\"}; v=${v%\"}
  elif [[ "$v" == \'*\' && "$v" == *\' ]]; then
    v=${v#\'}; v=${v%\'}
  fi
  printf '%s' "$v"
}

# Empty, or contains <placeholder> — never a live value.
is_allowed_value() {
  local v=$1
  [[ -z "$v" ]] && return 0
  [[ "$v" == *\<*\>* ]] && return 0
  return 1
}

while IFS= read -r -d '' f; do
  [[ -f "$f" ]] || continue
  # Binary files are not deploy manifests.
  grep -qI '' "$f" 2>/dev/null || continue

  lineno=0
  pending_env_key=""
  while IFS= read -r line || [[ -n "$line" ]]; do
    lineno=$((lineno + 1))

    if [[ "$line" == *ts.net* ]]; then
      echo "$f:$lineno: tailnet hostname (ts.net) is forbidden; use substrate.<tailnet>" >&2
      echo "  $line" >&2
      findings=$((findings + 1))
    fi

    if [[ "$line" =~ AKIA[0-9A-Z]{16} ]]; then
      echo "$f:$lineno: AWS-style access key id" >&2
      echo "  $line" >&2
      findings=$((findings + 1))
    fi

    if [[ "$line" == *"BEGIN "* && "$line" == *"PRIVATE KEY"* ]]; then
      echo "$f:$lineno: private key PEM" >&2
      echo "  $line" >&2
      findings=$((findings + 1))
    fi

    if [[ "$line" == *github_pat_* || "$line" == *ghp_* ]]; then
      echo "$f:$lineno: GitHub token" >&2
      echo "  $line" >&2
      findings=$((findings + 1))
    fi

    if [[ "$line" == *minioadmin* ]]; then
      echo "$f:$lineno: default MinIO credential" >&2
      echo "  $line" >&2
      findings=$((findings + 1))
    fi

    # A literal in a container `env:` block is `- name: POSTGRES_PASSWORD` on
    # one line and `value: hunter2` on the next: the key on the line carrying
    # the secret is `value`, which matches nothing below. Track the pair.
    if [[ "$line" =~ ^[[:space:]]*-[[:space:]]*name:[[:space:]]*(.*)$ ]]; then
      env_name=$(unquote "$(printf '%s' "${BASH_REMATCH[1]}" | sed 's/#.*//; s/^[[:space:]]*//; s/[[:space:]]*$//')")
      if [[ "$env_name" =~ ^$VALUE_KEY$ ]]; then
        pending_env_key=$env_name
      else
        pending_env_key=""
      fi
    elif [[ "$line" =~ ^[[:space:]]*valueFrom: ]]; then
      pending_env_key=""
    fi

    raw=""
    have_value=0
    if [[ "$line" =~ $VALUE_KEY:[[:space:]]*(.*)$ ]]; then
      raw=${BASH_REMATCH[2]}
      have_value=1
    elif [[ -n "$pending_env_key" ]] && [[ "$line" =~ ^[[:space:]]*value:[[:space:]]*(.*)$ ]]; then
      raw=${BASH_REMATCH[1]}
      have_value=1
      pending_env_key=""
    fi

    if (( have_value )); then
      # Drop a trailing inline comment.
      raw=${raw%%#*}
      # Trim whitespace.
      raw=${raw#"${raw%%[![:space:]]*}"}
      raw=${raw%"${raw##*[![:space:]]}"}
      val=$(unquote "$raw")
      if ! is_allowed_value "$val"; then
        echo "$f:$lineno: non-empty secret value (use \"\" or <placeholder>)" >&2
        echo "  $line" >&2
        findings=$((findings + 1))
      fi
    fi
  done <"$f"
done < <(find "$ROOT" -type f -print0)

if (( findings > 0 )); then
  echo "check-deploy-secrets: FAIL ($findings finding(s) under $ROOT)" >&2
  exit 1
fi

echo "check-deploy-secrets: OK $ROOT"
