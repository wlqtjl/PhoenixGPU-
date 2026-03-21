# Gate-1 Round 2 Closure (Dependency Governance)

## Scope
This round focused on dependency/go.sum blockers with priority on checkpoint/S3.

## Actions taken
1. Made S3 backend optional via build tags:
   - `pkg/checkpoint/storage_s3.go` now requires `-tags s3`.
   - Added default-build stub `pkg/checkpoint/storage_s3_stub.go` so non-S3 builds compile without AWS SDK.
2. Preserved API compatibility (`S3Config`, `S3Backend`, `NewS3Backend`) while returning explicit disabled errors in default builds.

## Verification commands
1. `go test ./pkg/checkpoint -count=1`
2. `go test ./cmd/phoenix-controller -count=1`
3. `go test ./cmd/api-server -count=1`

## Result summary
- `cmd/api-server` remains green.
- Checkpoint/controller still blocked by remaining go.sum/module issues (Prometheus/Zap/K8s transitive modules and network-restricted module download), but S3 AWS-module hard blocker is now isolated behind an opt-in build tag.

## Failure attribution closure list
### Closed in this round
- S3 AWS SDK import hard requirement in default build path.

### Remaining open
- go.sum entries missing for transitive modules under restricted proxy.
- broader module graph reproducibility for full `go test ./...`.
- functional test failure in `pkg/vgpu` (tracked in Gate-2).
