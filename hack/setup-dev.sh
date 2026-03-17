#!/usr/bin/env bash
# PhoenixGPU Development Environment Setup
# Usage: bash hack/setup-dev.sh
set -euo pipefail

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

info()    { echo -e "${BLUE}[INFO]${NC} $*"; }
success() { echo -e "${GREEN}[OK]${NC}   $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
error()   { echo -e "${RED}[ERR]${NC}  $*"; exit 1; }

echo ""
echo "╔══════════════════════════════════════════╗"
echo "║     PhoenixGPU Dev Environment Setup     ║"
echo "╚══════════════════════════════════════════╝"
echo ""

# ── 1. Check Go ───────────────────────────────────────────────────
info "Checking Go installation..."
if ! command -v go &>/dev/null; then
  error "Go not found. Install from https://golang.org/dl/ (>=1.22 required)"
fi
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
info "Go version: $GO_VERSION"
success "Go OK"

# ── 2. Check kubectl ──────────────────────────────────────────────
info "Checking kubectl..."
if command -v kubectl &>/dev/null; then
  success "kubectl found: $(kubectl version --client --short 2>/dev/null || echo 'version unknown')"
else
  warn "kubectl not found — install for K8s integration tests"
fi

# ── 3. Check CRIU ─────────────────────────────────────────────────
info "Checking CRIU..."
if command -v criu &>/dev/null; then
  CRIU_VER=$(criu --version 2>/dev/null | head -1)
  success "CRIU found: $CRIU_VER"
  if criu check &>/dev/null; then
    success "CRIU kernel check passed"
  else
    warn "CRIU kernel check failed — checkpoint features will not work"
    warn "This is expected in containers/VMs without nested virt"
  fi
else
  warn "CRIU not found — install with: sudo apt-get install criu"
  warn "Checkpoint/Restore tests will be skipped"
fi

# ── 4. Check cuda-checkpoint ──────────────────────────────────────
info "Checking cuda-checkpoint plugin..."
if command -v cuda-checkpoint &>/dev/null; then
  success "cuda-checkpoint found"
else
  warn "cuda-checkpoint not found — GPU context checkpoint will be limited"
  warn "See: https://github.com/NVIDIA/cuda-checkpoint"
fi

# ── 5. Install Go tools ───────────────────────────────────────────
info "Installing Go dev tools..."
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.57.2 && \
  success "golangci-lint installed" || warn "golangci-lint install failed"

go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0 && \
  success "controller-gen installed" || warn "controller-gen install failed"

# ── 6. Download Go dependencies ───────────────────────────────────
info "Downloading Go dependencies..."
go mod download
success "Go modules downloaded"

# ── 7. Run tests ──────────────────────────────────────────────────
info "Running unit tests..."
if go test ./... -short -count=1 2>&1 | tail -5; then
  success "Unit tests passed"
else
  warn "Some tests failed or were skipped (expected without GPU/CRIU)"
fi

echo ""
echo "════════════════════════════════════════════"
success "Setup complete! You're ready to contribute to PhoenixGPU."
echo ""
echo "  Next steps:"
echo "    make test         # run all tests"
echo "    make build        # build all binaries"
echo "    make lint         # run linter"
echo "    make kind-up      # start local Kind cluster"
echo ""
