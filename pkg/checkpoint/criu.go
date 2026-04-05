//go:build checkpointfull
// +build checkpointfull

// Package checkpoint provides CRIU-based GPU process checkpoint and restore.
//
// PhoenixGPU Core — Self-developed (not derived from HAMi)
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package checkpoint

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

// ─── Prometheus metrics for CRIU operations ──────────────────────────────────

var (
	criuMetricsOnce    sync.Once
	criuDumpDuration   *prometheus.HistogramVec
	criuDumpTotal      *prometheus.CounterVec
	criuRestoreDuration *prometheus.HistogramVec
	criuRestoreTotal   *prometheus.CounterVec
	criuSnapshotBytes  *prometheus.GaugeVec
)

func initCRIUMetrics() {
	criuMetricsOnce.Do(func() {
		criuDumpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "phoenixgpu_criu_dump_duration_seconds",
			Help:    "CRIU dump operation duration in seconds",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
		}, []string{"result"})

		criuDumpTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "phoenixgpu_criu_dump_total",
			Help: "Total number of CRIU dump operations",
		}, []string{"result"})

		criuRestoreDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "phoenixgpu_criu_restore_duration_seconds",
			Help:    "CRIU restore operation duration in seconds",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
		}, []string{"result"})

		criuRestoreTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "phoenixgpu_criu_restore_total",
			Help: "Total number of CRIU restore operations",
		}, []string{"result"})

		criuSnapshotBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "phoenixgpu_criu_snapshot_bytes",
			Help: "Size of the last checkpoint snapshot in bytes",
		}, []string{"dir"})
	})
}

// CRIUCheckpointer implements Checkpointer using CRIU.
// It wraps the `criu` CLI and handles GPU context via the cuda-checkpoint plugin.
type CRIUCheckpointer struct {
	criuBin       string // path to criu binary
	checkpointDir string // base directory for snapshot files
	gpuPlugin     bool   // whether cuda-checkpoint plugin is available
	logger        *zap.Logger
}

// Checkpointer is the interface all checkpoint backends must implement.
type Checkpointer interface {
	// Dump freezes the process and writes checkpoint files to Dir.
	Dump(ctx context.Context, pid int, dir string) error
	// PreDump writes memory pages without stopping the process (for incremental).
	PreDump(ctx context.Context, pid int, dir string) error
	// Restore restarts the process from a checkpoint directory.
	Restore(ctx context.Context, dir string) (int, error)
	// Available checks if the checkpoint backend is usable on this node.
	Available() error
}

// NewCRIUCheckpointer creates a new CRIU-backed checkpointer.
func NewCRIUCheckpointer(checkpointDir string, logger *zap.Logger) (*CRIUCheckpointer, error) {
	criuBin, err := exec.LookPath("criu")
	if err != nil {
		return nil, fmt.Errorf("criu binary not found in PATH: %w", err)
	}

	initCRIUMetrics()

	c := &CRIUCheckpointer{
		criuBin:       criuBin,
		checkpointDir: checkpointDir,
		logger:        logger,
	}

	// Detect GPU plugin availability
	if _, err := exec.LookPath("cuda-checkpoint"); err == nil {
		c.gpuPlugin = true
		logger.Info("cuda-checkpoint plugin detected, GPU context will be saved")
	} else {
		logger.Warn("cuda-checkpoint not found; GPU context checkpoint disabled",
			zap.String("hint", "install cuda-checkpoint for full GPU state persistence"))
	}

	return c, nil
}

// Available checks CRIU health and required capabilities.
func (c *CRIUCheckpointer) Available() error {
	// criu check verifies kernel support
	cmd := exec.Command(c.criuBin, "check")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("criu check failed: %w\noutput: %s", err, string(out))
	}
	return nil
}

// validateDir ensures the directory path is safe for use with CRIU commands.
// It rejects empty paths, relative paths, and paths containing suspicious sequences.
func validateDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("checkpoint directory must not be empty")
	}
	// Reject null bytes that could be used for injection
	if strings.ContainsRune(dir, '\x00') {
		return fmt.Errorf("checkpoint directory contains null byte")
	}
	// Reject path traversal sequences before cleaning
	if strings.Contains(dir, "..") {
		return fmt.Errorf("checkpoint directory must not contain path traversal: %s", dir)
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("checkpoint directory must be an absolute path: %s", dir)
	}
	return nil
}

// validatePath checks that dir is safe for use with CRIU commands and is
// within the checkpointer's configured base directory to prevent path traversal.
func (c *CRIUCheckpointer) validatePath(dir string) error {
	if err := validateDir(dir); err != nil {
		return err
	}
	// Enforce base directory boundary: resolved dir must be under checkpointDir
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve absolute path %s: %w", dir, err)
	}
	baseDir, err := filepath.Abs(c.checkpointDir)
	if err != nil {
		return fmt.Errorf("resolve base directory %s: %w", c.checkpointDir, err)
	}
	if absDir != baseDir && !strings.HasPrefix(absDir, baseDir+string(filepath.Separator)) {
		return fmt.Errorf("checkpoint directory %s is outside base directory %s", dir, c.checkpointDir)
	}
	return nil
}

// Dump performs a full checkpoint of the process with GPU context.
// The process is frozen after dump (use PreDump for live checkpoints).
func (c *CRIUCheckpointer) Dump(ctx context.Context, pid int, dir string) error {
	if err := c.validatePath(dir); err != nil {
		return fmt.Errorf("invalid checkpoint dir: %w", err)
	}
	if pid <= 0 {
		return fmt.Errorf("invalid pid: %d", pid)
	}

	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("mkdir checkpoint dir %s: %w", dir, err)
	}

	c.logger.Info("starting CRIU dump",
		zap.Int("pid", pid),
		zap.String("dir", dir),
		zap.Bool("gpuPlugin", c.gpuPlugin))

	start := time.Now()

	args := []string{
		"dump",
		"--pid", strconv.Itoa(pid),
		"--dir", dir,
		"--shell-job",       // allow jobs with controlling terminals
		"--tcp-established", // checkpoint established TCP connections
		"--leave-running",   // don't kill process after dump (for periodic ckpt)
		"-v4",               // verbose for debugging; reduce in production
	}

	if c.gpuPlugin {
		// cuda-checkpoint runs alongside CRIU to handle GPU state
		args = append(args, "--action-script", c.cudaCheckpointScript(dir))
	}

	cmd := exec.CommandContext(ctx, c.criuBin, args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	if err != nil {
		if criuDumpTotal != nil {
			criuDumpTotal.WithLabelValues("failure").Inc()
			criuDumpDuration.WithLabelValues("failure").Observe(elapsed.Seconds())
		}
		c.logger.Error("CRIU dump failed",
			zap.Int("pid", pid),
			zap.String("dir", dir),
			zap.Duration("elapsed", elapsed),
			zap.String("output", string(out)),
			zap.Error(err))
		return fmt.Errorf("criu dump pid=%d: %w\n%s", pid, err, string(out))
	}

	size, _ := dirSize(dir)
	if criuDumpTotal != nil {
		criuDumpTotal.WithLabelValues("success").Inc()
		criuDumpDuration.WithLabelValues("success").Observe(elapsed.Seconds())
		criuSnapshotBytes.WithLabelValues(dir).Set(float64(size))
	}
	c.logger.Info("CRIU dump successful",
		zap.Int("pid", pid),
		zap.Duration("elapsed", elapsed),
		zap.Int64("snapshotBytes", size))

	return nil
}

// PreDump performs a non-disruptive pre-dump (writes dirty memory pages).
// Use before Dump to reduce freeze time for large models.
func (c *CRIUCheckpointer) PreDump(ctx context.Context, pid int, dir string) error {
	if err := c.validatePath(dir); err != nil {
		return fmt.Errorf("invalid pre-dump dir: %w", err)
	}
	if pid <= 0 {
		return fmt.Errorf("invalid pid: %d", pid)
	}

	preDumpDir := filepath.Join(dir, "pre-dump")
	if err := os.MkdirAll(preDumpDir, 0750); err != nil {
		return fmt.Errorf("mkdir pre-dump dir: %w", err)
	}

	c.logger.Info("starting CRIU pre-dump (non-disruptive)",
		zap.Int("pid", pid),
		zap.String("dir", preDumpDir))

	args := []string{
		"pre-dump",
		"--pid", strconv.Itoa(pid),
		"--dir", preDumpDir,
	}

	cmd := exec.CommandContext(ctx, c.criuBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("criu pre-dump pid=%d: %w\n%s", pid, err, string(out))
	}

	c.logger.Info("CRIU pre-dump completed", zap.Int("pid", pid))
	return nil
}

// Restore starts a new process from a checkpoint directory.
// Returns the PID of the restored process.
func (c *CRIUCheckpointer) Restore(ctx context.Context, dir string) (int, error) {
	if err := c.validatePath(dir); err != nil {
		return 0, fmt.Errorf("invalid restore dir: %w", err)
	}

	c.logger.Info("starting CRIU restore", zap.String("dir", dir))
	start := time.Now()

	args := []string{
		"restore",
		"--dir", dir,
		"--shell-job",
		"--tcp-established",
		"-d", // detach (background)
		"-v4",
	}

	if c.gpuPlugin {
		args = append(args, "--action-script", c.cudaCheckpointScript(dir))
	}

	cmd := exec.CommandContext(ctx, c.criuBin, args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	if err != nil {
		if criuRestoreTotal != nil {
			criuRestoreTotal.WithLabelValues("failure").Inc()
			criuRestoreDuration.WithLabelValues("failure").Observe(elapsed.Seconds())
		}
		c.logger.Error("CRIU restore failed",
			zap.String("dir", dir),
			zap.Duration("elapsed", elapsed),
			zap.String("output", string(out)),
			zap.Error(err))
		return 0, fmt.Errorf("criu restore from %s: %w\n%s", dir, err, string(out))
	}

	// Parse restored PID from output
	pid, parseErr := parseRestoredPID(string(out))
	if criuRestoreTotal != nil {
		criuRestoreTotal.WithLabelValues("success").Inc()
		criuRestoreDuration.WithLabelValues("success").Observe(elapsed.Seconds())
	}
	c.logger.Info("CRIU restore successful",
		zap.Int("restoredPID", pid),
		zap.Duration("elapsed", elapsed))

	if parseErr != nil {
		c.logger.Warn("could not parse restored PID", zap.Error(parseErr))
	}

	return pid, nil
}

// SnapshotPath returns the canonical path for a job's checkpoint.
func (c *CRIUCheckpointer) SnapshotPath(namespace, jobName string, seq int) string {
	return filepath.Join(c.checkpointDir, namespace, jobName, fmt.Sprintf("ckpt-%05d", seq))
}

// cudaCheckpointScript generates the path to the CRIU action script
// that calls cuda-checkpoint at the right lifecycle hooks.
func (c *CRIUCheckpointer) cudaCheckpointScript(dir string) string {
	return filepath.Join(dir, "cuda-action.sh")
}

// dirSize returns total size of all files in a directory.
func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// parseRestoredPID extracts the PID from criu restore output.
func parseRestoredPID(output string) (int, error) {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Restored") && strings.Contains(line, "pid") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "pid" && i+1 < len(fields) {
					return strconv.Atoi(strings.TrimRight(fields[i+1], ","))
				}
			}
		}
	}
	return 0, fmt.Errorf("could not find PID in criu output")
}
