//go:build billingenginefull && billingfull
// +build billingenginefull,billingfull

// phoenix-billing-engine — PhoenixGPU Billing Engine Service
//
// Standalone service that collects GPU usage metrics, computes TFlops·h
// billing records, persists to TimescaleDB, and enforces quotas.
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
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/wlqtjl/PhoenixGPU/pkg/billing"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type options struct {
	dbDSN              string
	collectionInterval int
	metricsAddr        string
	probeAddr          string
}

func main() {
	opts := &options{}

	root := &cobra.Command{
		Use:   "billing-engine",
		Short: "PhoenixGPU Billing Engine — GPU usage metering and quota enforcement",
		Long: `billing-engine runs as a standalone service and:
  1. Collects GPU usage metrics from PhoenixJob annotations and DCGM
  2. Computes TFlops·h billing records
  3. Persists records to TimescaleDB via PostgresStore
  4. Enforces quota policies with configurable soft/hard limits
  5. Fires alerts when quota thresholds are exceeded`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		RunE:    func(cmd *cobra.Command, args []string) error { return run(opts) },
	}

	f := root.Flags()
	f.StringVar(&opts.dbDSN, "db-dsn", "", "TimescaleDB connection DSN (e.g. postgres://phoenix:secret@localhost:5432/phoenixgpu?sslmode=disable)")
	f.IntVar(&opts.collectionInterval, "collection-interval", 60, "Usage collection interval (seconds)")
	f.StringVar(&opts.metricsAddr, "metrics-bind-address", ":8085", "Metrics endpoint address")
	f.StringVar(&opts.probeAddr, "health-probe-bind-address", ":8086", "Health probe address")

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

	logger.Info("starting billing-engine",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.Int("collectionInterval", opts.collectionInterval))

	// ── Initialize store ─────────────────────────────────────────
	var store billing.Store
	if opts.dbDSN != "" {
		pgStore, err := billing.NewPostgresStore(opts.dbDSN, logger)
		if err != nil {
			return fmt.Errorf("connect to TimescaleDB: %w", err)
		}
		defer pgStore.Close()
		store = pgStore
		logger.Info("connected to TimescaleDB")
	} else {
		store = billing.NewMemoryStore()
		logger.Warn("no --db-dsn provided, using in-memory store (data will be lost on restart)")
	}

	// ── Initialize billing engine ─────────────────────────────────
	engine := billing.NewEngine(store, logger)

	// Register a logging alert hook
	engine.RegisterAlertHook(func(ctx context.Context, status billing.QuotaStatus) {
		logger.Warn("quota alert fired",
			zap.String("tenant", status.Policy.TenantID),
			zap.Float64("usedPct", status.UsedPct),
			zap.Float64("usedGPUHours", status.UsedGPUHours))
	})

	// ── Collection loop ───────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interval := time.Duration(opts.collectionInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("billing-engine ready",
		zap.Duration("collectionInterval", interval))

	// TODO Sprint 7: integrate with K8s API to discover active PhoenixJobs
	// and collect real usage metrics per collection interval
	_ = engine

	for {
		select {
		case <-ticker.C:
			logger.Debug("collection tick — pending real K8s integration")
		case sig := <-quit:
			logger.Info("received signal, shutting down", zap.String("signal", sig.String()))
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}
