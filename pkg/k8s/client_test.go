//go:build k8sfull
// +build k8sfull

// Real K8s client tests — TDD Red phase.
// Tests define the contract before implementation.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package k8s_test

import (
	"context"
	"testing"
	"time"

	"github.com/wlqtjl/PhoenixGPU/pkg/k8s"
)

// ── T48-1: Cache contract ─────────────────────────────────────────

func TestMetricsCache_ReturnsCachedValue(t *testing.T) {
	callCount := 0
	fetcher := func(_ context.Context) (float64, error) {
		callCount++
		return 74.5, nil
	}

	cache := k8s.NewMetricsCache(fetcher, 15*time.Second)
	ctx := context.Background()

	v1, _ := cache.Get(ctx)
	v2, _ := cache.Get(ctx)

	if callCount != 1 {
		t.Errorf("expected 1 fetcher call, got %d (cache not working)", callCount)
	}
	if v1 != v2 {
		t.Errorf("cached values differ: %f vs %f", v1, v2)
	}
}

func TestMetricsCache_RefetchesAfterTTL(t *testing.T) {
	callCount := 0
	fetcher := func(_ context.Context) (float64, error) {
		callCount++
		return float64(callCount) * 10, nil
	}

	cache := k8s.NewMetricsCache(fetcher, 50*time.Millisecond)
	ctx := context.Background()

	_, _ = cache.Get(ctx) // call 1
	time.Sleep(80 * time.Millisecond)
	_, _ = cache.Get(ctx) // call 2 (TTL expired)

	if callCount != 2 {
		t.Errorf("expected 2 fetcher calls after TTL, got %d", callCount)
	}
}

func TestMetricsCache_GracefulDegradation_OnFetchError(t *testing.T) {
	// If fetch fails, cache must return last known value (not error)
	good := true
	fetcher := func(_ context.Context) (float64, error) {
		if good {
			return 65.0, nil
		}
		return 0, context.DeadlineExceeded
	}

	cache := k8s.NewMetricsCache(fetcher, 50*time.Millisecond)
	ctx := context.Background()

	_, _ = cache.Get(ctx) // seed with 65.0
	good = false
	time.Sleep(80 * time.Millisecond)

	v, err := cache.Get(ctx) // TTL expired, fetch fails
	if err != nil {
		t.Errorf("graceful degradation: should return last known value, got error: %v", err)
	}
	if v != 65.0 {
		t.Errorf("expected last known value 65.0, got %f", v)
	}
}

// ── T48-2: NodeEnricher contract ─────────────────────────────────

func TestNodeEnricher_MergesK8sAndMetrics(t *testing.T) {
	// Node from K8s API + metrics from DCGM must be merged correctly
	node := k8s.RawNode{
		Name:     "gpu-node-01",
		GPUModel: "NVIDIA A100 80GB",
		GPUCount: 8,
		Ready:    true,
	}
	metrics := k8s.NodeMetrics{
		SMUtilPct:   82.5,
		VRAMUsedMiB: 61440,
		TempCelsius: 72.0,
		PowerWatt:   380.0,
	}

	enriched := k8s.EnrichNode(node, metrics, 81920)

	if enriched.Name != "gpu-node-01" {
		t.Errorf("name mismatch: %s", enriched.Name)
	}
	if enriched.SMUtilPct != 82.5 {
		t.Errorf("SMUtilPct mismatch: %f", enriched.SMUtilPct)
	}
	if enriched.VRAMTotalMiB != 81920 {
		t.Errorf("VRAMTotalMiB mismatch: %d", enriched.VRAMTotalMiB)
	}
	if enriched.Faulted {
		t.Error("ready node should not be marked faulted")
	}
}

func TestNodeEnricher_MarksFaultedWhenNotReady(t *testing.T) {
	node := k8s.RawNode{
		Name:  "gpu-node-03",
		Ready: false,
	}
	enriched := k8s.EnrichNode(node, k8s.NodeMetrics{}, 0)
	if !enriched.Faulted {
		t.Error("not-ready node should be marked faulted")
	}
}

// ── T48-3: ClusterSummary aggregation ────────────────────────────

func TestBuildClusterSummary_CountsCorrectly(t *testing.T) {
	jobs := []k8s.PhoenixJobStatus{
		{Phase: "Running"},
		{Phase: "Running"},
		{Phase: "Checkpointing"},
		{Phase: "Restoring"},
		{Phase: "Failed"},
	}
	nodes := []k8s.RawNode{
		{GPUCount: 8},
		{GPUCount: 8},
		{GPUCount: 4},
	}

	summary := k8s.BuildClusterSummary(jobs, nodes, 74.2, 3)

	if summary.TotalGPUs != 20 {
		t.Errorf("TotalGPUs: got %d, want 20", summary.TotalGPUs)
	}
	if summary.ActiveJobs != 2 {
		t.Errorf("ActiveJobs: got %d, want 2", summary.ActiveJobs)
	}
	if summary.CheckpointingJobs != 1 {
		t.Errorf("CheckpointingJobs: got %d, want 1", summary.CheckpointingJobs)
	}
	if summary.RestoringJobs != 1 {
		t.Errorf("RestoringJobs: got %d, want 1", summary.RestoringJobs)
	}
	if summary.AlertCount != 3 {
		t.Errorf("AlertCount: got %d, want 3", summary.AlertCount)
	}
}

// ── T48-4: P99 response time budget (unit proxy) ─────────────────

func TestFakeClientRespondsWithin100ms(t *testing.T) {
	// Fake client must be fast enough for benchmarks
	// (Real client measured via Prometheus histogram in integration)
	fake := k8s.NewFakeClient()
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 100; i++ {
		_, _ = fake.GetClusterSummary(ctx)
	}
	elapsed := time.Since(start)
	if elapsed > 100*time.Millisecond {
		t.Errorf("100 GetClusterSummary calls took %s (>100ms) — fake client too slow", elapsed)
	}
}
