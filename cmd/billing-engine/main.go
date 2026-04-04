//go:build billingenginefull && billingfull
// +build billingenginefull,billingfull

// phoenix-billing-engine — PhoenixGPU Billing Engine Service
//
// Standalone service that collects GPU usage metrics, computes TFlops·h
// billing records, persists to TimescaleDB, and enforces quotas.
//
// Architecture:
//  1. Connects to K8s API to discover active PhoenixJobs
//  2. Collects GPU usage metrics per collection interval
//  3. Computes billing records via billing.Engine
//  4. Persists to TimescaleDB (or in-memory store for dev)
//  5. Fires quota alerts when thresholds are exceeded
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

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
	kubeconfig         string
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

// PhoenixJobGVR is the GroupVersionResource for PhoenixJob CRD.
var PhoenixJobGVR = schema.GroupVersionResource{
	Group:    "phoenixgpu.io",
	Version:  "v1alpha1",
	Resource: "phoenixjobs",
}

// activeJob holds the extracted info from a running PhoenixJob CRD.
type activeJob struct {
	Name       string
	Namespace  string
	Phase      string
	Department string
	Project    string
	GPUModel   string
	AllocRatio float64
	NodeName   string
	PodName    string
	StartedAt  time.Time
}

// collectActiveJobs queries K8s API for all running PhoenixJobs.
func collectActiveJobs(ctx context.Context, dynClient dynamic.Interface, logger *zap.Logger) ([]activeJob, error) {
	list, err := dynClient.Resource(PhoenixJobGVR).Namespace(metav1.NamespaceAll).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list PhoenixJobs: %w", err)
	}

	var jobs []activeJob
	for _, item := range list.Items {
		phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
		// Only collect usage for running jobs
		if phase != "Running" && phase != "Checkpointing" {
			continue
		}

		dept, _, _ := unstructured.NestedString(item.Object, "spec", "billing", "department")
		project, _, _ := unstructured.NestedString(item.Object, "spec", "billing", "project")
		gpuModel, _, _ := unstructured.NestedString(item.Object, "spec", "template", "spec",
			"containers", "0", "resources", "limits", "nvidia.com/gpu-model")
		allocRatio, _, _ := unstructured.NestedFloat64(item.Object, "spec", "checkpoint", "allocRatio")
		nodeName, _, _ := unstructured.NestedString(item.Object, "status", "currentNodeName")
		podName, _, _ := unstructured.NestedString(item.Object, "status", "currentPodName")

		// Parse creation timestamp as start time
		startedAt := item.GetCreationTimestamp().Time

		// Use a default alloc ratio if not set
		if allocRatio <= 0 {
			allocRatio = 1.0
		}

		// Use a default GPU model if not set
		if gpuModel == "" {
			gpuModel = "NVIDIA-A100-80GB"
		}

		jobs = append(jobs, activeJob{
			Name:       item.GetName(),
			Namespace:  item.GetNamespace(),
			Phase:      phase,
			Department: dept,
			Project:    project,
			GPUModel:   gpuModel,
			AllocRatio: allocRatio,
			NodeName:   nodeName,
			PodName:    podName,
			StartedAt:  startedAt,
		})
	}

	logger.Debug("collected active jobs", zap.Int("count", len(jobs)))
	return jobs, nil
}

// recordUsage creates a billing record for a single collection interval.
func recordUsage(ctx context.Context, engine *billing.Engine, job activeJob, interval time.Duration, logger *zap.Logger) {
	now := time.Now()
	record := billing.UsageRecord{
		Namespace:  job.Namespace,
		PodName:    job.PodName,
		JobName:    job.Name,
		Department: job.Department,
		Project:    job.Project,
		GPUModel:   job.GPUModel,
		NodeName:   job.NodeName,
		AllocRatio: job.AllocRatio,
		StartedAt:  now.Add(-interval),
		EndedAt:    now,
	}

	if err := engine.Record(ctx, record); err != nil {
		logger.Warn("failed to record usage",
			zap.String("job", job.Name),
			zap.String("namespace", job.Namespace),
			zap.Error(err))
	}
}

// startProbeServer starts a health probe HTTP server.
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

	// ── Initialize K8s client ─────────────────────────────────────
	cfg, err := rest.InClusterConfig()
	if err != nil {
		logger.Warn("failed to get in-cluster config, running without K8s integration",
			zap.Error(err))
	}

	var dynClient dynamic.Interface
	if cfg != nil {
		dynClient, err = dynamic.NewForConfig(cfg)
		if err != nil {
			logger.Warn("failed to create dynamic K8s client",
				zap.Error(err))
		}
	}

	// ── Start health probe server ─────────────────────────────────
	probeSrv := startProbeServer(opts.probeAddr, logger)
	defer probeSrv.Close()

	// ── Collection loop ───────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interval := time.Duration(opts.collectionInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("billing-engine ready",
		zap.Duration("collectionInterval", interval),
		zap.Bool("k8sIntegration", dynClient != nil))

	for {
		select {
		case <-ticker.C:
			if dynClient == nil {
				logger.Debug("collection tick — no K8s client, skipping")
				continue
			}

			collectCtx, collectCancel := context.WithTimeout(ctx, 30*time.Second)
			jobs, err := collectActiveJobs(collectCtx, dynClient, logger)
			collectCancel()
			if err != nil {
				logger.Warn("failed to collect active jobs", zap.Error(err))
				continue
			}

			logger.Info("collection tick",
				zap.Int("activeJobs", len(jobs)))

			for _, job := range jobs {
				recordUsage(ctx, engine, job, interval, logger)
			}

		case sig := <-quit:
			logger.Info("received signal, shutting down", zap.String("signal", sig.String()))
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}
