#!/usr/bin/env bash
# PhoenixGPU — 本地 Kind 开发环境一键启动
# 用法: bash hack/kind-setup.sh [--reset]
#
# 依赖: kind, kubectl, helm, docker
# Copyright 2025 PhoenixGPU Authors
set -euo pipefail

CLUSTER_NAME="phoenixgpu-dev"
NAMESPACE="phoenixgpu-system"
REGISTRY="ghcr.io/wlqtjl/phoenixgpu"
VERSION="dev"

BLUE='\033[0;34m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()    { echo -e "${BLUE}[INFO]${NC}  $*"; }
success() { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error()   { echo -e "${RED}[ERR]${NC}   $*"; exit 1; }
step()    { echo -e "\n${BLUE}━━━ $* ━━━${NC}"; }

# ── Flags ─────────────────────────────────────────────────────────
RESET=false
for arg in "$@"; do
  [ "$arg" = "--reset" ] && RESET=true
done

echo ""
echo "╔══════════════════════════════════════════════════════╗"
echo "║     PhoenixGPU — Local Kind Dev Environment          ║"
echo "╚══════════════════════════════════════════════════════╝"
echo ""

# ── Check prerequisites ────────────────────────────────────────────
step "Checking prerequisites"
for bin in kind kubectl helm docker; do
  if command -v "$bin" &>/dev/null; then
    success "$bin found: $(command -v $bin)"
  else
    error "$bin not found. Install it and retry."
  fi
done

# ── Optionally reset ──────────────────────────────────────────────
if $RESET && kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
  warn "Deleting existing cluster: $CLUSTER_NAME"
  kind delete cluster --name "$CLUSTER_NAME"
fi

# ── Create cluster ────────────────────────────────────────────────
step "Creating Kind cluster: $CLUSTER_NAME"
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
  warn "Cluster $CLUSTER_NAME already exists, reusing"
else
  kind create cluster --name "$CLUSTER_NAME" --config - <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 30090    # API server NodePort
        hostPort: 8090
        protocol: TCP
      - containerPort: 30080    # WebUI NodePort
        hostPort: 3000
        protocol: TCP
  - role: worker
    labels:
      node-role.kubernetes.io/gpu: "true"
  - role: worker
    labels:
      node-role.kubernetes.io/gpu: "true"
EOF
  success "Kind cluster created"
fi

# Set kubectl context
kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null
success "kubectl context: kind-$CLUSTER_NAME"

# ── Build images ──────────────────────────────────────────────────
step "Building Docker images"

COMPONENTS=(phoenix-controller device-plugin scheduler-extender billing-engine api-server)
for comp in "${COMPONENTS[@]}"; do
  dockerfile="build/Dockerfile.${comp}"
  if [ -f "$dockerfile" ]; then
    info "Building $comp..."
    docker build -t "${REGISTRY}/${comp}:${VERSION}" -f "$dockerfile" . --quiet
    kind load docker-image "${REGISTRY}/${comp}:${VERSION}" --name "$CLUSTER_NAME"
    success "$comp loaded into Kind"
  else
    warn "No Dockerfile for $comp (${dockerfile}) — skipping"
  fi
done

# ── Install CRDs ──────────────────────────────────────────────────
step "Installing PhoenixGPU CRDs"
kubectl apply -f deploy/manifests/crd/ --server-side
success "CRDs installed"

# ── Create namespace ──────────────────────────────────────────────
step "Setting up namespace"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
success "Namespace: $NAMESPACE"

# ── Helm install ──────────────────────────────────────────────────
step "Installing PhoenixGPU via Helm"
helm upgrade --install phoenixgpu ./deploy/helm/phoenixgpu \
  --namespace "$NAMESPACE" \
  --set global.imageRegistry="$REGISTRY" \
  --set "phoenixController.image.tag=${VERSION}" \
  --set "webui.image.tag=${VERSION}" \
  --set "webui.service.type=NodePort" \
  --set "snapshotStorage.backend=pvc" \
  --set "snapshotStorage.pvc.size=10Gi" \
  --set "monitoring.dcgmExporter.enabled=false" \
  --wait --timeout=120s 2>&1 || {
    warn "Helm install failed (expected without GPU hardware)"
    warn "Deploying in mock/dev mode..."
    helm upgrade --install phoenixgpu ./deploy/helm/phoenixgpu \
      --namespace "$NAMESPACE" \
      --set global.imageRegistry="$REGISTRY" \
      --set "phoenixController.enabled=false" \
      --set "devicePlugin.enabled=false" \
      --set "billingEngine.enabled=false"
  }

# ── Wait for pods ─────────────────────────────────────────────────
step "Waiting for pods to be ready"
kubectl wait --for=condition=Ready pod \
  --selector="app.kubernetes.io/name=phoenixgpu" \
  --namespace="$NAMESPACE" \
  --timeout=120s 2>/dev/null || warn "Some pods not ready (OK for dev environment)"

# ── Show status ───────────────────────────────────────────────────
step "Cluster Status"
echo ""
kubectl get pods -n "$NAMESPACE" -o wide 2>/dev/null || true
echo ""

# ── Print access info ─────────────────────────────────────────────
echo ""
echo "╔══════════════════════════════════════════════════════╗"
success "PhoenixGPU dev environment ready!"
echo ""
echo "  WebUI:      http://localhost:3000"
echo "  API Server: http://localhost:8090/api/v1"
echo "  API Health: http://localhost:8090/healthz"
echo ""
echo "  kubectl get phoenixjobs -A"
echo "  kubectl logs -n $NAMESPACE -l app.kubernetes.io/component=phoenix-controller -f"
echo ""
echo "  To reset: bash hack/kind-setup.sh --reset"
echo "╚══════════════════════════════════════════════════════╝"
echo ""
