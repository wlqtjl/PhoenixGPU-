// Package checkpoint — Snapshot Upload Worker Pool.
//
// Architecture (Engineering Covenant §2.4):
//
//   CRIU Watcher ──→ Task Channel (buffered=64) ──→ [Worker 1..N] ──→ StorageBackend
//
// Guarantees:
//   - Fixed N workers (configurable, no dynamic scaling — YAGNI)
//   - S3 upload timeout does NOT block local checkpoint generation
//   - Failed uploads are retried up to MaxRetries; local snapshot is retained
//   - All key paths instrumented with Prometheus metrics (Covenant §4)
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package checkpoint

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

const (
	defaultWorkers     = 4
	defaultChanBuf     = 64
	defaultUploadTO    = 5 * time.Minute
	defaultMaxRetries  = 3
)

// UploaderConfig controls Worker Pool behaviour.
type UploaderConfig struct {
	Workers       int           // number of concurrent upload workers (default 4)
	ChannelBuffer int           // task channel buffer size (default 64)
	UploadTimeout time.Duration // per-file S3 upload timeout (default 5min)
	MaxRetries    int           // max upload attempts before giving up (default 3)
}

func (c *UploaderConfig) withDefaults() UploaderConfig {
	out := *c
	if out.Workers <= 0       { out.Workers = defaultWorkers }
	if out.ChannelBuffer <= 0 { out.ChannelBuffer = defaultChanBuf }
	if out.UploadTimeout <= 0 { out.UploadTimeout = defaultUploadTO }
	if out.MaxRetries <= 0    { out.MaxRetries = defaultMaxRetries }
	return out
}

// Uploader manages a pool of workers that upload CRIU snapshots to a StorageBackend.
type Uploader struct {
	backend  StorageBackend
	cfg      UploaderConfig
	logger   *zap.Logger
	tasks    chan UploadTask

	// Atomic counters for O(1) status queries — Covenant §2.3 (atomic > mutex for counters)
	pending   int64
	succeeded int64
	failed    int64

	// Prometheus (Covenant §4)
	taskQueueDepth   prometheus.Gauge
	uploadSuccessful prometheus.Counter
	uploadFailed     *prometheus.CounterVec
	uploadDuration   *prometheus.HistogramVec

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// NewUploader creates and starts the Worker Pool.
// Call Shutdown() to drain and stop workers gracefully.
func NewUploader(backend StorageBackend, cfg UploaderConfig, logger *zap.Logger) *Uploader {
	cfg = cfg.withDefaults()

	u := &Uploader{
		backend: backend,
		cfg:     cfg,
		logger:  logger,
		tasks:   make(chan UploadTask, cfg.ChannelBuffer),

		taskQueueDepth: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "phoenixgpu_uploader_queue_depth",
			Help: "Number of snapshot upload tasks waiting in queue",
		}),
		uploadSuccessful: promauto.NewCounter(prometheus.CounterOpts{
			Name: "phoenixgpu_uploader_success_total",
			Help: "Total successfully uploaded snapshots",
		}),
		uploadFailed: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "phoenixgpu_uploader_failure_total",
			Help: "Total failed snapshot uploads by reason",
		}, []string{"reason"}),
		uploadDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "phoenixgpu_uploader_duration_seconds",
			Help:    "End-to-end upload duration per snapshot",
			Buckets: []float64{5, 15, 30, 60, 120, 300},
		}, []string{"result"}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	u.start(ctx)
	return u
}

// Enqueue submits a snapshot for upload. Non-blocking if channel has space.
// Returns an error only if the queue is full — the caller should log and continue.
func (u *Uploader) Enqueue(task UploadTask) error {
	select {
	case u.tasks <- task:
		atomic.AddInt64(&u.pending, 1)
		u.taskQueueDepth.Set(float64(atomic.LoadInt64(&u.pending)))
		return nil
	default:
		u.uploadFailed.WithLabelValues("queue_full").Inc()
		return fmt.Errorf("upload queue full (depth=%d): snapshot seq=%d will be retried next checkpoint",
			u.cfg.ChannelBuffer, task.Meta.Seq)
	}
}

// Stats returns current pool counters (O(1) via atomic reads).
func (u *Uploader) Stats() (pending, succeeded, failed int64) {
	return atomic.LoadInt64(&u.pending),
		atomic.LoadInt64(&u.succeeded),
		atomic.LoadInt64(&u.failed)
}

// Shutdown signals workers to stop and waits for in-flight uploads to finish.
func (u *Uploader) Shutdown() {
	u.cancel()
	close(u.tasks)
	u.wg.Wait()
	u.logger.Info("uploader shutdown complete",
		zap.Int64("totalSucceeded", atomic.LoadInt64(&u.succeeded)),
		zap.Int64("totalFailed", atomic.LoadInt64(&u.failed)))
}

// ── Internal ─────────────────────────────────────────────────────

func (u *Uploader) start(ctx context.Context) {
	for i := 0; i < u.cfg.Workers; i++ {
		u.wg.Add(1)
		go func(workerID int) {
			defer u.wg.Done()
			u.workerLoop(ctx, workerID)
		}(i)
	}
	u.logger.Info("uploader started",
		zap.Int("workers", u.cfg.Workers),
		zap.Int("channelBuffer", u.cfg.ChannelBuffer),
		zap.Duration("uploadTimeout", u.cfg.UploadTimeout))
}

func (u *Uploader) workerLoop(ctx context.Context, workerID int) {
	log := u.logger.With(zap.Int("worker", workerID))
	for task := range u.tasks {
		atomic.AddInt64(&u.pending, -1)
		u.taskQueueDepth.Set(float64(atomic.LoadInt64(&u.pending)))
		u.processTask(ctx, task, log)
	}
	log.Debug("worker stopped")
}

func (u *Uploader) processTask(ctx context.Context, task UploadTask, log *zap.Logger) {
	start := time.Now()

	// Engineering Covenant §6: Upload timeout is ISOLATED from the caller's ctx.
	// Even if caller is gone, we finish the upload (or time out independently).
	uploadCtx, cancel := context.WithTimeout(context.Background(), u.cfg.UploadTimeout)
	defer cancel()

	// Also respect overall shutdown via the parent ctx
	go func() {
		select {
		case <-ctx.Done():
			cancel() // shutdown triggered — abort upload
		case <-uploadCtx.Done():
		}
	}()

	log.Info("uploading snapshot",
		zap.String("job", task.Meta.JobKey()),
		zap.Int("seq", task.Meta.Seq),
		zap.Int("attempt", task.Attempt+1),
		zap.String("src", task.SourceDir))

	err := u.backend.Save(uploadCtx, task.SourceDir, task.Meta)
	elapsed := time.Since(start)

	if err == nil {
		atomic.AddInt64(&u.succeeded, 1)
		u.uploadSuccessful.Inc()
		u.uploadDuration.WithLabelValues("success").Observe(elapsed.Seconds())
		log.Info("snapshot uploaded",
			zap.String("job", task.Meta.JobKey()),
			zap.Int("seq", task.Meta.Seq),
			zap.Duration("elapsed", elapsed))
		return
	}

	// Upload failed
	atomic.AddInt64(&u.failed, 1)
	u.uploadDuration.WithLabelValues("failure").Observe(elapsed.Seconds())

	if task.Attempt+1 >= u.cfg.MaxRetries {
		u.uploadFailed.WithLabelValues("max_retries").Inc()
		log.Error("snapshot upload failed: max retries reached, local copy retained",
			zap.String("job", task.Meta.JobKey()),
			zap.Int("seq", task.Meta.Seq),
			zap.Int("attempts", task.Attempt+1),
			zap.Error(err))
		// Engineering Covenant §6: local snapshot is RETAINED, not deleted
		return
	}

	// Retry with exponential backoff
	backoff := time.Duration(1<<uint(task.Attempt)) * 10 * time.Second
	log.Warn("snapshot upload failed, will retry",
		zap.String("job", task.Meta.JobKey()),
		zap.Int("seq", task.Meta.Seq),
		zap.Duration("retryAfter", backoff),
		zap.Error(err))

	retryTask := task
	retryTask.Attempt++
	time.AfterFunc(backoff, func() {
		if err := u.Enqueue(retryTask); err != nil {
			log.Error("retry enqueue failed", zap.Error(err))
			u.uploadFailed.WithLabelValues("retry_enqueue_failed").Inc()
		}
	})
}
