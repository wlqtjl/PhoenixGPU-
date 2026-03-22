## PhoenixGPU Makefile
## Usage: make <target>

BINARY_DIR   := bin
REGISTRY     := ghcr.io/wlqtjl/phoenixgpu
VERSION      := $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0-dev")
COMMIT       := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE         := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS      := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
GO           := go
GOFLAGS      := -trimpath
CGO_ENABLED  := 0

COMPONENTS := phoenix-controller device-plugin scheduler-extender billing-engine webhook

.PHONY: all build test lint fmt vet clean docker-build helm-lint kind-up kind-down help \
	quality-api-server quality-api-server-race quality-api-server-bench

## ── Default ─────────────────────────────────────────────────────
all: lint test build

## ── Build ───────────────────────────────────────────────────────
build: $(addprefix build-, $(COMPONENTS))

build-%:
	@echo "→ Building $*..."
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) \
		-ldflags "$(LDFLAGS)" \
		-o $(BINARY_DIR)/$* \
		./cmd/$*/...
	@echo "  ✓ $(BINARY_DIR)/$*"

## libvgpu (C/C++ CUDA interception layer)
build-libvgpu:
	@echo "→ Building libvgpu.so..."
	@mkdir -p libvgpu/build
	cd libvgpu && cmake -B build -DCMAKE_BUILD_TYPE=Release . && cmake --build build -j$(shell nproc)
	@echo "  ✓ libvgpu/build/libvgpu.so"

## ── Test ────────────────────────────────────────────────────────
test:
	@echo "→ Running unit tests..."
	$(GO) test ./... -race -coverprofile=coverage.out -covermode=atomic -timeout=5m
	@echo "  ✓ Tests passed"
	@$(GO) tool cover -func=coverage.out | tail -1

test-short:
	$(GO) test ./... -short -count=1

test-verbose:
	$(GO) test ./... -v -race -timeout=5m

quality-api-server:
	@echo "→ API Server quality baseline (L1/L2)..."
	$(GO) test ./cmd/api-server -count=1 -coverprofile=coverage-api-server.out
	@$(GO) tool cover -func=coverage-api-server.out | tail -1

quality-api-server-race:
	@echo "→ API Server race check (L3)..."
	$(GO) test -race ./cmd/api-server -count=1

quality-api-server-bench:
	@echo "→ API Server benchmark smoke (L4)..."
	$(GO) test ./cmd/api-server -run '^$$' -bench . -benchmem -count=1

test-e2e:
	@echo "→ Running E2E tests (requires running cluster)..."
	$(GO) test ./test/e2e/... -v -timeout=30m

## ── Code quality ────────────────────────────────────────────────
lint:
	@echo "→ Running linter..."
	golangci-lint run --timeout=5m
	@echo "  ✓ Lint passed"

fmt:
	$(GO) fmt ./...
	goimports -w .

vet:
	$(GO) vet ./...

## ── Generate ─────────────────────────────────────────────────────
generate:
	@echo "→ Running code generators..."
	controller-gen rbac:roleName=phoenixgpu-role \
		crd:trivialVersions=true \
		webhook \
		paths="./..." \
		output:crd:artifacts:config=deploy/manifests/crd
	@echo "  ✓ CRDs generated in deploy/manifests/crd/"

## ── Docker ──────────────────────────────────────────────────────
docker-build:
	@for c in $(COMPONENTS); do \
		echo "→ Building image: $$c"; \
		docker build -t $(REGISTRY)/$$c:$(VERSION) \
			-f build/Dockerfile.$$c .; \
	done

docker-push:
	@for c in $(COMPONENTS); do \
		docker push $(REGISTRY)/$$c:$(VERSION); \
	done

## ── Helm ────────────────────────────────────────────────────────
helm-lint:
	helm lint ./deploy/helm/phoenixgpu

helm-install:
	helm upgrade --install phoenixgpu ./deploy/helm/phoenixgpu \
		--namespace phoenixgpu-system \
		--create-namespace \
		--wait

helm-uninstall:
	helm uninstall phoenixgpu -n phoenixgpu-system

## ── Local dev cluster (KinD) ─────────────────────────────────────
kind-up:
	@echo "→ Creating KinD cluster..."
	kind create cluster --name phoenixgpu-dev --config hack/kind-config.yaml
	kubectl apply -f https://raw.githubusercontent.com/NVIDIA/k8s-device-plugin/v0.14.5/nvidia-device-plugin.yml || true
	@echo "  ✓ Cluster ready: kubectl cluster-info --context kind-phoenixgpu-dev"

kind-down:
	kind delete cluster --name phoenixgpu-dev

kind-load:
	@for c in $(COMPONENTS); do \
		kind load docker-image $(REGISTRY)/$$c:$(VERSION) --name phoenixgpu-dev; \
	done

## ── Utilities ────────────────────────────────────────────────────
clean:
	rm -rf $(BINARY_DIR) coverage.out libvgpu/build

deps:
	$(GO) mod download
	$(GO) mod tidy

## ── Help ─────────────────────────────────────────────────────────
help:
	@echo ""
	@echo "PhoenixGPU Build System"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  make build          Build all Go binaries"
	@echo "  make build-libvgpu  Build CUDA interception library"
	@echo "  make test           Run all unit tests with race detector"
	@echo "  make lint           Run golangci-lint"
	@echo "  make fmt            Format code"
	@echo "  make generate       Regenerate CRD manifests & RBAC"
	@echo "  make docker-build   Build all Docker images"
	@echo "  make helm-install   Deploy to current kubectl context"
	@echo "  make kind-up        Create local KinD dev cluster"
	@echo "  make clean          Remove build artifacts"
	@echo ""
