// Package checkpoint_test — TDD: tests written BEFORE full implementation.
// These define the contract that CRIUCheckpointer must fulfill.
//
// Red → Green → Refactor
package checkpoint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func newTestLogger(t *testing.T) *zap.Logger {
	return zaptest.NewLogger(t)
}

func criuAvailable() bool {
	_, err := exec.LookPath("criu")
	return err == nil
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "phoenixgpu-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// ─── T08-1: Constructor tests ─────────────────────────────────────────────────

func TestNewCRIUCheckpointer_BinaryNotFound(t *testing.T) {
	// Simulate missing CRIU by temporarily manipulating PATH
	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)
	os.Setenv("PATH", "/nonexistent")

	_, err := NewCRIUCheckpointer("/tmp/ckpt", newTestLogger(t))
	if err == nil {
		t.Fatal("expected error when criu binary not found, got nil")
	}
}

func TestNewCRIUCheckpointer_Success(t *testing.T) {
	if !criuAvailable() {
		t.Skip("criu not installed — skipping (expected in CI without GPU)")
	}

	dir := tempDir(t)
	c, err := NewCRIUCheckpointer(dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("checkpointer should not be nil")
	}
}

// ─── T08-2: Available() contract ─────────────────────────────────────────────

func TestAvailable_ReturnsErrorWhenCRIUAbsent(t *testing.T) {
	c := &CRIUCheckpointer{
		criuBin: "/nonexistent/criu",
		logger:  newTestLogger(t),
	}
	if err := c.Available(); err == nil {
		t.Fatal("Available() should fail when criu binary is wrong path")
	}
}

func TestAvailable_SucceedsOnNode(t *testing.T) {
	if !criuAvailable() {
		t.Skip("criu not installed")
	}
	dir := tempDir(t)
	c, _ := NewCRIUCheckpointer(dir, newTestLogger(t))
	if err := c.Available(); err != nil {
		t.Errorf("Available() returned error on node with criu: %v", err)
	}
}

// ─── T08-3: SnapshotPath contract ────────────────────────────────────────────

func TestSnapshotPath_Format(t *testing.T) {
	c := &CRIUCheckpointer{
		checkpointDir: "/mnt/snapshots",
		logger:        newTestLogger(t),
	}

	cases := []struct {
		ns, job string
		seq     int
		want    string
	}{
		{"default", "train-job", 1, "/mnt/snapshots/default/train-job/ckpt-00001"},
		{"research", "llm-pretrain", 42, "/mnt/snapshots/research/llm-pretrain/ckpt-00042"},
		{"default", "job", 99999, "/mnt/snapshots/default/job/ckpt-99999"},
	}

	for _, tc := range cases {
		got := c.SnapshotPath(tc.ns, tc.job, tc.seq)
		if got != tc.want {
			t.Errorf("SnapshotPath(%q,%q,%d) = %q, want %q",
				tc.ns, tc.job, tc.seq, got, tc.want)
		}
	}
}

// ─── T08-4: Dump creates directory ───────────────────────────────────────────

func TestDump_CreatesDirIfNotExists(t *testing.T) {
	if !criuAvailable() {
		t.Skip("criu not installed")
	}

	base := tempDir(t)
	newDir := filepath.Join(base, "new", "nested", "dir")

	c, _ := NewCRIUCheckpointer(base, newTestLogger(t))

	// Dump a harmless process — use current process PID for testing
	// In real env this would be the training job PID
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// We expect this to create the dir, even if dump itself may fail
	// (criu needs CAP_SYS_PTRACE in most environments)
	_ = c.Dump(ctx, os.Getpid(), newDir)

	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		t.Error("Dump() should create snapshot directory even if criu fails")
	}
}

// ─── T08-5: Restore interface compliance ─────────────────────────────────────

func TestRestore_FailsGracefullyOnMissingDir(t *testing.T) {
	if !criuAvailable() {
		t.Skip("criu not installed")
	}

	c, _ := NewCRIUCheckpointer("/tmp", newTestLogger(t))
	ctx := context.Background()

	_, err := c.Restore(ctx, "/nonexistent/checkpoint/dir")
	if err == nil {
		t.Fatal("Restore() should return error when snapshot dir does not exist")
	}
}

// ─── T08-6: dirSize helper ───────────────────────────────────────────────────

func TestDirSize_EmptyDir(t *testing.T) {
	dir := tempDir(t)
	size, err := dirSize(dir)
	if err != nil {
		t.Fatalf("dirSize error: %v", err)
	}
	if size != 0 {
		t.Errorf("expected 0 bytes for empty dir, got %d", size)
	}
}

func TestDirSize_WithFiles(t *testing.T) {
	dir := tempDir(t)

	// Write two known-size files
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), make([]byte, 1024), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), make([]byte, 2048), 0644); err != nil {
		t.Fatal(err)
	}

	size, err := dirSize(dir)
	if err != nil {
		t.Fatalf("dirSize error: %v", err)
	}
	if size != 3072 {
		t.Errorf("expected 3072 bytes, got %d", size)
	}
}

// ─── T08-7: parseRestoredPID ────────────────────────────────────────────────

func TestParseRestoredPID(t *testing.T) {
	cases := []struct {
		input   string
		wantPID int
		wantErr bool
	}{
		{"Restored process pid 12345, done", 12345, false},
		{"no pid info here", 0, true},
		{"Restored process pid 99,", 99, false},
	}

	for _, tc := range cases {
		pid, err := parseRestoredPID(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseRestoredPID(%q) error=%v, wantErr=%v", tc.input, err, tc.wantErr)
		}
		if !tc.wantErr && pid != tc.wantPID {
			t.Errorf("parseRestoredPID(%q) = %d, want %d", tc.input, pid, tc.wantPID)
		}
	}
}

// ─── T08-8: Integration stub (Sprint 2 will flesh this out) ─────────────────

// TestCheckpointRestoreCycle_Integration will verify the full
// checkpoint → restore → training-continues cycle.
// This test is intentionally skipped until Sprint 2 HA Controller lands.
func TestCheckpointRestoreCycle_Integration(t *testing.T) {
	t.Skip("Sprint 2: implement after PhoenixHA Controller is ready")

	// Acceptance criteria (to be implemented):
	// 1. Start a synthetic training process (writes incrementing counter to file)
	// 2. Checkpoint at counter=50
	// 3. Kill process
	// 4. Restore process
	// 5. Assert process continues from counter >= 50 (not reset to 0)
}
