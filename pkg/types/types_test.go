// Unit tests for pkg/types — ErrNotFound, IsNotFound, and FakeK8sClient.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package types

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// ── ErrNotFound / IsNotFound ──────────────────────────────────────

func TestIsNotFound_DirectMatch(t *testing.T) {
	if !IsNotFound(ErrNotFound) {
		t.Error("IsNotFound(ErrNotFound) should be true")
	}
}

func TestIsNotFound_WrappedError(t *testing.T) {
	wrapped := fmt.Errorf("job lookup: %w", ErrNotFound)
	if !IsNotFound(wrapped) {
		t.Error("IsNotFound should match wrapped ErrNotFound")
	}
}

func TestIsNotFound_DoubleWrappedError(t *testing.T) {
	inner := fmt.Errorf("inner: %w", ErrNotFound)
	outer := fmt.Errorf("outer: %w", inner)
	if !IsNotFound(outer) {
		t.Error("IsNotFound should match doubly wrapped ErrNotFound")
	}
}

func TestIsNotFound_UnrelatedError(t *testing.T) {
	err := errors.New("some other error")
	if IsNotFound(err) {
		t.Error("IsNotFound should return false for unrelated errors")
	}
}

func TestIsNotFound_NilError(t *testing.T) {
	if IsNotFound(nil) {
		t.Error("IsNotFound(nil) should be false")
	}
}

// ── FakeK8sClient ─────────────────────────────────────────────────

func TestFakeK8sClient_ImplementsInterface(t *testing.T) {
	var _ K8sClientInterface = NewFakeK8sClient()
}

func TestFakeK8sClient_GetClusterSummary(t *testing.T) {
	c := NewFakeK8sClient()
	s, err := c.GetClusterSummary(context.Background())
	if err != nil {
		t.Fatalf("GetClusterSummary error: %v", err)
	}
	if s.TotalGPUs <= 0 {
		t.Error("expected TotalGPUs > 0")
	}
	if s.ActiveJobs <= 0 {
		t.Error("expected ActiveJobs > 0")
	}
}

func TestFakeK8sClient_ListGPUNodes_NonEmpty(t *testing.T) {
	c := NewFakeK8sClient()
	nodes, err := c.ListGPUNodes(context.Background())
	if err != nil {
		t.Fatalf("ListGPUNodes error: %v", err)
	}
	if len(nodes) == 0 {
		t.Error("expected at least one GPU node")
	}
	for _, n := range nodes {
		if n.Name == "" {
			t.Error("node name must not be empty")
		}
		if n.GPUCount <= 0 {
			t.Errorf("node %s should have GPUCount > 0", n.Name)
		}
	}
}

func TestFakeK8sClient_ListPhoenixJobs_AllNamespaces(t *testing.T) {
	c := NewFakeK8sClient()
	jobs, err := c.ListPhoenixJobs(context.Background(), "")
	if err != nil {
		t.Fatalf("ListPhoenixJobs error: %v", err)
	}
	if len(jobs) == 0 {
		t.Error("expected jobs in all-namespace query")
	}
}

func TestFakeK8sClient_ListPhoenixJobs_FilterByNamespace(t *testing.T) {
	c := NewFakeK8sClient()
	jobs, err := c.ListPhoenixJobs(context.Background(), "research")
	if err != nil {
		t.Fatalf("ListPhoenixJobs error: %v", err)
	}
	for _, j := range jobs {
		if j.Namespace != "research" {
			t.Errorf("job %s has namespace %q, want research", j.Name, j.Namespace)
		}
	}
}

func TestFakeK8sClient_ListPhoenixJobs_NonexistentNamespace(t *testing.T) {
	c := NewFakeK8sClient()
	jobs, err := c.ListPhoenixJobs(context.Background(), "nonexistent-ns")
	if err != nil {
		t.Fatalf("ListPhoenixJobs error: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs for nonexistent namespace, got %d", len(jobs))
	}
}

func TestFakeK8sClient_GetPhoenixJob_Found(t *testing.T) {
	c := NewFakeK8sClient()
	job, err := c.GetPhoenixJob(context.Background(), "research", "llm-pretrain-v3")
	if err != nil {
		t.Fatalf("GetPhoenixJob error: %v", err)
	}
	if job.Name != "llm-pretrain-v3" {
		t.Errorf("job name = %q, want llm-pretrain-v3", job.Name)
	}
}

func TestFakeK8sClient_GetPhoenixJob_NotFound(t *testing.T) {
	c := NewFakeK8sClient()
	_, err := c.GetPhoenixJob(context.Background(), "research", "no-such-job")
	if err == nil {
		t.Fatal("expected error for nonexistent job")
	}
	if !IsNotFound(err) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestFakeK8sClient_TriggerCheckpoint_ExistingJob(t *testing.T) {
	c := NewFakeK8sClient()
	err := c.TriggerCheckpoint(context.Background(), "research", "llm-pretrain-v3")
	if err != nil {
		t.Errorf("TriggerCheckpoint should succeed for existing job: %v", err)
	}
}

func TestFakeK8sClient_TriggerCheckpoint_NotFound(t *testing.T) {
	c := NewFakeK8sClient()
	err := c.TriggerCheckpoint(context.Background(), "ns", "no-job")
	if err == nil {
		t.Error("expected error for nonexistent job")
	}
	if !IsNotFound(err) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestFakeK8sClient_GetBillingByDepartment(t *testing.T) {
	c := NewFakeK8sClient()
	depts, err := c.GetBillingByDepartment(context.Background(), "monthly")
	if err != nil {
		t.Fatalf("GetBillingByDepartment error: %v", err)
	}
	if len(depts) == 0 {
		t.Error("expected billing departments")
	}
	for _, d := range depts {
		if d.Department == "" {
			t.Error("department name should not be empty")
		}
		if d.GPUHours < 0 {
			t.Errorf("department %s has negative GPUHours", d.Department)
		}
	}
}

func TestFakeK8sClient_GetBillingRecords_All(t *testing.T) {
	c := NewFakeK8sClient()
	records, err := c.GetBillingRecords(context.Background(), "")
	if err != nil {
		t.Fatalf("GetBillingRecords error: %v", err)
	}
	if len(records) == 0 {
		t.Error("expected billing records")
	}
}

func TestFakeK8sClient_GetBillingRecords_FilterByDepartment(t *testing.T) {
	c := NewFakeK8sClient()
	all, _ := c.GetBillingRecords(context.Background(), "")
	filtered, err := c.GetBillingRecords(context.Background(), "算法研究院")
	if err != nil {
		t.Fatalf("GetBillingRecords error: %v", err)
	}
	if len(filtered) >= len(all) && len(all) > 1 {
		t.Error("filtered records should be fewer than all records")
	}
	for _, r := range filtered {
		if r.Department != "算法研究院" {
			t.Errorf("record department = %q, want 算法研究院", r.Department)
		}
	}
}

func TestFakeK8sClient_ListAlerts(t *testing.T) {
	c := NewFakeK8sClient()
	alerts, err := c.ListAlerts(context.Background())
	if err != nil {
		t.Fatalf("ListAlerts error: %v", err)
	}
	if len(alerts) == 0 {
		t.Error("expected fake alerts")
	}
}

func TestFakeK8sClient_ResolveAlert_Idempotent(t *testing.T) {
	c := NewFakeK8sClient()

	// Resolving existing alert should succeed
	if err := c.ResolveAlert(context.Background(), "alert-1"); err != nil {
		t.Fatalf("ResolveAlert error: %v", err)
	}

	// Resolving again (idempotent)
	if err := c.ResolveAlert(context.Background(), "alert-1"); err != nil {
		t.Fatalf("second ResolveAlert error: %v", err)
	}

	// Resolving unknown alert should also not error
	if err := c.ResolveAlert(context.Background(), "nonexistent"); err != nil {
		t.Fatalf("ResolveAlert unknown: %v", err)
	}
}

func TestFakeK8sClient_ResolveAlert_MarksResolved(t *testing.T) {
	c := NewFakeK8sClient()
	_ = c.ResolveAlert(context.Background(), "alert-1")

	alerts, _ := c.ListAlerts(context.Background())
	for _, a := range alerts {
		if a.ID == "alert-1" && !a.Resolved {
			t.Error("alert-1 should be marked as resolved")
		}
	}
}

func TestFakeK8sClient_GetUtilizationHistory(t *testing.T) {
	c := NewFakeK8sClient()
	pts, err := c.GetUtilizationHistory(context.Background(), 6)
	if err != nil {
		t.Fatalf("GetUtilizationHistory error: %v", err)
	}
	// 6 hours * 2 points per hour = 12 points
	if len(pts) != 12 {
		t.Errorf("expected 12 points, got %d", len(pts))
	}
	for _, p := range pts {
		if p.TS.IsZero() {
			t.Error("time series point has zero timestamp")
		}
	}
}
