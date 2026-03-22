//go:build migrationfull
// +build migrationfull

// Sprint 7 TDD Red — migration E2E + K8s exec contract tests.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package migration_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wlqtjl/PhoenixGPU/pkg/migration"
)

// ── T57: K8s exec interface contract ─────────────────────────────

type fakeExec struct {
	calls []migration.ExecCall
	errOn string // return error if command contains this string
}

func (f *fakeExec) ExecInPod(ctx context.Context, call migration.ExecCall) error {
	f.calls = append(f.calls, call)
	if f.errOn != "" && strings.Contains(call.Command, f.errOn) {
		return errors.New("injected exec error: " + f.errOn)
	}
	return nil
}

func TestK8sExec_AllStagesCallCorrectCommands(t *testing.T) {
	exec := &fakeExec{}
	plan := migration.Plan{
		JobNamespace: "research",
		JobName:      "llm-pretrain",
		SourceNode:   "gpu-node-01",
		TargetNode:   "gpu-node-02",
		SnapshotDir:  "/tmp/test-snap",
	}
	plan.SetDefaults()

	executor := migration.NewRealExecutorWithExec(exec, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Will fail at execOnNode (not implemented yet) — but exec calls must be recorded
	_, _ = executor.Execute(ctx, plan)

	// Verify stage commands were issued in correct order
	commands := make([]string, len(exec.calls))
	for i, c := range exec.calls {
		commands[i] = c.Stage
	}

	expectedOrder := []string{"pre-dump", "dump", "transfer", "restore"}
	for i, want := range expectedOrder {
		if i >= len(commands) {
			break
		}
		if commands[i] != want {
			t.Errorf("stage[%d] = %q, want %q", i, commands[i], want)
		}
	}
}

func TestK8sExec_SourceNodeUsedForDump(t *testing.T) {
	exec := &fakeExec{}
	plan := migration.Plan{
		JobNamespace: "ns", JobName: "job",
		SourceNode: "src-node", TargetNode: "tgt-node",
	}
	plan.SetDefaults()

	executor := migration.NewRealExecutorWithExec(exec, nil)
	ctx := context.Background()
	_, _ = executor.Execute(ctx, plan)

	for _, call := range exec.calls {
		if call.Stage == "dump" && call.NodeName != "src-node" {
			t.Errorf("dump must execute on source node, got %q", call.NodeName)
		}
		if call.Stage == "restore" && call.NodeName != "tgt-node" {
			t.Errorf("restore must execute on target node, got %q", call.NodeName)
		}
	}
}

func TestK8sExec_UnfreezesSourceOnRestoreFailure(t *testing.T) {
	exec := &fakeExec{errOn: "criu restore"} // restore fails
	plan := migration.Plan{
		JobNamespace: "ns", JobName: "job",
		SourceNode: "src-node", TargetNode: "tgt-node",
	}
	plan.SetDefaults()

	executor := migration.NewRealExecutorWithExec(exec, nil)
	ctx := context.Background()
	result, err := executor.Execute(ctx, plan)

	if err == nil {
		t.Error("expected error when restore fails")
	}
	if result.State != migration.StateFailed {
		t.Errorf("state = %s, want Failed", result.State)
	}

	// Verify unfreeze was called on source
	unfreezeCalled := false
	for _, c := range exec.calls {
		if c.Stage == "unfreeze" && c.NodeName == "src-node" {
			unfreezeCalled = true
		}
	}
	if !unfreezeCalled {
		t.Error("source node must be unfrozen when restore fails")
	}
}

// ── T58: Full E2E mock migration ─────────────────────────────────

func TestE2E_MockMigration_AllStagesComplete(t *testing.T) {
	executor := migration.NewMockExecutor()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	plan := migration.Plan{
		JobNamespace: "research",
		JobName:      "test-job",
		SourceNode:   "gpu-node-01",
		TargetNode:   "gpu-node-02",
	}

	result, err := executor.Execute(ctx, plan)
	if err != nil {
		t.Fatalf("mock migration failed: %v", err)
	}
	if result.State != migration.StateDone {
		t.Errorf("state = %s, want Done", result.State)
	}

	// All stages must have recorded durations
	for _, stage := range []migration.State{
		migration.StatePreDumping,
		migration.StateDumping,
		migration.StateTransferring,
		migration.StateRestoring,
	} {
		if _, ok := result.StageDurations[stage]; !ok {
			t.Errorf("missing duration for stage %s", stage)
		}
	}
}

func TestE2E_MockMigration_FreezeWindowRecorded(t *testing.T) {
	executor := migration.NewMockExecutor()
	plan := migration.Plan{
		JobNamespace: "ns", JobName: "j",
		SourceNode: "n1", TargetNode: "n2",
	}

	result, _ := executor.Execute(context.Background(), plan)
	if result.FreezeWindow == 0 {
		t.Error("freeze window must be recorded")
	}
}

func TestE2E_CancelledContext_FailsGracefully(t *testing.T) {
	executor := migration.NewMockExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	plan := migration.Plan{
		JobNamespace: "ns", JobName: "j",
		SourceNode: "n1", TargetNode: "n2",
	}

	result, err := executor.Execute(ctx, plan)
	if err == nil {
		t.Error("expected error with cancelled context")
	}
	if result.State != migration.StateFailed {
		t.Errorf("state = %s, want Failed on cancellation", result.State)
	}
}

// ── T59: Migration API handler contract ──────────────────────────

func TestMigrationRequest_Validates(t *testing.T) {
	valid := migration.MigrateRequest{
		JobNamespace: "research",
		JobName:      "llm-pretrain",
		TargetNode:   "gpu-node-02",
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid request rejected: %v", err)
	}

	invalid := migration.MigrateRequest{
		JobNamespace: "research",
		// missing JobName
		TargetNode: "gpu-node-02",
	}
	if err := invalid.Validate(); err == nil {
		t.Error("invalid request should be rejected")
	}
}
