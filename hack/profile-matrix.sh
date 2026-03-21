#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

usage() {
  cat <<USAGE
Usage: $0 [--profile <name>]... [--list]

Profiles:
  default
  k8sfull
  checkpointfull
  migrationfull
  billingfull
  k8sfull+migrationfull
  checkpointfull+billingfull
  k8sfull+checkpointfull+migrationfull+billingfull
USAGE
}

profiles=(
  "default"
  "k8sfull"
  "checkpointfull"
  "migrationfull"
  "billingfull"
  "k8sfull+migrationfull"
  "checkpointfull+billingfull"
  "k8sfull+checkpointfull+migrationfull+billingfull"
)

selected=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile)
      shift
      [[ $# -gt 0 ]] || { echo "error: --profile requires a value" >&2; exit 2; }
      selected+=("$1")
      ;;
    --list)
      printf '%s\n' "${profiles[@]}"
      exit 0
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if [[ ${#selected[@]} -eq 0 ]]; then
  selected=("${profiles[@]}")
fi

run_profile() {
  local profile="$1"
  local cmd
  if [[ "$profile" == "default" ]]; then
    cmd="go test ./..."
  else
    cmd="go test -tags ${profile//+/,} ./..."
  fi

  echo "==> [$profile] $cmd"
  if bash -lc "$cmd"; then
    echo "PASS: $profile"
    return 0
  fi

  echo "FAIL: $profile"
  return 1
}

failures=0
for p in "${selected[@]}"; do
  if ! run_profile "$p"; then
    failures=$((failures + 1))
  fi
done

if [[ $failures -gt 0 ]]; then
  echo "profile matrix completed with $failures failure(s)"
  exit 1
fi

echo "profile matrix passed"
