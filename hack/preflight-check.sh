#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# This script checks deployment intent against build-profile requirements.
# Configure via env vars:
#   PHOENIX_ENABLE_REAL_K8S=true      -> requires k8sfull profile
#   PHOENIX_ENABLE_MIGRATION=true     -> requires migrationfull profile
#   PHOENIX_ENABLE_CHECKPOINT=true    -> requires checkpointfull profile
#   PHOENIX_ENABLE_BILLING=true       -> requires billingfull profile
#   PHOENIX_BUILD_TAGS="a,b,c"       -> explicit active tags for check

truthy() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|on|ON) return 0 ;;
    *) return 1 ;;
  esac
}

declare -A required=(
  [PHOENIX_ENABLE_REAL_K8S]="k8sfull"
  [PHOENIX_ENABLE_MIGRATION]="migrationfull"
  [PHOENIX_ENABLE_CHECKPOINT]="checkpointfull"
  [PHOENIX_ENABLE_BILLING]="billingfull"
)

active_tags=()
if [[ -n "${PHOENIX_BUILD_TAGS:-}" ]]; then
  IFS=',' read -r -a active_tags <<<"$PHOENIX_BUILD_TAGS"
fi

has_tag() {
  local needle="$1"
  for t in "${active_tags[@]}"; do
    [[ "$t" == "$needle" ]] && return 0
  done
  return 1
}

failures=0
for env_key in "${!required[@]}"; do
  tag="${required[$env_key]}"
  value="${!env_key:-}"
  if truthy "$value"; then
    if [[ ${#active_tags[@]} -eq 0 ]]; then
      echo "ERROR: $env_key=true requires build tag '$tag', but PHOENIX_BUILD_TAGS is empty."
      failures=$((failures + 1))
      continue
    fi
    if ! has_tag "$tag"; then
      echo "ERROR: $env_key=true requires build tag '$tag', active tags: ${PHOENIX_BUILD_TAGS}"
      failures=$((failures + 1))
    else
      echo "OK: $env_key=true satisfied by build tag '$tag'"
    fi
  fi
done

if [[ $failures -gt 0 ]]; then
  echo "preflight failed with $failures issue(s)"
  exit 1
fi

echo "preflight passed"
