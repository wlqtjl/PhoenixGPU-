// phoenix-controller — PhoenixGPU HA Controller
//
// Watches PhoenixJob CRDs, schedules periodic Checkpoints,
// and triggers Restore on node failure.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/wlqtjl/PhoenixGPU/pkg/checkpoint"
	"github.com/wlqtjl/PhoenixGPU/pkg/hacontroller"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type options struct {
	metricsAddr          string
	probeAddr            string
	checkpointDir        string
	checkpointIntervalS  int
	notReadyThresholdS   int
	faultPollIntervalS   int
	maxRestoreAttempts   int
	restoreTimeoutS      int
	leaderElection       bool
	leaderElectionID     string
}

func main() {
	opts := &options{}

	root := &cobra.Command{
		Use:   "phoenix-controller",
		Short: "PhoenixGPU HA Controller — GPU fault detection and checkpoint/restore",
		Long: `phoenix-controller watches GPU training jobs (PhoenixJob CRDs) and:
  1. Periodically checkpoints running jobs using CRIU
  2. Detects node failures via Node NotReady conditions
  3. Automatically restores jobs on healthy nodes from the latest snapshot`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		RunE:    func(cmd *cobra.Command, args []string) error { return run(opts) },
	}

	f := root.Flags()
	f.StringVar(&opts.metricsAddr, "metrics-bind-address", ":8080", "Metrics endpoint address")
	f.StringVar(&opts.probeAddr, "health-probe-bind-address", ":8081", "Health probe address")
	f.StringVar(&opts.checkpointDir, "checkpoint-dir", "/mnt/phoenix-snapshots", "Snapshot storage root")
	f.IntVar(&opts.checkpointIntervalS, "checkpoint-interval", 300, "Default checkpoint interval (seconds)")
	f.IntVar(&opts.notReadyThresholdS, "notready-threshold", 30, "Seconds before a NotReady node is treated as faulted")
	f.IntVar(&opts.faultPollIntervalS, "fault-poll-interval", 10, "Node fault poll interval (seconds)")
	f.IntVar(&opts.maxRestoreAttempts, "max-restore-attempts", 3, "Max restore attempts before marking job Failed")
	f.IntVar(&opts.restoreTimeoutS, "restore-timeout", 120, "Restore timeout (seconds)")
	f.BoolVar(&opts.leaderElection, "leader-elect", true, "Enable leader election for HA controller")
	f.StringVar(&opts.leaderElectionID, "leader-election-id", "phoenixgpu-controller-leader", "Leader election resource name")

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(opts *options) error {
	// ── Logger ───────────────────────────────────────────────────
	logger, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck
	ctrl.SetLogger(ctrlzap.New(ctrlzap.UseFlagOptions(&ctrlzap.Options{})))

	logger.Info("starting phoenix-controller",
		zap.String("version", version),
		zap.String("commit", commit))

	// ── Controller-runtime Manager ────────────────────────────────
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme(),
		MetricsBindAddress:     opts.metricsAddr,
		HealthProbeBindAddress: opts.probeAddr,
		LeaderElection:         opts.leaderElection,
		LeaderElectionID:       opts.leaderElectionID,
	})
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}

	// ── Checkpointer ─────────────────────────────────────────────
	ckpter, err := checkpoint.NewCRIUCheckpointer(opts.checkpointDir, logger)
	if err != nil {
		logger.Warn("CRIU checkpointer unavailable, HA checkpoint disabled",
			zap.Error(err))
		// Don't fatal — controller still useful for scheduling without checkpoint
	}
	if ckpter != nil {
		if err := ckpter.Available(); err != nil {
			logger.Warn("CRIU check failed on this node", zap.Error(err))
		}
	}

	// ── HA Controller ────────────────────────────────────────────
	haCtrl := &hacontroller.PhoenixHAController{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		Logger:                logger,
		CheckpointInterval:    time.Duration(opts.checkpointIntervalS) * time.Second,
		RestoreTimeoutSeconds: opts.restoreTimeoutS,
		MaxRestoreAttempts:    opts.maxRestoreAttempts,
	}
	if ckpter != nil {
		haCtrl.Checkpointer = ckpter
	}

	if err := haCtrl.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setup HA controller: %w", err)
	}

	// ── Fault Detector ────────────────────────────────────────────
	faultDetector := hacontroller.NewFaultDetector(
		mgr.GetClient(),
		logger,
		haCtrl.HandleNodeFault,
	)
	faultDetector.PollInterval      = time.Duration(opts.faultPollIntervalS) * time.Second
	faultDetector.NotReadyThreshold = time.Duration(opts.notReadyThresholdS) * time.Second

	// Run fault detector as a background goroutine inside the manager
	if err := mgr.Add(runnable(func(ctx context.Context) error {
		faultDetector.Run(ctx)
		return nil
	})); err != nil {
		return fmt.Errorf("add fault detector: %w", err)
	}

	// ── Health checks ─────────────────────────────────────────────
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("add healthz: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("add readyz: %w", err)
	}

	logger.Info("phoenix-controller ready",
		zap.String("checkpointDir", opts.checkpointDir),
		zap.Duration("checkpointInterval", time.Duration(opts.checkpointIntervalS)*time.Second),
		zap.Duration("notReadyThreshold", time.Duration(opts.notReadyThresholdS)*time.Second))

	return mgr.Start(ctrl.SetupSignalHandler())
}

// runnable adapts a func to controller-runtime's Runnable interface.
type runnable func(ctx context.Context) error

func (r runnable) Start(ctx context.Context) error { return r(ctx) }

func scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	return s
}
