# Gate-1 Round 3 Closure (Optional Adapters / Build Profiles)

## Objective
Reduce default dependency graph (Prometheus/Zap/K8s/Postgres-heavy paths) so `go test ./...` is reproducible in restricted environments.

## Strategy
Switch heavy runtime modules to **opt-in build profiles** and provide default lightweight stubs:

- `controllerfull` for controller-runtime based binaries/components.
- `checkpointfull` for CRIU uploader/PVC/S3 full stack.
- `k8sfull` for real K8s client integration package.
- `migrationfull` for live migration execution stack.
- `billingfull` for billing engine + postgres store.

## Closed items (this round)
1. Default build no longer requires Prometheus/Zap-heavy checkpoint uploader/runtime.
2. Default build no longer requires controller-runtime/k8s transitive graph for controller package.
3. Default build no longer requires internal-package-cross-import in `pkg/k8s`.
4. Oversubscription accounting bug fixed (`swapUsed` now counts only overflow after remaining physical VRAM).

## Verification commands
1. `go test ./...`
2. `go test ./cmd/api-server -count=1`

## Result
- ✅ `go test ./...` passes in current environment.
- ✅ `go test ./cmd/api-server -count=1` remains green.

## Remaining risks
- Full enterprise runtime features are now behind explicit build tags and are **not** active in default build.
- Next gate should validate each full profile explicitly in a dependency-complete CI environment.
