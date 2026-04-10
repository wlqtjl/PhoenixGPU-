// Unit tests for migration state machine, Plan validation, and EstimateFreezeWindow.
// These tests require the migrationfull build tag because the types and functions
// are defined in files guarded by that tag.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
//
//go:build migrationfull
// +build migrationfull

package migration

import (
	"testing"
	"time"
)

// ── CanTransition state machine ──────────────────────────────────

func TestCanTransition_ValidForwardPaths(t *testing.T) {
	validPaths := []struct {
		from, to State
	}{
		{StatePending, StatePreDumping},
		{StatePreDumping, StateDumping},
		{StatePreDumping, StateFailed},
		{StateDumping, StateTransferring},
		{StateDumping, StateFailed},
		{StateTransferring, StateRestoring},
		{StateTransferring, StateFailed},
		{StateRestoring, StateDone},
		{StateRestoring, StateFailed},
	}
	for _, p := range validPaths {
		if !CanTransition(p.from, p.to) {
			t.Errorf("CanTransition(%s, %s) = false, want true", p.from, p.to)
		}
	}
}

func TestCanTransition_InvalidPaths(t *testing.T) {
	invalidPaths := []struct {
		from, to State
	}{
		// Can't skip stages
		{StatePending, StateDumping},
		{StatePending, StateTransferring},
		{StatePending, StateRestoring},
		{StatePending, StateDone},
		{StatePreDumping, StateTransferring},
		{StateDumping, StateRestoring},
		{StateTransferring, StateDone},

		// Can't go backward
		{StateDumping, StatePreDumping},
		{StateTransferring, StateDumping},
		{StateRestoring, StateTransferring},
		{StateDone, StatePending},
		{StateDone, StatePreDumping},

		// Terminal states can't transition
		{StateDone, StateFailed},
		{StateFailed, StateDone},
		{StateFailed, StatePending},

		// Self-transitions not allowed
		{StatePending, StatePending},
		{StateDumping, StateDumping},
		{StateFailed, StateFailed},
	}
	for _, p := range invalidPaths {
		if CanTransition(p.from, p.to) {
			t.Errorf("CanTransition(%s, %s) = true, want false", p.from, p.to)
		}
	}
}

func TestCanTransition_UnknownState(t *testing.T) {
	if CanTransition(State("Unknown"), StatePending) {
		t.Error("unknown state should not transition anywhere")
	}
}

// ── Plan.Validate ────────────────────────────────────────────────

func TestPlanValidate_ValidPlan(t *testing.T) {
	p := Plan{
		JobNamespace: "research",
		JobName:      "llm-train",
		SourceNode:   "gpu-node-01",
		TargetNode:   "gpu-node-02",
	}
	if err := p.Validate(); err != nil {
		t.Errorf("valid plan rejected: %v", err)
	}
}

func TestPlanValidate_MissingJobNamespace(t *testing.T) {
	p := Plan{JobName: "job", SourceNode: "a", TargetNode: "b"}
	if err := p.Validate(); err == nil {
		t.Error("expected error for missing JobNamespace")
	}
}

func TestPlanValidate_MissingJobName(t *testing.T) {
	p := Plan{JobNamespace: "ns", SourceNode: "a", TargetNode: "b"}
	if err := p.Validate(); err == nil {
		t.Error("expected error for missing JobName")
	}
}

func TestPlanValidate_MissingSourceNode(t *testing.T) {
	p := Plan{JobNamespace: "ns", JobName: "j", TargetNode: "b"}
	if err := p.Validate(); err == nil {
		t.Error("expected error for missing SourceNode")
	}
}

func TestPlanValidate_MissingTargetNode(t *testing.T) {
	p := Plan{JobNamespace: "ns", JobName: "j", SourceNode: "a"}
	if err := p.Validate(); err == nil {
		t.Error("expected error for missing TargetNode")
	}
}

func TestPlanValidate_SameSourceAndTarget(t *testing.T) {
	p := Plan{
		JobNamespace: "ns",
		JobName:      "j",
		SourceNode:   "gpu-node-01",
		TargetNode:   "gpu-node-01",
	}
	if err := p.Validate(); err == nil {
		t.Error("expected error when SourceNode == TargetNode")
	}
}

// ── Plan.withDefaults ─────────────────────────────────────────────

func TestPlanWithDefaults_SetsSnapshotDir(t *testing.T) {
	p := Plan{JobName: "my-job"}
	p.withDefaults()
	if p.SnapshotDir != "/tmp/phoenix-migration/my-job" {
		t.Errorf("SnapshotDir = %q, want /tmp/phoenix-migration/my-job", p.SnapshotDir)
	}
}

func TestPlanWithDefaults_SetsTransferMethod(t *testing.T) {
	p := Plan{}
	p.withDefaults()
	if p.TransferMethod != "rsync" {
		t.Errorf("TransferMethod = %q, want rsync", p.TransferMethod)
	}
}

func TestPlanWithDefaults_SetsFreezeTimeout(t *testing.T) {
	p := Plan{}
	p.withDefaults()
	if p.FreezeTimeout != 10*time.Second {
		t.Errorf("FreezeTimeout = %v, want 10s", p.FreezeTimeout)
	}
}

func TestPlanWithDefaults_PreservesExistingValues(t *testing.T) {
	p := Plan{
		SnapshotDir:    "/custom/dir",
		TransferMethod: "s3",
		FreezeTimeout:  30 * time.Second,
	}
	p.withDefaults()
	if p.SnapshotDir != "/custom/dir" {
		t.Errorf("SnapshotDir should not be overwritten: %q", p.SnapshotDir)
	}
	if p.TransferMethod != "s3" {
		t.Errorf("TransferMethod should not be overwritten: %q", p.TransferMethod)
	}
	if p.FreezeTimeout != 30*time.Second {
		t.Errorf("FreezeTimeout should not be overwritten: %v", p.FreezeTimeout)
	}
}

// ── EstimateFreezeWindow ──────────────────────────────────────────

func TestEstimateFreezeWindow_A100_80GB(t *testing.T) {
	// 80GB = ~81920 MiB
	est := EstimateFreezeWindow(81920)
	// dirty = 81920 * 0.02 = 1638.4 MB, disk = 2000 MB/s → ~0.82 seconds
	if est <= 0 {
		t.Errorf("estimate must be positive, got %f", est)
	}
	if est > 5.0 {
		t.Errorf("estimate %f seems too high for 80GB A100 after pre-dump", est)
	}
}

func TestEstimateFreezeWindow_ZeroVRAM(t *testing.T) {
	est := EstimateFreezeWindow(0)
	if est != 0 {
		t.Errorf("estimate for 0 VRAM should be 0, got %f", est)
	}
}

func TestEstimateFreezeWindow_Monotonic(t *testing.T) {
	small := EstimateFreezeWindow(4096)
	large := EstimateFreezeWindow(81920)
	if large <= small {
		t.Errorf("larger VRAM (%f) should have longer freeze than smaller (%f)", large, small)
	}
}

// ── MockExecutor ──────────────────────────────────────────────────

func TestMockExecutor_RejectsInvalidPlan(t *testing.T) {
	exec := NewMockExecutor()
	_, err := exec.Execute(nil, Plan{}) // missing required fields
	if err == nil {
		t.Error("expected error for invalid plan")
	}
}
