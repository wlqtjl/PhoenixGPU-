# PhoenixGPU Deployment Verification Runbook

This document provides step-by-step procedures for verifying all 10 core
features of a PhoenixGPU deployment.

## Quick Start

```bash
# Mock mode (no cluster required — runs Go + WebUI tests only)
bash hack/validate-deployment.sh --mock

# Full validation (requires running cluster)
bash hack/validate-deployment.sh

# Validate specific API server
bash hack/validate-deployment.sh --api-url http://10.0.0.5:8090

# Run only the Go validation suite
go test ./test/validation/... -v
```

## Prerequisites

| Tool     | Required For       | Install                              |
|----------|--------------------|--------------------------------------|
| Go 1.22+ | Go tests           | https://go.dev/dl/                   |
| Node 18+ | WebUI tests        | https://nodejs.org/                  |
| kubectl  | Cluster validation | https://kubernetes.io/docs/tasks/    |
| helm     | Chart validation   | https://helm.sh/docs/intro/install/  |
| curl     | API validation     | Pre-installed on most systems        |

---

## Verification Matrix

| # | Feature                    | Test Type    | Command                                       | SLA Target          |
|---|---------------------------|-------------|-----------------------------------------------|---------------------|
| 1 | 60s Fault Recovery         | Go unit     | `go test ./test/validation/ -run HASLA`        | Recovery < 60s      |
| 2 | Live Migration             | Go unit     | `go test ./test/validation/ -run Migration`    | Freeze < 5s         |
| 3 | vGPU Oversubscription      | Go unit     | `go test ./test/validation/ -run VGPU`         | Alloc < 1µs         |
| 4 | API Smoke Tests            | Go unit     | `go test ./test/validation/ -run Smoke`        | Response < 5s       |
| 5 | Billing Accuracy           | Go unit     | `go test ./test/validation/ -run Billing`      | Formula correct     |
| 6 | WebUI Components           | Vitest      | `cd webui && npx vitest run`                   | All pass            |
| 7 | Helm Chart                 | helm lint   | `helm lint deploy/helm/phoenixgpu`             | No errors           |
| 8 | Go Build                   | go build    | `go build ./...`                               | No errors           |
| 9 | E2E HA Recovery            | E2E (Linux) | `go test ./test/e2e/... -v -tags e2e`          | AC1-AC3 pass        |
| 10| Cluster Deployment         | kubectl     | `bash hack/validate-deployment.sh`             | All pods Running    |

---

## Detailed Verification Procedures

### 1. 60-Second Fault Recovery (Most Critical)

**SLA Target**: From node fault detection to training resumption < 60 seconds.

**Timing Budget** (configured in `deploy/helm/phoenixgpu/values.yaml`):

| Component                 | Time   | Config Key                              |
|--------------------------|--------|-----------------------------------------|
| Fault detection polling   | 10s    | `phoenixController.config.faultDetectorPollSeconds` |
| NotReady threshold        | 30s    | `phoenixController.config.notReadyThresholdSeconds` |
| Restore execution         | ~20s   | Depends on checkpoint size              |
| **Total (worst case)**    | **~60s** |                                       |

**Automated verification**:
```bash
go test ./test/validation/ -run TestHASLA -v
```

**Manual verification on live cluster**:
```bash
# 1. Deploy test job
kubectl apply -f deploy/manifests/examples/test-phoenixjob-ha.yaml

# 2. Wait for Running + checkpoint
kubectl get phoenixjobs -w
# Wait until: Phase=Running, Checkpoints >= 1

# 3. Record pre-fault state
FAULT_TIME=$(date +%s)
kubectl get phoenixjob ha-recovery-test -o jsonpath='{.status.checkpointCount}'

# 4. Simulate node fault (on the node running the job)
NODE=$(kubectl get phoenixjob ha-recovery-test -o jsonpath='{.status.currentNodeName}')
# Option A: kubectl cordon + drain (graceful)
kubectl cordon $NODE && kubectl drain $NODE --force --ignore-daemonsets
# Option B: SSH to node and stop kubelet (abrupt)
# ssh $NODE 'sudo systemctl stop kubelet'

# 5. Watch recovery
kubectl get phoenixjobs -w
# Expected: Phase transitions: Running -> Restoring -> Running

# 6. Verify SLA
RECOVERY_TIME=$(date +%s)
echo "Recovery time: $((RECOVERY_TIME - FAULT_TIME)) seconds"

# 7. Cleanup
kubectl delete -f deploy/manifests/examples/test-phoenixjob-ha.yaml
kubectl uncordon $NODE
```

**Acceptance Criteria**:
- AC1: Training step after restore >= step before checkpoint (no progress lost)
- AC2: Recovery time < 60 seconds
- AC3: Single checkpoint duration < 30 seconds

### 2. Live GPU Migration

**SLA Target**: Freeze window < 5 seconds for 80GB VRAM.

```bash
# Deploy migration test job
kubectl apply -f deploy/manifests/examples/test-phoenixjob-migration.yaml

# Wait for Running state
kubectl get phoenixjobs -w

# Trigger migration
curl -X POST http://<API_URL>/api/v1/jobs/default/migration-test/migrate \
  -H 'Content-Type: application/json' \
  -d '{"targetNode": "gpu-node-02"}'

# Monitor migration status
watch -n 1 'curl -s http://<API_URL>/api/v1/jobs/default/migration-test/migration-status'
# Expected stages: Pending -> PreDumping -> Dumping -> Transferring -> Restoring -> Done

# Cleanup
kubectl delete -f deploy/manifests/examples/test-phoenixjob-migration.yaml
```

### 3. vGPU Oversubscription

**SLA Target**: 2x density improvement with < 10% overhead.

```bash
# Run vGPU tests
go test ./test/validation/ -run TestVGPU -v
go test ./pkg/vgpu/... -v

# Run benchmarks
go test ./pkg/vgpu/... -bench=. -benchmem

# Deploy sharing test (2 tasks on same GPU)
kubectl apply -f deploy/manifests/examples/test-phoenixjob-vgpu-sharing.yaml

# Verify both tasks are running
kubectl get phoenixjobs -l test-pair=vgpu-share

# Check VRAM usage via API
curl http://<API_URL>/api/v1/nodes | python3 -m json.tool

# Cleanup
kubectl delete -f deploy/manifests/examples/test-phoenixjob-vgpu-sharing.yaml
```

### 4. Billing System

**SLA Target**: TFlops-h formula: `AllocRatio * FP16TFlops * DurationHours`

```bash
# Run billing accuracy tests
go test ./test/validation/ -run TestBilling -v

# Deploy billing test job
kubectl apply -f deploy/manifests/examples/test-phoenixjob-billing.yaml

# Wait 5 minutes for billing collection (default interval: 60s)
sleep 300

# Check billing by department
curl http://<API_URL>/api/v1/billing/departments?period=daily | python3 -m json.tool

# Check billing records
curl http://<API_URL>/api/v1/billing/records?department=qa-billing | python3 -m json.tool

# Cleanup
kubectl delete -f deploy/manifests/examples/test-phoenixjob-billing.yaml
```

### 5. Web Console (WebUI)

**Verification checklist for each page**:

| Page      | URL Path       | Verify                                                          |
|-----------|---------------|-----------------------------------------------------------------|
| Dashboard | `/`           | KPI cards, 24h utilization chart, cost donut, recent alerts     |
| Jobs      | `/jobs`       | Job list with phases, checkpoint bars, trigger button           |
| Billing   | `/billing`    | Period selector, department usage, quota status pills            |
| Nodes     | `/nodes`      | Node summary, per-node GPU model/VRAM/SM/power/temp             |
| Alerts    | `/alerts`     | Severity + state filters, resolve button, count chips           |

```bash
# Run WebUI tests
cd webui && npx vitest run --reporter=verbose

# Access WebUI (after deployment)
# Default: http://localhost:3000 (Kind) or via Ingress
```

### 6. API Endpoints

All 14 endpoints with expected responses:

| Method | Endpoint                                      | Expected Status |
|--------|-----------------------------------------------|-----------------|
| GET    | `/healthz`                                    | 200             |
| GET    | `/readyz`                                     | 200             |
| GET    | `/metrics`                                    | 200             |
| GET    | `/api/v1/cluster/summary`                     | 200             |
| GET    | `/api/v1/cluster/utilization-history?hours=24` | 200            |
| GET    | `/api/v1/nodes`                               | 200             |
| GET    | `/api/v1/jobs`                                | 200             |
| GET    | `/api/v1/jobs/{ns}/{name}`                    | 200 / 404       |
| POST   | `/api/v1/jobs/{ns}/{name}/checkpoint`         | 202             |
| POST   | `/api/v1/jobs/{ns}/{name}/migrate`            | 202             |
| GET    | `/api/v1/jobs/{ns}/{name}/migration-status`   | 200             |
| GET    | `/api/v1/billing/departments?period=monthly`  | 200             |
| GET    | `/api/v1/billing/records`                     | 200             |
| GET    | `/api/v1/alerts`                              | 200             |
| POST   | `/api/v1/alerts/{id}/resolve`                 | 200             |

```bash
# Run full API smoke test
go test ./test/validation/ -run TestSmoke -v

# Against live API
go test ./test/validation/ -v -api-url=http://localhost:8090
```

### 7. Monitoring

```bash
# Check component metrics endpoints
curl http://<controller>:8080/metrics   # Phoenix Controller
curl http://<scheduler>:8084/metrics    # Scheduler Extender
curl http://<device-plugin>:8082/metrics # Device Plugin
curl http://<billing>:8085/metrics      # Billing Engine
curl http://<api-server>:8091/metrics   # API Server

# Check DCGM Exporter
kubectl get pods -n phoenixgpu-system -l app=dcgm-exporter
```

---

## Troubleshooting

| Symptom                              | Likely Cause                          | Fix                                     |
|--------------------------------------|---------------------------------------|----------------------------------------|
| Pods in CrashLoopBackOff             | Missing GPU drivers or CRIU           | Install NVIDIA drivers, CRIU package   |
| PhoenixJob stuck in Pending          | No vGPU resources available           | Check device-plugin logs               |
| Checkpoint fails                     | Insufficient PVC storage              | Increase `snapshotStorage.pvc.size`    |
| Recovery > 60s                       | Large checkpoint or slow storage      | Enable preDump, use NVMe storage       |
| Billing shows 0 records              | Billing engine not collecting         | Check billing-engine pod logs          |
| WebUI shows "Loading..."             | API server unreachable                | Check network/service configuration    |
| Migration fails at Transferring      | SSH connectivity between nodes        | Check node-to-node SSH keys            |

---

## Cleanup Test Resources

```bash
# Remove all test PhoenixJobs
kubectl delete phoenixjobs -l phoenixgpu.io/test=true

# Or remove individual test jobs
kubectl delete -f deploy/manifests/examples/
```
