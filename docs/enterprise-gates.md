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

## Gate-3: Production Semantics

**Objective**
- non-mock path must use real K8s client.
- migration status durable (CRD/DB), with audit logs.

## Gate-4: Enterprise Runtime Controls

**Objective**
- AuthN/AuthZ, rate limiting, circuit breaking, observability dashboards/alerts.

