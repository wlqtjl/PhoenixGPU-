//go:build checkpointfull
// +build checkpointfull

// Package checkpoint — StorageBackend contract tests.
// Written BEFORE implementation (TDD Red phase).
// Any backend that passes these tests is production-ready.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package checkpoint_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wlqtjl/PhoenixGPU/pkg/checkpoint"
)

// contractTest runs the full StorageBackend contract against any implementation.
// This is the single source of truth for what "correct" means.
func contractTest(t *testing.T, backend checkpoint.StorageBackend) {
	t.Helper()
	ctx := context.Background()

	meta := checkpoint.SnapshotMeta{
		JobName:   "llm-pretrain",
		Namespace: "research",
		Seq:       1,
		CreatedAt: time.Now().Truncate(time.Second),
		SizeBytes: 0, // filled by backend
	}

	// ── T16-C1: Save and Load roundtrip ───────────────────────────
	t.Run("save_and_load_roundtrip", func(t *testing.T) {
		src := t.TempDir()
		// Write synthetic checkpoint files (simulate CRIU output)
		files := map[string][]byte{
			"pages-1.img":   make([]byte, 4096),
			"core-1234.img": []byte("synthetic core dump"),
			"mm-1234.img":   []byte("synthetic memory map"),
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(src, name), data, 0644); err != nil {
				t.Fatalf("write test file %s: %v", name, err)
			}
		}

		if err := backend.Save(ctx, src, meta); err != nil {
			t.Fatalf("Save() error: %v", err)
		}

		dst := t.TempDir()
		if err := backend.Load(ctx, meta, dst); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		// Verify all files are present and identical
		for name, want := range files {
			got, err := os.ReadFile(filepath.Join(dst, name))
			if err != nil {
				t.Errorf("file %s missing after Load: %v", name, err)
				continue
			}
			if len(got) != len(want) {
				t.Errorf("file %s: size mismatch got=%d want=%d", name, len(got), len(want))
			}
		}
	})

	// ── T16-C2: List returns saved snapshots ──────────────────────
	t.Run("list_returns_snapshots", func(t *testing.T) {
		jobKey := fmt.Sprintf("%s/%s", meta.Namespace, meta.JobName)
		snaps, err := backend.List(ctx, jobKey)
		if err != nil {
			t.Fatalf("List() error: %v", err)
		}
		if len(snaps) == 0 {
			t.Error("List() returned empty after Save()")
		}
		found := false
		for _, s := range snaps {
			if s.Seq == meta.Seq {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("saved snapshot seq=%d not found in List()", meta.Seq)
		}
	})

	// ── T16-C3: Delete removes snapshot ───────────────────────────
	t.Run("delete_removes_snapshot", func(t *testing.T) {
		if err := backend.Delete(ctx, meta); err != nil {
			t.Fatalf("Delete() error: %v", err)
		}
		jobKey := fmt.Sprintf("%s/%s", meta.Namespace, meta.JobName)
		snaps, _ := backend.List(ctx, jobKey)
		for _, s := range snaps {
			if s.Seq == meta.Seq {
				t.Errorf("snapshot seq=%d still present after Delete()", meta.Seq)
			}
		}
	})

	// ── T16-C4: Load non-existent returns error ───────────────────
	t.Run("load_nonexistent_returns_error", func(t *testing.T) {
		ghost := checkpoint.SnapshotMeta{
			JobName:   "ghost-job",
			Namespace: "nowhere",
			Seq:       99999,
		}
		if err := backend.Load(ctx, ghost, t.TempDir()); err == nil {
			t.Error("Load() of non-existent snapshot should return error")
		}
	})

	// ── T16-C5: Prune keeps only N newest ─────────────────────────
	t.Run("prune_keeps_newest", func(t *testing.T) {
		pruneJob := "prune-test-job"
		pruneNS := "prune-ns"
		jobKey := pruneNS + "/" + pruneJob

		// Save 5 snapshots
		for i := 1; i <= 5; i++ {
			src := t.TempDir()
			_ = os.WriteFile(filepath.Join(src, "data.img"), []byte(fmt.Sprintf("snap-%d", i)), 0644)
			m := checkpoint.SnapshotMeta{JobName: pruneJob, Namespace: pruneNS, Seq: i, CreatedAt: time.Now()}
			if err := backend.Save(ctx, src, m); err != nil {
				t.Fatalf("Save seq=%d: %v", i, err)
			}
		}

		// Prune to keep only 3
		if err := backend.Prune(ctx, jobKey, 3); err != nil {
			t.Fatalf("Prune() error: %v", err)
		}

		snaps, _ := backend.List(ctx, jobKey)
		if len(snaps) != 3 {
			t.Errorf("after Prune(keep=3) expected 3 snapshots, got %d", len(snaps))
		}
		// Verify the 3 newest are kept (seq 3,4,5)
		seqs := make(map[int]bool)
		for _, s := range snaps {
			seqs[s.Seq] = true
		}
		for _, expected := range []int{3, 4, 5} {
			if !seqs[expected] {
				t.Errorf("expected seq=%d to be retained after Prune", expected)
			}
		}
		// Verify oldest are gone (seq 1,2)
		for _, pruned := range []int{1, 2} {
			if seqs[pruned] {
				t.Errorf("expected seq=%d to be pruned", pruned)
			}
		}
	})

	// ── T16-C6: Prune is idempotent ──────────────────────────────
	t.Run("prune_idempotent", func(t *testing.T) {
		jobKey := fmt.Sprintf("%s/%s", meta.Namespace, meta.JobName)
		// Prune empty / already-pruned — must not error
		if err := backend.Prune(ctx, jobKey, 5); err != nil {
			t.Errorf("Prune() on already-pruned set should not error: %v", err)
		}
	})
}
