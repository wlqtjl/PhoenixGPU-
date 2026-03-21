// Package checkpoint — StorageBackend interface and SnapshotMeta types.
//
// Design principles (Engineering Covenant v0.2):
//   - Interface is the contract; implementations are interchangeable
//   - Context on every method: callers control timeouts
//   - No panic, all errors wrapped with context
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package checkpoint

import (
	"context"
	"time"
)

// SnapshotMeta is the identifier and metadata for a single checkpoint snapshot.
// It is stored alongside the snapshot data and used for indexing, pruning,
// and Prometheus labelling.
type SnapshotMeta struct {
	// Identity
	Namespace string
	JobName   string
	Seq       int // monotonically increasing sequence number within a job

	// Provenance
	NodeName string
	PodName  string
	GPUModel string

	// Timing
	CreatedAt  time.Time
	DurationMS int64 // how long the checkpoint took

	// Size (filled by backend after Save)
	SizeBytes int64
}

// JobKey returns the canonical key used to group snapshots for a job.
func (m SnapshotMeta) JobKey() string {
	return m.Namespace + "/" + m.JobName
}

// StorageBackend abstracts where checkpoint snapshots are stored.
// Implementations: LocalPVC, S3.
// All methods must be safe for concurrent use.
// All methods must be idempotent where specified.
type StorageBackend interface {
	// Save uploads all files in the local directory src to the backend,
	// associated with the given meta. Idempotent: calling Save twice with
	// the same meta overwrites the previous snapshot.
	Save(ctx context.Context, src string, meta SnapshotMeta) error

	// Load downloads the snapshot identified by meta into the local directory dst.
	// Returns an error if the snapshot does not exist.
	Load(ctx context.Context, meta SnapshotMeta, dst string) error

	// List returns all snapshots for the given jobKey (namespace/name),
	// ordered from oldest to newest (ascending Seq).
	List(ctx context.Context, jobKey string) ([]SnapshotMeta, error)

	// Delete removes the snapshot identified by meta.
	// Idempotent: deleting a non-existent snapshot must not return an error.
	Delete(ctx context.Context, meta SnapshotMeta) error

	// Prune deletes snapshots for jobKey, retaining only the `keep` newest.
	// If fewer than `keep` snapshots exist, Prune is a no-op.
	// Idempotent.
	Prune(ctx context.Context, jobKey string, keep int) error
}

// UploadTask is sent from the CRIU Watcher to the Worker Pool.
type UploadTask struct {
	SourceDir string
	Meta      SnapshotMeta
	Attempt   int // retry counter
}
