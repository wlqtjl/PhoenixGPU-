//go:build schedulerfull
// +build schedulerfull

// phoenix-scheduler-extender — PhoenixGPU Scheduler Extender
//
// Extends the Kubernetes scheduler with GPU-aware bin-packing/spreading,
// NUMA affinity, and topology-aware placement.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type options struct {
	addr             string
	metricsAddr      string
	schedulingPolicy string
	numaAware        bool
}

func main() {
	opts := &options{}

	root := &cobra.Command{
		Use:   "scheduler-extender",
		Short: "PhoenixGPU Scheduler Extender — GPU-aware pod scheduling",
		Long: `scheduler-extender provides the Kubernetes scheduler with:
  1. GPU-aware filter and prioritize webhooks
  2. Bin-pack or spread scheduling policies
  3. NUMA topology awareness for GPU/CPU affinity
  4. vGPU allocation tracking integration`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		RunE:    func(cmd *cobra.Command, args []string) error { return run(opts) },
	}

	f := root.Flags()
	f.StringVar(&opts.addr, "addr", ":8888", "HTTP listen address for scheduler extender")
	f.StringVar(&opts.metricsAddr, "metrics-bind-address", ":8084", "Metrics endpoint address")
	f.StringVar(&opts.schedulingPolicy, "scheduling-policy", "binpack", "Scheduling policy: binpack or spread")
	f.BoolVar(&opts.numaAware, "numa-aware", true, "Enable NUMA topology-aware scheduling")

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

	logger.Info("starting scheduler-extender",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("policy", opts.schedulingPolicy),
		zap.Bool("numaAware", opts.numaAware))

	// TODO Sprint 7: implement scheduler extender HTTP endpoints
	// POST /filter   — remove nodes that cannot fit the GPU request
	// POST /prioritize — score remaining nodes by bin-pack/spread policy
	mux := http.NewServeMux()
	mux.HandleFunc("/filter", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return unfiltered node list (pass-through)
		fmt.Fprintf(w, `{"Nodes":{"Items":[]}}`)
	})
	mux.HandleFunc("/prioritize", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `[]`)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:         opts.addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("scheduler-extender listening", zap.String("addr", opts.addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-quit:
		logger.Info("received signal, shutting down", zap.String("signal", sig.String()))
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}
