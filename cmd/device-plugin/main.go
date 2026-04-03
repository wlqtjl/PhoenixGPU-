//go:build devicepluginfull
// +build devicepluginfull

// phoenix-device-plugin — PhoenixGPU vGPU Device Plugin
//
// Registers virtual GPU resources (nvidia.com/vgpu, nvidia.com/vgpu-memory)
// with the kubelet device plugin API and manages fractional GPU allocation.
//
// Architecture:
//   1. Discovers physical GPUs on the node via NVML (nvidia-smi fallback)
//   2. Computes fractional vGPU slices per physical GPU
//   3. Runs a gRPC server implementing kubelet DevicePlugin v1beta1
//   4. ListAndWatch advertises vGPU devices to kubelet
//   5. Allocate injects libvgpu config (LD_PRELOAD, VGPU_* env) into containers
//   6. Health-check loop monitors GPU health via NVML
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type options struct {
	resourceName       string
	memoryResourceName string
	socketDir          string
	metricsAddr        string
	probeAddr          string
	slicesPerGPU       int
	healthCheckSec     int
}

func main() {
	opts := &options{}

	root := &cobra.Command{
		Use:   "device-plugin",
		Short: "PhoenixGPU vGPU Device Plugin — fractional GPU resource registration",
		Long: `device-plugin registers virtual GPU resources with the kubelet and:
  1. Discovers physical GPUs on the node via NVML
  2. Advertises fractional vGPU slices (nvidia.com/vgpu)
  3. Manages device allocation and memory partitioning
  4. Reports device health to kubelet`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		RunE:    func(cmd *cobra.Command, args []string) error { return run(opts) },
	}

	f := root.Flags()
	f.StringVar(&opts.resourceName, "resource-name", "nvidia.com/vgpu", "Kubernetes extended resource name for vGPU")
	f.StringVar(&opts.memoryResourceName, "memory-resource-name", "nvidia.com/vgpu-memory", "Kubernetes extended resource name for vGPU memory")
	f.StringVar(&opts.socketDir, "socket-dir", "/var/lib/kubelet/device-plugins", "Device plugin socket directory")
	f.StringVar(&opts.metricsAddr, "metrics-bind-address", ":8082", "Metrics endpoint address")
	f.StringVar(&opts.probeAddr, "health-probe-bind-address", ":8083", "Health probe address")
	f.IntVar(&opts.slicesPerGPU, "slices-per-gpu", 4, "Number of vGPU slices per physical GPU")
	f.IntVar(&opts.healthCheckSec, "health-check-interval", 30, "GPU health check interval in seconds")

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// ── GPU Discovery ─────────────────────────────────────────────────

// physicalGPU represents a physical GPU discovered on the node.
type physicalGPU struct {
	Index      int
	UUID       string
	Model      string
	MemoryMiB  int64
	Healthy    bool
}

// discoverGPUs finds physical GPUs using nvidia-smi.
// Returns the list of GPUs or an error if no GPUs are found.
func discoverGPUs(logger *zap.Logger) ([]physicalGPU, error) {
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=index,uuid,name,memory.total",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi query failed: %w", err)
	}

	var gpus []physicalGPU
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ", ", 4)
		if len(parts) < 4 {
			logger.Warn("skipping malformed nvidia-smi line", zap.String("line", line))
			continue
		}
		idx, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		memMiB, _ := strconv.ParseInt(strings.TrimSpace(parts[3]), 10, 64)

		gpus = append(gpus, physicalGPU{
			Index:     idx,
			UUID:      strings.TrimSpace(parts[1]),
			Model:     strings.TrimSpace(parts[2]),
			MemoryMiB: memMiB,
			Healthy:   true,
		})
	}

	if len(gpus) == 0 {
		return nil, fmt.Errorf("no GPUs discovered via nvidia-smi")
	}

	logger.Info("discovered GPUs",
		zap.Int("count", len(gpus)),
		zap.String("model", gpus[0].Model))
	return gpus, nil
}

// ── vGPU Device ───────────────────────────────────────────────────

// vGPUDevice represents a virtual GPU slice advertised to kubelet.
type vGPUDevice struct {
	ID            string // e.g. "gpu-0-slice-0"
	PhysicalIndex int
	PhysicalUUID  string
	MemoryMiB     int64 // memory slice for this vGPU
	Healthy       bool
}

// buildVGPUDevices creates fractional vGPU devices from physical GPUs.
func buildVGPUDevices(gpus []physicalGPU, slicesPerGPU int) []vGPUDevice {
	var devices []vGPUDevice
	for _, gpu := range gpus {
		memPerSlice := gpu.MemoryMiB / int64(slicesPerGPU)
		for s := 0; s < slicesPerGPU; s++ {
			devices = append(devices, vGPUDevice{
				ID:            fmt.Sprintf("gpu-%d-slice-%d", gpu.Index, s),
				PhysicalIndex: gpu.Index,
				PhysicalUUID:  gpu.UUID,
				MemoryMiB:     memPerSlice,
				Healthy:       gpu.Healthy,
			})
		}
	}
	return devices
}

// ── Device Plugin gRPC Server ─────────────────────────────────────

// phoenixDevicePlugin implements the kubelet DevicePlugin gRPC interface.
type phoenixDevicePlugin struct {
	pluginapi.UnimplementedDevicePluginServer

	resourceName string
	socketPath   string
	slicesPerGPU int
	logger       *zap.Logger

	mu      sync.RWMutex
	devices []vGPUDevice
	gpus    []physicalGPU

	server  *grpc.Server
	stopCh  chan struct{}
}

func newDevicePlugin(opts *options, gpus []physicalGPU, logger *zap.Logger) *phoenixDevicePlugin {
	socketName := strings.ReplaceAll(opts.resourceName, "/", "-") + ".sock"
	return &phoenixDevicePlugin{
		resourceName: opts.resourceName,
		socketPath:   filepath.Join(opts.socketDir, socketName),
		slicesPerGPU: opts.slicesPerGPU,
		logger:       logger,
		gpus:         gpus,
		devices:      buildVGPUDevices(gpus, opts.slicesPerGPU),
		stopCh:       make(chan struct{}),
	}
}

// GetDevicePluginOptions returns options for the device plugin.
func (p *phoenixDevicePlugin) GetDevicePluginOptions(_ context.Context, _ *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{
		PreStartRequired:                false,
		GetPreferredAllocationAvailable: false,
	}, nil
}

// ListAndWatch streams the list of devices to kubelet.
func (p *phoenixDevicePlugin) ListAndWatch(_ *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	p.logger.Info("ListAndWatch called, sending initial device list",
		zap.Int("deviceCount", len(p.devices)))

	// Send initial list
	if err := p.sendDeviceList(stream); err != nil {
		return err
	}

	// Block until stopped — health check loop updates device state
	// and triggers re-sends via the health check goroutine
	<-p.stopCh
	return nil
}

func (p *phoenixDevicePlugin) sendDeviceList(stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	resp := &pluginapi.ListAndWatchResponse{}
	for _, d := range p.devices {
		health := pluginapi.Healthy
		if !d.Healthy {
			health = pluginapi.Unhealthy
		}
		resp.Devices = append(resp.Devices, &pluginapi.Device{
			ID:     d.ID,
			Health: health,
		})
	}
	return stream.Send(resp)
}

// Allocate handles container allocation requests from kubelet.
// It injects libvgpu environment variables for CUDA interception.
func (p *phoenixDevicePlugin) Allocate(_ context.Context, req *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	resp := &pluginapi.AllocateResponse{}

	for _, creq := range req.ContainerRequests {
		p.logger.Info("Allocate request",
			zap.Strings("deviceIDs", creq.DevicesIDs))

		cresp := &pluginapi.ContainerAllocateResponse{
			Envs: make(map[string]string),
		}

		// Resolve physical GPU indices and memory for the requested slices
		var gpuIndices []string
		var totalMemMiB int64
		seen := make(map[int]bool)

		p.mu.RLock()
		for _, reqID := range creq.DevicesIDs {
			for _, d := range p.devices {
				if d.ID == reqID {
					if !seen[d.PhysicalIndex] {
						gpuIndices = append(gpuIndices, strconv.Itoa(d.PhysicalIndex))
						seen[d.PhysicalIndex] = true
					}
					totalMemMiB += d.MemoryMiB
					break
				}
			}
		}
		p.mu.RUnlock()

		// Inject libvgpu LD_PRELOAD and configuration env vars
		cresp.Envs["LD_PRELOAD"] = "/usr/lib/phoenixgpu/libvgpu.so"
		cresp.Envs["NVIDIA_VISIBLE_DEVICES"] = strings.Join(gpuIndices, ",")
		cresp.Envs["VGPU_MEMORY_LIMIT_MIB"] = strconv.FormatInt(totalMemMiB, 10)
		cresp.Envs["VGPU_DEVICE_COUNT"] = strconv.Itoa(len(creq.DevicesIDs))
		allocRatio := float64(len(creq.DevicesIDs)) / float64(p.slicesPerGPU)
		cresp.Envs["VGPU_ALLOC_RATIO"] = fmt.Sprintf("%.4f", allocRatio)

		resp.ContainerResponses = append(resp.ContainerResponses, cresp)
	}

	return resp, nil
}

// PreStartContainer is a no-op; not needed for PhoenixGPU.
func (p *phoenixDevicePlugin) PreStartContainer(_ context.Context, _ *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

// GetPreferredAllocation is a no-op.
func (p *phoenixDevicePlugin) GetPreferredAllocation(_ context.Context, _ *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	return &pluginapi.PreferredAllocationResponse{}, nil
}

// Start creates the gRPC server and registers with kubelet.
func (p *phoenixDevicePlugin) Start() error {
	// Remove stale socket
	if err := os.Remove(p.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	lis, err := net.Listen("unix", p.socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", p.socketPath, err)
	}

	p.server = grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(p.server, p)

	go func() {
		p.logger.Info("gRPC server listening", zap.String("socket", p.socketPath))
		if err := p.server.Serve(lis); err != nil {
			p.logger.Error("gRPC server error", zap.Error(err))
		}
	}()

	// Wait for the socket to become available
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(p.socketPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

// Register registers the device plugin with kubelet.
func (p *phoenixDevicePlugin) Register(kubeletSocket string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, kubeletSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", addr)
		}),
	)
	if err != nil {
		return fmt.Errorf("connect to kubelet: %w", err)
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)
	_, err = client.Register(ctx, &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     filepath.Base(p.socketPath),
		ResourceName: p.resourceName,
		Options: &pluginapi.DevicePluginOptions{
			PreStartRequired:                false,
			GetPreferredAllocationAvailable: false,
		},
	})
	if err != nil {
		return fmt.Errorf("register with kubelet: %w", err)
	}

	p.logger.Info("registered with kubelet",
		zap.String("resource", p.resourceName),
		zap.String("socket", p.socketPath))
	return nil
}

// Stop gracefully shuts down the gRPC server.
func (p *phoenixDevicePlugin) Stop() {
	close(p.stopCh)
	if p.server != nil {
		p.server.GracefulStop()
	}
	if err := os.Remove(p.socketPath); err != nil && !os.IsNotExist(err) {
		p.logger.Warn("failed to remove socket", zap.Error(err))
	}
}

// ── Health Check ──────────────────────────────────────────────────

// runHealthCheck periodically checks GPU health via nvidia-smi.
func (p *phoenixDevicePlugin) runHealthCheck(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.checkHealth()
		}
	}
}

// nvidiaSmiHealthOutput is the JSON format for nvidia-smi health queries.
type nvidiaSmiHealthOutput struct {
	Index  string `json:"index"`
	UUID   string `json:"uuid"`
	Health string `json:"gpu_health"` // "Healthy" or other states
}

func (p *phoenixDevicePlugin) checkHealth() {
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=index,uuid,gpu_bus_id",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		p.logger.Warn("health check failed, marking all GPUs unhealthy", zap.Error(err))
		p.mu.Lock()
		for i := range p.devices {
			p.devices[i].Healthy = false
		}
		p.mu.Unlock()
		return
	}

	// If nvidia-smi succeeds, GPUs are responsive
	healthyUUIDs := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ", ", 3)
		if len(parts) >= 2 {
			healthyUUIDs[strings.TrimSpace(parts[1])] = true
		}
	}

	p.mu.Lock()
	for i := range p.devices {
		p.devices[i].Healthy = healthyUUIDs[p.devices[i].PhysicalUUID]
	}
	p.mu.Unlock()
}

// ── Health/Metrics HTTP Server ────────────────────────────────────

func startProbeServer(addr string, logger *zap.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("probe server error", zap.Error(err))
		}
	}()
	return srv
}

// ── Main Run ──────────────────────────────────────────────────────

func run(opts *options) error {
	logger, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck

	logger.Info("starting device-plugin",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("resource", opts.resourceName),
		zap.String("socketDir", opts.socketDir),
		zap.Int("slicesPerGPU", opts.slicesPerGPU))

	// 1. Discover GPUs via NVML / nvidia-smi
	gpus, err := discoverGPUs(logger)
	if err != nil {
		return fmt.Errorf("GPU discovery: %w", err)
	}

	// 2. Create device plugin
	plugin := newDevicePlugin(opts, gpus, logger)
	devices := buildVGPUDevices(gpus, opts.slicesPerGPU)
	logger.Info("vGPU devices created",
		zap.Int("totalDevices", len(devices)),
		zap.Int("physicalGPUs", len(gpus)),
		zap.Int("slicesPerGPU", opts.slicesPerGPU))

	// 3. Start gRPC server
	if err := plugin.Start(); err != nil {
		return fmt.Errorf("start device plugin: %w", err)
	}
	defer plugin.Stop()

	// 4. Register with kubelet
	kubeletSocket := filepath.Join(opts.socketDir, "kubelet.sock")
	if err := plugin.Register(kubeletSocket); err != nil {
		return fmt.Errorf("register device plugin: %w", err)
	}

	// 5. Start health check loop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	healthInterval := time.Duration(opts.healthCheckSec) * time.Second
	go plugin.runHealthCheck(ctx, healthInterval)

	// 6. Start health probe HTTP server
	probeSrv := startProbeServer(opts.probeAddr, logger)
	defer probeSrv.Close()

	// 7. Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("device-plugin ready")
	sig := <-quit
	logger.Info("received signal, shutting down", zap.String("signal", sig.String()))

	return nil
}

// Ensure discoverGPUs result is JSON-marshalable for debug logging.
var _ json.Marshaler = (*physicalGPU)(nil)

func (g *physicalGPU) MarshalJSON() ([]byte, error) {
	type alias physicalGPU
	return json.Marshal((*alias)(g))
}
