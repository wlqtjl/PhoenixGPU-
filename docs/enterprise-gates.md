# Enterprise Gates (Obra/Superpowers-style)

This file tracks hard release gates for production readiness.

## Gate-1: Build/Dependency Red Lines

**Objective**
- No syntax errors in entry binaries.
- Dependency issues are explicit and reproducible in CI.

**Current scope**
1. Fix `cmd/phoenix-controller` syntax errors first.
2. Keep `go.mod` dependency list stable (no unresolvable additions under restricted proxy).
3. Record exact failing commands and blockers.

**Acceptance criteria**
- `go test ./cmd/phoenix-controller -count=1` no longer fails due syntax errors.
- Remaining failures are dependency/network/go.sum blockers only (tracked).

**Status (this round)**
- ✅ Syntax error fixed in `cmd/phoenix-controller/main.go` import alias.
- ⚠️ Dependency/go.sum blockers remain in environment for full compile/test.

## Gate-2: Full Test Green

**Objective**
- `go test ./...` must pass in CI with deterministic module resolution.

**Current known blockers**
- Missing go.sum entries for multiple transitive dependencies.
- Failing test in `pkg/vgpu` (`TestDensity_TwoTasksShareOneGPU`).

**Execution tooling**
- `hack/profile-matrix.sh` now provides a repeatable build-profile test matrix:
  - default (`go test ./...`)
  - single-tag lanes (`k8sfull`, `checkpointfull`, `migrationfull`, `billingfull`)
  - multi-tag combo lanes for integration pressure checks.

## Gate-3: Production Semantics

**Objective**
- non-mock path must use real K8s client.
- migration status durable (CRD/DB), with audit logs.

**Execution tooling**
- `hack/preflight-check.sh` enforces runtime intent vs build tags before deployment.
  - Example: `PHOENIX_ENABLE_REAL_K8S=true` requires `k8sfull` in `PHOENIX_BUILD_TAGS`.
  - Example: `PHOENIX_ENABLE_MIGRATION=true` requires `migrationfull` in `PHOENIX_BUILD_TAGS`.
- Shared API domain types now live in `pkg/apitypes`, removing `pkg/k8s -> cmd/api-server/internal` reverse import for `k8sfull` builds.
- Migration status endpoint now supports durable file-backed state (`--migration-status-file`) and JSONL audit events (`--migration-audit-file`) for restart-safe tracking and auditability.
- Migration status backend now supports `--migration-store=crd` (requires `migrationfull`) for PhoenixJob CRD `status.migration` persistence.

## Gate-4: Enterprise Runtime Controls

**Objective**
- AuthN/AuthZ, rate limiting, circuit breaking, observability dashboards/alerts.
