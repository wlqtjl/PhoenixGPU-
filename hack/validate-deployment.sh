#!/usr/bin/env bash
# PhoenixGPU — Deployment Validation Script
#
# Validates all 10 core features of a running PhoenixGPU deployment.
# Can run against a live cluster or in mock mode (no cluster required).
#
# Usage:
#   bash hack/validate-deployment.sh                    # Full validation (requires running cluster)
#   bash hack/validate-deployment.sh --mock             # Mock mode (unit tests only, no cluster needed)
#   bash hack/validate-deployment.sh --api-url http://host:8090  # Validate specific API server
#
# Copyright 2025 PhoenixGPU Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# ── Colors and formatting ────────────────────────────────────────
BLUE='\033[0;34m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
RED='\033[0;31m'; BOLD='\033[1m'; NC='\033[0m'

info()    { echo -e "${BLUE}[INFO]${NC}  $*"; }
pass()    { echo -e "${GREEN}[PASS]${NC}  $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC}  $*"; }
fail()    { echo -e "${RED}[FAIL]${NC}  $*"; }
header()  { echo -e "\n${BOLD}${BLUE}━━━ $* ━━━${NC}"; }

# ── Parse arguments ──────────────────────────────────────────────
MOCK_MODE=false
API_URL=""
NAMESPACE="phoenixgpu-system"
SKIP_CLUSTER=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mock)        MOCK_MODE=true; SKIP_CLUSTER=true ;;
    --api-url)     shift; API_URL="${1:-}" ;;
    --namespace)   shift; NAMESPACE="${1:-}" ;;
    --skip-cluster) SKIP_CLUSTER=true ;;
    -h|--help)
      echo "Usage: $0 [--mock] [--api-url URL] [--namespace NS] [--skip-cluster]"
      exit 0
      ;;
    *) echo "Unknown arg: $1"; exit 2 ;;
  esac
  shift
done

PASSED=0
FAILED=0
SKIPPED=0

check_pass() { PASSED=$((PASSED + 1)); pass "$1"; }
check_fail() { FAILED=$((FAILED + 1)); fail "$1"; }
check_skip() { SKIPPED=$((SKIPPED + 1)); warn "SKIP: $1"; }

echo ""
echo "╔══════════════════════════════════════════════════════╗"
echo "║     PhoenixGPU — Deployment Validation Suite         ║"
echo "╚══════════════════════════════════════════════════════╝"
echo ""
info "Mode: $(if $MOCK_MODE; then echo 'MOCK (no cluster required)'; else echo 'LIVE'; fi)"
info "Root: $ROOT_DIR"
echo ""

# ══════════════════════════════════════════════════════════════════
# Phase 1: Go Unit Test Validation (always runs)
# ══════════════════════════════════════════════════════════════════

header "Phase 1: Go Validation Tests"

info "Running API smoke tests..."
if go test ./test/validation/... -v -count=1 -timeout=5m 2>&1 | tee /tmp/phoenixgpu-validation.log; then
  check_pass "API smoke tests"
else
  check_fail "API smoke tests (see /tmp/phoenixgpu-validation.log)"
fi

info "Running existing API server tests..."
if go test ./cmd/api-server/... -count=1 -timeout=3m 2>&1; then
  check_pass "API server unit tests"
else
  check_fail "API server unit tests"
fi

info "Running vGPU oversubscription tests..."
if go test ./pkg/vgpu/... -v -count=1 -timeout=2m 2>&1; then
  check_pass "vGPU oversubscription tests"
else
  check_fail "vGPU oversubscription tests"
fi

# ══════════════════════════════════════════════════════════════════
# Phase 2: WebUI Tests
# ══════════════════════════════════════════════════════════════════

header "Phase 2: WebUI Tests"

if [ -f "webui/package.json" ]; then
  if command -v npx &>/dev/null; then
    info "Running WebUI component tests..."
    if (cd webui && npx vitest run --reporter=verbose 2>&1); then
      check_pass "WebUI component tests"
    else
      check_fail "WebUI component tests"
    fi
  else
    check_skip "WebUI tests (npx not available)"
  fi
else
  check_skip "WebUI tests (webui/package.json not found)"
fi

# ══════════════════════════════════════════════════════════════════
# Phase 3: Helm Chart Validation
# ══════════════════════════════════════════════════════════════════

header "Phase 3: Helm Chart Validation"

if command -v helm &>/dev/null; then
  info "Linting Helm chart..."
  if helm lint ./deploy/helm/phoenixgpu 2>&1; then
    check_pass "Helm chart lint"
  else
    check_fail "Helm chart lint"
  fi

  info "Validating CRD..."
  if [ -f deploy/manifests/crd/phoenixjobs.yaml ]; then
    check_pass "PhoenixJob CRD exists"
  else
    check_fail "PhoenixJob CRD missing"
  fi
else
  check_skip "Helm validation (helm not installed)"
fi

# ══════════════════════════════════════════════════════════════════
# Phase 4: Live Cluster Validation (skipped in mock mode)
# ══════════════════════════════════════════════════════════════════

if ! $SKIP_CLUSTER; then
  header "Phase 4: Live Cluster Validation"

  if ! command -v kubectl &>/dev/null; then
    check_skip "Cluster validation (kubectl not installed)"
  else
    # 4.1 Check cluster connectivity
    info "Checking cluster connectivity..."
    if kubectl cluster-info &>/dev/null; then
      check_pass "Cluster connectivity"
    else
      check_fail "Cluster connectivity"
      warn "Remaining cluster checks will be skipped"
      SKIP_CLUSTER=true
    fi
  fi
fi

if ! $SKIP_CLUSTER && command -v kubectl &>/dev/null; then
  # 4.2 Check PhoenixGPU pods
  info "Checking PhoenixGPU pods in $NAMESPACE..."
  POD_COUNT=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -c Running || echo 0)
  if [ "$POD_COUNT" -gt 0 ]; then
    check_pass "PhoenixGPU pods running ($POD_COUNT pods)"
    kubectl get pods -n "$NAMESPACE" -o wide 2>/dev/null || true
  else
    check_fail "No running PhoenixGPU pods in $NAMESPACE"
  fi

  # 4.3 Check CRD registration
  info "Checking PhoenixJob CRD registration..."
  if kubectl get crd phoenixjobs.phoenixgpu.io &>/dev/null; then
    check_pass "PhoenixJob CRD registered"
  else
    check_fail "PhoenixJob CRD not registered"
  fi

  # 4.4 Check GPU nodes
  info "Checking GPU nodes..."
  GPU_NODES=$(kubectl get nodes -l node-role.kubernetes.io/gpu=true --no-headers 2>/dev/null | wc -l || echo 0)
  if [ "$GPU_NODES" -gt 0 ]; then
    check_pass "GPU nodes found ($GPU_NODES nodes)"
  else
    warn "No GPU-labeled nodes found (label: node-role.kubernetes.io/gpu=true)"
    SKIPPED=$((SKIPPED + 1))
  fi

  # 4.5 Check vGPU resources
  info "Checking vGPU device registration..."
  VGPU_NODES=$(kubectl get nodes -o json 2>/dev/null | \
    python3 -c "import json,sys; d=json.load(sys.stdin); print(sum(1 for n in d['items'] if 'nvidia.com/vgpu' in n.get('status',{}).get('allocatable',{})))" 2>/dev/null || echo 0)
  if [ "$VGPU_NODES" -gt 0 ]; then
    check_pass "vGPU resources registered ($VGPU_NODES nodes)"
  else
    check_skip "vGPU resources not registered (device-plugin may not be running)"
  fi
fi

# ══════════════════════════════════════════════════════════════════
# Phase 5: API Server Validation (if API URL provided or discoverable)
# ══════════════════════════════════════════════════════════════════

if [ -n "$API_URL" ] || (! $SKIP_CLUSTER && command -v kubectl &>/dev/null); then
  header "Phase 5: Live API Validation"

  # Discover API URL if not provided
  if [ -z "$API_URL" ]; then
    API_URL=$(kubectl get svc -n "$NAMESPACE" -l app.kubernetes.io/component=api-server \
      -o jsonpath='{.items[0].spec.clusterIP}' 2>/dev/null || echo "")
    if [ -n "$API_URL" ]; then
      API_URL="http://${API_URL}:8090"
      info "Discovered API server: $API_URL"
    else
      check_skip "API server not discoverable via kubectl"
    fi
  fi

  if [ -n "$API_URL" ]; then
    # 5.1 Health check
    info "Checking API health..."
    if curl -sf "${API_URL}/healthz" &>/dev/null; then
      check_pass "API /healthz"
    else
      check_fail "API /healthz unreachable"
    fi

    # 5.2 Cluster summary
    info "Checking cluster summary..."
    SUMMARY=$(curl -sf "${API_URL}/api/v1/cluster/summary" 2>/dev/null || echo "")
    if [ -n "$SUMMARY" ] && echo "$SUMMARY" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'data' in d" 2>/dev/null; then
      check_pass "API /cluster/summary returns data"
      echo "$SUMMARY" | python3 -m json.tool 2>/dev/null | head -20 || true
    else
      check_fail "API /cluster/summary failed"
    fi

    # 5.3 Nodes endpoint
    info "Checking nodes..."
    if curl -sf "${API_URL}/api/v1/nodes" &>/dev/null; then
      check_pass "API /nodes"
    else
      check_fail "API /nodes"
    fi

    # 5.4 Jobs endpoint
    info "Checking jobs..."
    if curl -sf "${API_URL}/api/v1/jobs" &>/dev/null; then
      check_pass "API /jobs"
    else
      check_fail "API /jobs"
    fi

    # 5.5 Billing endpoint
    info "Checking billing..."
    if curl -sf "${API_URL}/api/v1/billing/departments?period=monthly" &>/dev/null; then
      check_pass "API /billing/departments"
    else
      check_fail "API /billing/departments"
    fi

    # 5.6 Alerts endpoint
    info "Checking alerts..."
    if curl -sf "${API_URL}/api/v1/alerts" &>/dev/null; then
      check_pass "API /alerts"
    else
      check_fail "API /alerts"
    fi

    # 5.7 Metrics endpoint
    info "Checking metrics..."
    if curl -sf "${API_URL}/metrics" &>/dev/null; then
      check_pass "API /metrics"
    else
      check_fail "API /metrics"
    fi
  fi
fi

# ══════════════════════════════════════════════════════════════════
# Phase 6: Build Verification
# ══════════════════════════════════════════════════════════════════

header "Phase 6: Build Verification"

info "Verifying Go build..."
if go build ./... 2>&1; then
  check_pass "Go build (all packages)"
else
  check_fail "Go build"
fi

# ══════════════════════════════════════════════════════════════════
# Summary
# ══════════════════════════════════════════════════════════════════

echo ""
echo "╔══════════════════════════════════════════════════════╗"
echo "║     PhoenixGPU Validation Report                     ║"
echo "╠══════════════════════════════════════════════════════╣"
printf "║  ${GREEN}Passed:  %-4d${NC}                                      ║\n" "$PASSED"
printf "║  ${RED}Failed:  %-4d${NC}                                      ║\n" "$FAILED"
printf "║  ${YELLOW}Skipped: %-4d${NC}                                      ║\n" "$SKIPPED"
echo "╠══════════════════════════════════════════════════════╣"

if [ "$FAILED" -eq 0 ]; then
  echo -e "║  ${GREEN}${BOLD}RESULT: ALL CHECKS PASSED${NC}                          ║"
else
  echo -e "║  ${RED}${BOLD}RESULT: $FAILED CHECK(S) FAILED${NC}                         ║"
fi
echo "╚══════════════════════════════════════════════════════╝"
echo ""

exit "$FAILED"
