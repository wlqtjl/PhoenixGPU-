module github.com/wlqtjl/PhoenixGPU

go 1.22

require (
	k8s.io/api v0.29.3
	k8s.io/apimachinery v0.29.3
	k8s.io/client-go v0.29.3
	sigs.k8s.io/controller-runtime v0.17.2

	// GPU monitoring
	github.com/NVIDIA/go-nvml v0.12.0-1

	// Kubernetes device plugin
	k8s.io/kubelet v0.29.3
	google.golang.org/grpc v1.62.1

	// Observability
	github.com/prometheus/client_golang v1.19.0
	go.uber.org/zap v1.27.0

	// Storage
	github.com/lib/pq v1.10.9

	// HTTP server
	github.com/gin-gonic/gin v1.9.1

	// Utilities
	github.com/spf13/cobra v1.8.0
	github.com/spf13/viper v1.18.2
)

require (
	// indirect dependencies managed by go mod tidy
	github.com/go-logr/logr v1.4.1 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
)
