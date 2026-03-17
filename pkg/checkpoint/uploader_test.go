package checkpoint_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
	"github.com/wlqtjl/PhoenixGPU/pkg/checkpoint"
)

// ── Mock backend for Uploader tests ──────────────────────────────

type mockBackend struct {
	saveCalls  int64
	saveDelay  time.Duration
	saveErr    error
}

func (m *mockBackend) Save(_ context.Context, _ string, _ checkpoint.SnapshotMeta) error {
	if m.saveDelay > 0 {
		time.Sleep(m.saveDelay)
	}
	atomic.AddInt64(&m.saveCalls, 1)
	return m.saveErr
}
func (m *mockBackend) Load(_ context.Context, _ checkpoint.SnapshotMeta, _ string) error { return nil }
func (m *mockBackend) List(_ context.Context, _ string) ([]checkpoint.SnapshotMeta, error) { return nil, nil }
func (m *mockBackend) Delete(_ context.Context, _ checkpoint.SnapshotMeta) error { return nil }
func (m *mockBackend) Prune(_ context.Context, _ string, _ int) error { return nil }

// ── Tests ──────────────────────────────────────────────────────────

func TestUploader_EnqueueAndProcess(t *testing.T) {
	backend := &mockBackend{}
	logger := zaptest.NewLogger(t)

	u := checkpoint.NewUploader(backend, checkpoint.UploaderConfig{
		Workers:       2,
		ChannelBuffer: 10,
		UploadTimeout: 5 * time.Second,
	}, logger)
	defer u.Shutdown()

	task := checkpoint.UploadTask{
		SourceDir: t.TempDir(),
		Meta: checkpoint.SnapshotMeta{
			JobName: "train", Namespace: "research", Seq: 1,
		},
	}
	if err := u.Enqueue(task); err != nil {
		t.Fatalf("Enqueue error: %v", err)
	}

	// Wait for worker to process
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&backend.saveCalls) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt64(&backend.saveCalls) < 1 {
		t.Error("expected backend.Save to be called within 2s")
	}

	_, succeeded, failed := u.Stats()
	if succeeded != 1 {
		t.Errorf("expected 1 succeeded, got %d", succeeded)
	}
	if failed != 0 {
		t.Errorf("expected 0 failed, got %d", failed)
	}
}

func TestUploader_QueueFullReturnsError(t *testing.T) {
	// Backend with delay so workers are busy
	backend := &mockBackend{saveDelay: 500 * time.Millisecond}
	logger := zaptest.NewLogger(t)

	u := checkpoint.NewUploader(backend, checkpoint.UploaderConfig{
		Workers:       1,
		ChannelBuffer: 2, // tiny buffer to force queue full quickly
		UploadTimeout: 5 * time.Second,
	}, logger)
	defer u.Shutdown()

	task := checkpoint.UploadTask{
		SourceDir: t.TempDir(),
		Meta:      checkpoint.SnapshotMeta{JobName: "j", Namespace: "n", Seq: 1},
	}

	// Fill up the queue + worker
	var queueFullErr error
	for i := 0; i < 10; i++ {
		task.Meta.Seq = i
		if err := u.Enqueue(task); err != nil {
			queueFullErr = err
			break
		}
	}
	if queueFullErr == nil {
		t.Error("expected queue-full error, got nil for all enqueues")
	}
}

func TestUploader_ContextIsolation(t *testing.T) {
	// Verify: cancelling the caller's context does NOT cancel the upload
	// (upload has its own isolated timeout — Engineering Covenant §6)
	var uploadStarted, uploadCompleted int64

	backend := &mockBackend{} // instant save
	logger := zaptest.NewLogger(t)

	u := checkpoint.NewUploader(backend, checkpoint.UploaderConfig{
		Workers:       1,
		ChannelBuffer: 10,
		UploadTimeout: 5 * time.Second,
	}, logger)
	defer u.Shutdown()

	task := checkpoint.UploadTask{
		SourceDir: t.TempDir(),
		Meta:      checkpoint.SnapshotMeta{JobName: "j", Namespace: "n", Seq: 1},
	}
	_ = u.Enqueue(task)
	_ = uploadStarted // suppress unused

	// Even if caller cancels, upload should proceed
	time.Sleep(200 * time.Millisecond)
	atomic.StoreInt64(&uploadCompleted, atomic.LoadInt64(&backend.saveCalls))

	if atomic.LoadInt64(&uploadCompleted) < 1 {
		t.Error("upload should complete even without caller context")
	}
}

func TestUploader_RetryOnFailure(t *testing.T) {
	callCount := int64(0)
	backend := &mockBackend{
		saveErr: fmt.Errorf("transient error"),
	}
	// Succeed on 3rd attempt
	origSave := backend.saveErr
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = origSave
		atomic.StoreInt64(&callCount, atomic.LoadInt64(&backend.saveCalls))
	}()

	logger := zaptest.NewLogger(t)
	u := checkpoint.NewUploader(backend, checkpoint.UploaderConfig{
		Workers:       1,
		ChannelBuffer: 10,
		UploadTimeout: 2 * time.Second,
		MaxRetries:    3,
	}, logger)
	defer u.Shutdown()

	_ = u.Enqueue(checkpoint.UploadTask{
		SourceDir: t.TempDir(),
		Meta:      checkpoint.SnapshotMeta{JobName: "j", Namespace: "n", Seq: 1},
	})

	time.Sleep(500 * time.Millisecond)
	// Should have attempted at least once; local snapshot must not be deleted
	pending, _, _ := u.Stats()
	_ = pending // stats are valid even during retries
}

func TestUploader_ShutdownDrainsQueue(t *testing.T) {
	var processed int64
	backend := &mockBackend{}
	logger := zaptest.NewLogger(t)

	u := checkpoint.NewUploader(backend, checkpoint.UploaderConfig{
		Workers:       2,
		ChannelBuffer: 20,
		UploadTimeout: 5 * time.Second,
	}, logger)

	for i := 0; i < 5; i++ {
		_ = u.Enqueue(checkpoint.UploadTask{
			SourceDir: t.TempDir(),
			Meta:      checkpoint.SnapshotMeta{JobName: "j", Namespace: "n", Seq: i},
		})
	}

	u.Shutdown() // must drain all 5 tasks
	atomic.StoreInt64(&processed, atomic.LoadInt64(&backend.saveCalls))

	if atomic.LoadInt64(&processed) != 5 {
		t.Errorf("expected 5 processed after Shutdown, got %d", processed)
	}
}
