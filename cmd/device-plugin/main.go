//go:build devicepluginfull
// +build devicepluginfull

// phoenix-device-plugin — PhoenixGPU vGPU Device Plugin
//
// Registers virtual GPU resources (nvidia.com/vgpu, nvidia.com/vgpu-memory)
// with the kubelet device plugin API and manages fractional GPU allocation.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
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

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

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
		zap.String("socketDir", opts.socketDir))

	// TODO Sprint 7: implement kubelet device plugin gRPC server
	// 1. Discover GPUs via NVML
	// 2. Register with kubelet ListAndWatch
	// 3. Serve Allocate requests with libvgpu config injection
	// 4. Health-check loop

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("device-plugin ready, waiting for signal")
	select {
	case sig := <-quit:
		logger.Info("received signal, shutting down", zap.String("signal", sig.String()))
	case <-ctx.Done():
	}

	return nil
}
