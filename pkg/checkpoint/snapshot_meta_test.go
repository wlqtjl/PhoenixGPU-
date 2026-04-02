// Unit tests for SnapshotMeta and FaultEvent types.
// These types have no build tags and are available in default builds.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package checkpoint

import (
	"testing"
	"time"
)

// ── SnapshotMeta.JobKey ───────────────────────────────────────────

func TestSnapshotMeta_JobKey(t *testing.T) {
	cases := []struct {
		ns, job string
		want    string
	}{
		{"default", "train-job", "default/train-job"},
		{"research", "llm-pretrain-v3", "research/llm-pretrain-v3"},
		{"", "", "/"},
	}
	for _, tc := range cases {
		m := SnapshotMeta{Namespace: tc.ns, JobName: tc.job}
		if got := m.JobKey(); got != tc.want {
			t.Errorf("JobKey(%q,%q) = %q, want %q", tc.ns, tc.job, got, tc.want)
		}
	}
}

func TestSnapshotMeta_FieldDefaults(t *testing.T) {
	var m SnapshotMeta
	if m.Seq != 0 {
		t.Errorf("default Seq should be 0, got %d", m.Seq)
	}
	if m.DurationMS != 0 {
		t.Errorf("default DurationMS should be 0, got %d", m.DurationMS)
	}
	if m.SizeBytes != 0 {
		t.Errorf("default SizeBytes should be 0, got %d", m.SizeBytes)
	}
	if !m.CreatedAt.IsZero() {
		t.Error("default CreatedAt should be zero time")
	}
}

func TestSnapshotMeta_FullyPopulated(t *testing.T) {
	now := time.Now()
	m := SnapshotMeta{
		Namespace:  "research",
		JobName:    "llm-pretrain",
		Seq:        42,
		NodeName:   "gpu-node-01",
		PodName:    "llm-pretrain-abc123",
		GPUModel:   "NVIDIA A100 80GB",
		CreatedAt:  now,
		DurationMS: 8200,
		SizeBytes:  2_800_000_000,
	}

	if m.JobKey() != "research/llm-pretrain" {
		t.Errorf("unexpected JobKey: %s", m.JobKey())
	}
	if m.Seq != 42 {
		t.Errorf("Seq = %d, want 42", m.Seq)
	}
	if m.DurationMS != 8200 {
		t.Errorf("DurationMS = %d, want 8200", m.DurationMS)
	}
	if m.SizeBytes != 2_800_000_000 {
		t.Errorf("SizeBytes = %d, want 2800000000", m.SizeBytes)
	}
}

// ── UploadTask ────────────────────────────────────────────────────

func TestUploadTask_DefaultAttempt(t *testing.T) {
	task := UploadTask{
		SourceDir: "/tmp/snapshot",
		Meta:      SnapshotMeta{Namespace: "ns", JobName: "job", Seq: 1},
	}
	if task.Attempt != 0 {
		t.Errorf("default Attempt should be 0, got %d", task.Attempt)
	}
}

func TestUploadTask_RetryIncrement(t *testing.T) {
	task := UploadTask{
		SourceDir: "/tmp/snapshot",
		Meta:      SnapshotMeta{Namespace: "ns", JobName: "job", Seq: 5},
		Attempt:   3,
	}
	if task.Attempt != 3 {
		t.Errorf("Attempt = %d, want 3", task.Attempt)
	}
}

// ── FaultEvent ────────────────────────────────────────────────────

func TestFaultEvent_Fields(t *testing.T) {
	now := time.Now()
	e := FaultEvent{
		NodeName:   "gpu-node-01",
		DetectedAt: now,
	}
	if e.NodeName != "gpu-node-01" {
		t.Errorf("NodeName = %q, want gpu-node-01", e.NodeName)
	}
	if e.DetectedAt != now {
		t.Errorf("DetectedAt mismatch")
	}
}

func TestFaultEvent_ZeroValue(t *testing.T) {
	var e FaultEvent
	if e.NodeName != "" {
		t.Errorf("default NodeName should be empty, got %q", e.NodeName)
	}
	if !e.DetectedAt.IsZero() {
		t.Error("default DetectedAt should be zero time")
	}
}
