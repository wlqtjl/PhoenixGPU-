// Unit tests for FakeK8sClient, utility functions, and domain types.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package internal

import (
	"context"
	"testing"
)

// ── parsePositiveInt ──────────────────────────────────────────────

func TestParsePositiveInt_Valid(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"1", 1},
		{"42", 42},
		{"  100  ", 100},
	}
	for _, tc := range cases {
		got, err := parsePositiveInt(tc.input)
		if err != nil {
			t.Errorf("parsePositiveInt(%q) error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("parsePositiveInt(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestParsePositiveInt_Invalid(t *testing.T) {
	cases := []string{"0", "-1", "abc", "", "3.14"}
	for _, input := range cases {
		_, err := parsePositiveInt(input)
		if err == nil {
			t.Errorf("parsePositiveInt(%q) should return error", input)
		}
	}
}

// ── isNotFound ────────────────────────────────────────────────────

func TestIsNotFound(t *testing.T) {
	if !isNotFound(ErrNotFound) {
		t.Error("isNotFound(ErrNotFound) should return true")
	}
	if isNotFound(nil) {
		t.Error("isNotFound(nil) should return false")
	}
}

// ── FakeK8sClient ─────────────────────────────────────────────────

func TestFakeK8sClient_GetClusterSummary(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	s, err := c.GetClusterSummary(ctx)
	if err != nil {
		t.Fatalf("GetClusterSummary: %v", err)
	}
	if s.TotalGPUs == 0 {
		t.Error("expected non-zero TotalGPUs")
	}
	if s.ActiveJobs == 0 {
		t.Error("expected non-zero ActiveJobs")
	}
	if s.AvgUtilPct <= 0 {
		t.Error("expected positive AvgUtilPct")
	}
}

func TestFakeK8sClient_GetUtilizationHistory(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	pts, err := c.GetUtilizationHistory(ctx, 6)
	if err != nil {
		t.Fatalf("GetUtilizationHistory: %v", err)
	}
	// 6 hours * 2 points per hour = 12 points
	if len(pts) != 12 {
		t.Errorf("expected 12 points, got %d", len(pts))
	}
}

func TestFakeK8sClient_ListGPUNodes(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	nodes, err := c.ListGPUNodes(ctx)
	if err != nil {
		t.Fatalf("ListGPUNodes: %v", err)
	}
	if len(nodes) != 4 {
		t.Errorf("expected 4 nodes, got %d", len(nodes))
	}
	for _, n := range nodes {
		if n.Name == "" {
			t.Error("node Name should not be empty")
		}
		if n.GPUCount == 0 {
			t.Errorf("node %s GPUCount should be > 0", n.Name)
		}
	}
}

func TestFakeK8sClient_ListPhoenixJobs_All(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	jobs, err := c.ListPhoenixJobs(ctx, "")
	if err != nil {
		t.Fatalf("ListPhoenixJobs: %v", err)
	}
	if len(jobs) != 3 {
		t.Errorf("expected 3 jobs, got %d", len(jobs))
	}
}

func TestFakeK8sClient_ListPhoenixJobs_FilterNamespace(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	jobs, err := c.ListPhoenixJobs(ctx, "research")
	if err != nil {
		t.Fatalf("ListPhoenixJobs: %v", err)
	}
	for _, j := range jobs {
		if j.Namespace != "research" {
			t.Errorf("expected namespace=research, got %s", j.Namespace)
		}
	}
}

func TestFakeK8sClient_ListPhoenixJobs_NonexistentNamespace(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	jobs, err := c.ListPhoenixJobs(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("ListPhoenixJobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs for nonexistent namespace, got %d", len(jobs))
	}
}

func TestFakeK8sClient_GetPhoenixJob_Found(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	job, err := c.GetPhoenixJob(ctx, "research", "llm-pretrain-v3")
	if err != nil {
		t.Fatalf("GetPhoenixJob: %v", err)
	}
	if job.Name != "llm-pretrain-v3" {
		t.Errorf("expected name=llm-pretrain-v3, got %s", job.Name)
	}
	if job.Phase != "Running" {
		t.Errorf("expected phase=Running, got %s", job.Phase)
	}
}

func TestFakeK8sClient_GetPhoenixJob_NotFound(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	_, err := c.GetPhoenixJob(ctx, "ns", "no-such-job")
	if err == nil {
		t.Fatal("expected error for non-existent job")
	}
	if !isNotFound(err) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestFakeK8sClient_TriggerCheckpoint_Existing(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	err := c.TriggerCheckpoint(ctx, "research", "llm-pretrain-v3")
	if err != nil {
		t.Fatalf("TriggerCheckpoint: %v", err)
	}
}

func TestFakeK8sClient_TriggerCheckpoint_NotFound(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	err := c.TriggerCheckpoint(ctx, "ns", "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent job")
	}
	if !isNotFound(err) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestFakeK8sClient_GetBillingByDepartment(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	depts, err := c.GetBillingByDepartment(ctx, "monthly")
	if err != nil {
		t.Fatalf("GetBillingByDepartment: %v", err)
	}
	if len(depts) != 5 {
		t.Errorf("expected 5 departments, got %d", len(depts))
	}
	for _, d := range depts {
		if d.Department == "" {
			t.Error("department name should not be empty")
		}
		if d.GPUHours <= 0 {
			t.Errorf("department %s GPUHours should be positive", d.Department)
		}
	}
}

func TestFakeK8sClient_GetBillingRecords_All(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	records, err := c.GetBillingRecords(ctx, "")
	if err != nil {
		t.Fatalf("GetBillingRecords: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records, got %d", len(records))
	}
}

func TestFakeK8sClient_GetBillingRecords_FilterByDepartment(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	records, err := c.GetBillingRecords(ctx, "NLP平台组")
	if err != nil {
		t.Fatalf("GetBillingRecords: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record, got %d", len(records))
	}
	if records[0].Department != "NLP平台组" {
		t.Errorf("expected department NLP平台组, got %s", records[0].Department)
	}
}

func TestFakeK8sClient_ListAlerts(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	alerts, err := c.ListAlerts(ctx)
	if err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if len(alerts) != 3 {
		t.Errorf("expected 3 alerts, got %d", len(alerts))
	}
}

func TestFakeK8sClient_ResolveAlert_ExistingAlert(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	if err := c.ResolveAlert(ctx, "alert-1"); err != nil {
		t.Fatalf("ResolveAlert: %v", err)
	}
	// Verify it's resolved
	alerts, _ := c.ListAlerts(ctx)
	for _, a := range alerts {
		if a.ID == "alert-1" && !a.Resolved {
			t.Error("alert-1 should be resolved")
		}
	}
}

func TestFakeK8sClient_ResolveAlert_UnknownAlert_Idempotent(t *testing.T) {
	c := NewFakeK8sClient()
	ctx := context.Background()

	// Resolving unknown alert should not error (idempotent)
	if err := c.ResolveAlert(ctx, "nonexistent"); err != nil {
		t.Errorf("ResolveAlert on unknown alert should succeed: %v", err)
	}
}

// ── unavailableK8sClient ──────────────────────────────────────────

func TestUnavailableK8sClient_AllMethodsReturnError(t *testing.T) {
	c := unavailableK8sClient{}
	ctx := context.Background()

	if _, err := c.GetClusterSummary(ctx); err == nil {
		t.Error("expected error from unavailable client")
	}
	if _, err := c.GetUtilizationHistory(ctx, 24); err == nil {
		t.Error("expected error from unavailable client")
	}
	if _, err := c.ListGPUNodes(ctx); err == nil {
		t.Error("expected error from unavailable client")
	}
	if _, err := c.ListPhoenixJobs(ctx, ""); err == nil {
		t.Error("expected error from unavailable client")
	}
	if _, err := c.GetPhoenixJob(ctx, "ns", "name"); err == nil {
		t.Error("expected error from unavailable client")
	}
	if err := c.TriggerCheckpoint(ctx, "ns", "name"); err == nil {
		t.Error("expected error from unavailable client")
	}
	if _, err := c.GetBillingByDepartment(ctx, "monthly"); err == nil {
		t.Error("expected error from unavailable client")
	}
	if _, err := c.GetBillingRecords(ctx, ""); err == nil {
		t.Error("expected error from unavailable client")
	}
	if _, err := c.ListAlerts(ctx); err == nil {
		t.Error("expected error from unavailable client")
	}
	if err := c.ResolveAlert(ctx, "id"); err == nil {
		t.Error("expected error from unavailable client")
	}
}
