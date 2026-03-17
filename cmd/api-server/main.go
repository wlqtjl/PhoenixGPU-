// phoenix-api-server — PhoenixGPU REST API Server
//
// Serves WebUI and external integrations via a RESTful JSON API.
// Reads live data from Kubernetes API server and Prometheus/DCGM.
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

	"github.com/wlqtjl/PhoenixGPU/cmd/api-server/internal"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type opts struct {
	addr        string
	metricsAddr string
	mock        bool
	logLevel    string
}

func main() {
	o := &opts{}
	root := &cobra.Command{
		Use:     "phoenix-api-server",
		Short:   "PhoenixGPU REST API server",
		Version: fmt.Sprintf("%s (%s, %s)", version, commit, date),
		RunE:    func(cmd *cobra.Command, _ []string) error { return run(o) },
	}

	f := root.Flags()
	f.StringVar(&o.addr,        "addr",         ":8090",   "HTTP listen address")
	f.StringVar(&o.metricsAddr, "metrics-addr", ":8091",   "Prometheus metrics address")
	f.BoolVar(&o.mock,          "mock",          false,     "Use fake data (no K8s connection)")
	f.StringVar(&o.logLevel,    "log-level",     "info",    "Log level: debug|info|warn|error")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(o *opts) error {
	// ── Logger ────────────────────────────────────────────────────
	var logger *zap.Logger
	var err error
	if o.logLevel == "debug" {
		logger, err = zap.NewDevelopment()
	} else {
		logger, err = zap.NewProduction()
	}
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck

	logger.Info("starting phoenix-api-server",
		zap.String("version", version),
		zap.String("addr", o.addr),
		zap.Bool("mock", o.mock))

	// ── K8s client ────────────────────────────────────────────────
	var client internal.K8sClientInterface
	if o.mock {
		logger.Warn("mock mode enabled — using fake data")
		client = internal.NewFakeK8sClient()
	} else {
		// TODO Sprint 5: RealK8sClient using in-cluster config
		logger.Warn("real K8s client not yet implemented, falling back to mock")
		client = internal.NewFakeK8sClient()
	}

	// ── HTTP server ───────────────────────────────────────────────
	router := internal.NewRouter(internal.RouterConfig{
		K8sClient:  client,
		Logger:     logger,
		EnableMock: o.mock,
	})

	srv := &http.Server{
		Addr:         o.addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// ── Graceful shutdown ─────────────────────────────────────────
	errCh := make(chan error, 1)
	go func() {
		logger.Info("API server listening", zap.String("addr", o.addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("server error: %w", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case serverErr := <-errCh:
		return fmt.Errorf("api server fatal: %w", serverErr)
	case sig := <-quit:
		logger.Info("shutting down", zap.String("signal", sig.String()))
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}
