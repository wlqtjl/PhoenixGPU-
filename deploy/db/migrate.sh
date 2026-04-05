#!/usr/bin/env bash
# PhoenixGPU Database Migration Tool
#
# Simple, dependency-free schema migration runner for PostgreSQL.
# Applies numbered SQL files in order, tracks applied migrations
# in a `schema_migrations` table to ensure idempotency.
#
# Usage:
#   deploy/db/migrate.sh                    # Apply pending migrations
#   deploy/db/migrate.sh status             # Show migration status
#   deploy/db/migrate.sh --dry-run          # Show what would be applied
#
# Environment:
#   BILLING_DB_DSN  PostgreSQL connection string (required)
#                   e.g. "postgres://user:pass@localhost:5432/phoenixgpu?sslmode=disable"
#
# Migration files must be named NNN_description.sql (e.g. 001_initial_schema.sql)
# and placed in the same directory as this script.
#
# Copyright 2025 PhoenixGPU Authors
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRY_RUN=false
ACTION="migrate"

# ── Parse arguments ─────────────────────────────────────────────
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=true ;;
    status)    ACTION="status" ;;
    -h|--help)
      echo "Usage: $0 [status] [--dry-run]"
      echo ""
      echo "Commands:"
      echo "  (default)   Apply pending migrations"
      echo "  status      Show applied/pending migrations"
      echo ""
      echo "Options:"
      echo "  --dry-run   Show what would be applied without executing"
      echo ""
      echo "Environment:"
      echo "  BILLING_DB_DSN  PostgreSQL connection string (required)"
      exit 0
      ;;
  esac
done

# ── Validate environment ────────────────────────────────────────
if [[ -z "${BILLING_DB_DSN:-}" ]]; then
  echo "ERROR: BILLING_DB_DSN environment variable is required"
  echo "Example: export BILLING_DB_DSN='postgres://user:pass@localhost:5432/phoenixgpu'"
  exit 1
fi

if ! command -v psql &>/dev/null; then
  echo "ERROR: psql is not installed or not in PATH"
  exit 1
fi

# ── Ensure tracking table exists ─────────────────────────────────
psql "$BILLING_DB_DSN" -q <<'SQL'
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     TEXT        PRIMARY KEY,
    filename    TEXT        NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    checksum    TEXT        NOT NULL
);
COMMENT ON TABLE schema_migrations IS 'Tracks applied database migrations for PhoenixGPU billing';
SQL

# ── Discover migration files ─────────────────────────────────────
# Sorted by filename to ensure correct order
mapfile -t MIGRATIONS < <(find "$SCRIPT_DIR" -maxdepth 1 -name '[0-9][0-9][0-9]_*.sql' -printf '%f\n' | sort)

if [[ ${#MIGRATIONS[@]} -eq 0 ]]; then
  echo "No migration files found in $SCRIPT_DIR"
  exit 0
fi

# ── Get already applied migrations ───────────────────────────────
APPLIED=$(psql "$BILLING_DB_DSN" -t -A -c "SELECT version FROM schema_migrations ORDER BY version;")

# ── Status command ───────────────────────────────────────────────
if [[ "$ACTION" == "status" ]]; then
  echo "PhoenixGPU Database Migrations"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  printf "%-8s %-40s %s\n" "VERSION" "FILENAME" "STATUS"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  for f in "${MIGRATIONS[@]}"; do
    version="${f%%_*}"
    if echo "$APPLIED" | grep -q "^${version}$"; then
      applied_at=$(psql "$BILLING_DB_DSN" -t -A -c "SELECT applied_at FROM schema_migrations WHERE version='${version}';")
      printf "%-8s %-40s ✅ Applied (%s)\n" "$version" "$f" "$applied_at"
    else
      printf "%-8s %-40s ⏳ Pending\n" "$version" "$f"
    fi
  done
  exit 0
fi

# ── Apply pending migrations ─────────────────────────────────────
PENDING=0
APPLIED_COUNT=0

for f in "${MIGRATIONS[@]}"; do
  version="${f%%_*}"

  # Skip already applied
  if echo "$APPLIED" | grep -q "^${version}$"; then
    continue
  fi

  PENDING=$((PENDING + 1))
  filepath="$SCRIPT_DIR/$f"
  checksum=$(sha256sum "$filepath" | awk '{print $1}')

  if [[ "$DRY_RUN" == "true" ]]; then
    echo "[DRY-RUN] Would apply: $f (checksum: ${checksum:0:16}…)"
    continue
  fi

  echo "→ Applying migration: $f"

  # Apply in a transaction
  psql "$BILLING_DB_DSN" -v ON_ERROR_STOP=1 --single-transaction <<SQL
-- Migration: $f
\i $filepath

-- Record migration
INSERT INTO schema_migrations (version, filename, checksum)
VALUES ('$version', '$f', '$checksum');
SQL

  if [[ $? -eq 0 ]]; then
    APPLIED_COUNT=$((APPLIED_COUNT + 1))
    echo "  ✓ Applied: $f"
  else
    echo "  ✗ FAILED: $f"
    echo "  Migration aborted. Fix the issue and re-run."
    exit 1
  fi
done

if [[ "$DRY_RUN" == "true" ]]; then
  echo ""
  echo "$PENDING migration(s) would be applied."
elif [[ $PENDING -eq 0 ]]; then
  echo "✓ Database is up to date. No pending migrations."
else
  echo ""
  echo "✓ Applied $APPLIED_COUNT migration(s) successfully."
fi
