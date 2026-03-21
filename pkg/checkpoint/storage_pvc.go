//go:build checkpointfull
// +build checkpointfull

// Package checkpoint — LocalPVC StorageBackend.
//
// Stores snapshots on a mounted PersistentVolumeClaim.
// Directory layout:
//
//	<root>/<namespace>/<jobName>/ckpt-<seq:05d>/
//	    meta.json        — SnapshotMeta serialized as JSON
//	    *.img            — CRIU checkpoint files (copied verbatim)
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.uber.org/zap"
)

// LocalPVCBackend implements StorageBackend using a local filesystem (PVC mount).
type LocalPVCBackend struct {
	root   string // e.g. /mnt/phoenix-snapshots
	logger *zap.Logger
}

// NewLocalPVCBackend creates a backend rooted at the given directory.
func NewLocalPVCBackend(root string, logger *zap.Logger) (*LocalPVCBackend, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("create snapshot root %s: %w", root, err)
	}
	return &LocalPVCBackend{root: root, logger: logger}, nil
}

// ── Save ──────────────────────────────────────────────────────────

func (b *LocalPVCBackend) Save(ctx context.Context, src string, meta SnapshotMeta) error {
	dst := b.snapDir(meta)
	if err := os.MkdirAll(dst, 0755); err != nil {
		return fmt.Errorf("pvc save mkdir %s: %w", dst, err)
	}

	// Copy every file from src to dst
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("pvc save readdir %s: %w", src, err)
	}

	var totalBytes int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue // CRIU only produces flat files
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("pvc save cancelled: %w", ctx.Err())
		default:
		}

		n, err := copyFile(
			filepath.Join(src, entry.Name()),
			filepath.Join(dst, entry.Name()),
		)
		if err != nil {
			return fmt.Errorf("pvc save copy %s: %w", entry.Name(), err)
		}
		totalBytes += n
	}

	meta.SizeBytes = totalBytes
	if err := b.writeMeta(dst, meta); err != nil {
		return fmt.Errorf("pvc save meta: %w", err)
	}

	b.logger.Info("pvc snapshot saved",
		zap.String("job", meta.JobKey()),
		zap.Int("seq", meta.Seq),
		zap.String("dir", dst),
		zap.Int64("bytes", totalBytes))
	return nil
}

// ── Load ──────────────────────────────────────────────────────────

func (b *LocalPVCBackend) Load(ctx context.Context, meta SnapshotMeta, dst string) error {
	src := b.snapDir(meta)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return fmt.Errorf("snapshot not found: %s seq=%d", meta.JobKey(), meta.Seq)
	}

	if err := os.MkdirAll(dst, 0755); err != nil {
		return fmt.Errorf("pvc load mkdir %s: %w", dst, err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("pvc load readdir %s: %w", src, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "meta.json" {
			continue
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("pvc load cancelled: %w", ctx.Err())
		default:
		}
		if _, err := copyFile(
			filepath.Join(src, entry.Name()),
			filepath.Join(dst, entry.Name()),
		); err != nil {
			return fmt.Errorf("pvc load copy %s: %w", entry.Name(), err)
		}
	}

	b.logger.Info("pvc snapshot loaded",
		zap.String("job", meta.JobKey()),
		zap.Int("seq", meta.Seq),
		zap.String("dst", dst))
	return nil
}

// ── List ──────────────────────────────────────────────────────────

func (b *LocalPVCBackend) List(_ context.Context, jobKey string) ([]SnapshotMeta, error) {
	parts := strings.SplitN(jobKey, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid jobKey %q: expected namespace/name", jobKey)
	}
	jobDir := filepath.Join(b.root, parts[0], parts[1])

	entries, err := os.ReadDir(jobDir)
	if os.IsNotExist(err) {
		return nil, nil // no snapshots yet — not an error
	}
	if err != nil {
		return nil, fmt.Errorf("list readdir %s: %w", jobDir, err)
	}

	var metas []SnapshotMeta
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "ckpt-") {
			continue
		}
		m, err := b.readMeta(filepath.Join(jobDir, e.Name()))
		if err != nil {
			b.logger.Warn("skip corrupt snapshot dir",
				zap.String("dir", e.Name()), zap.Error(err))
			continue
		}
		metas = append(metas, m)
	}

	// Sort ascending by Seq
	sort.Slice(metas, func(i, j int) bool { return metas[i].Seq < metas[j].Seq })
	return metas, nil
}

// ── Delete ────────────────────────────────────────────────────────

func (b *LocalPVCBackend) Delete(_ context.Context, meta SnapshotMeta) error {
	dir := b.snapDir(meta)
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("pvc delete %s: %w", dir, err)
	}
	b.logger.Info("pvc snapshot deleted",
		zap.String("job", meta.JobKey()), zap.Int("seq", meta.Seq))
	return nil
}

// ── Prune ─────────────────────────────────────────────────────────

func (b *LocalPVCBackend) Prune(ctx context.Context, jobKey string, keep int) error {
	metas, err := b.List(ctx, jobKey)
	if err != nil {
		return fmt.Errorf("prune list: %w", err)
	}
	if len(metas) <= keep {
		return nil // nothing to prune
	}

	// metas is sorted oldest→newest; delete the head (oldest)
	toDelete := metas[:len(metas)-keep]
	for _, m := range toDelete {
		if err := b.Delete(ctx, m); err != nil {
			return fmt.Errorf("prune delete seq=%d: %w", m.Seq, err)
		}
	}
	b.logger.Info("pvc prune complete",
		zap.String("job", jobKey),
		zap.Int("deleted", len(toDelete)),
		zap.Int("retained", keep))
	return nil
}

// ── Internal helpers ──────────────────────────────────────────────

func (b *LocalPVCBackend) snapDir(meta SnapshotMeta) string {
	return filepath.Join(b.root, meta.Namespace, meta.JobName,
		fmt.Sprintf("ckpt-%05d", meta.Seq))
}

func (b *LocalPVCBackend) writeMeta(dir string, meta SnapshotMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "meta.json"), data, 0644)
}

func (b *LocalPVCBackend) readMeta(dir string) (SnapshotMeta, error) {
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return SnapshotMeta{}, err
	}
	var m SnapshotMeta
	return m, json.Unmarshal(data, &m)
}

// copyFile copies src → dst, returns bytes written.
// Uses io.Copy for streaming (no full-file buffering).
func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("open src %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return 0, fmt.Errorf("open dst %s: %w", dst, err)
	}
	defer out.Close()

	n, err := io.Copy(out, in)
	if err != nil {
		return n, fmt.Errorf("copy %s→%s: %w", src, dst, err)
	}
	if err := out.Sync(); err != nil {
		return n, fmt.Errorf("fsync %s: %w", dst, err)
	}
	return n, nil
}
