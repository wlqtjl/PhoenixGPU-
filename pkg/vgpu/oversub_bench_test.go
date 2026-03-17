// Package vgpu — oversubscription performance benchmarks.
//
// Run: go test ./pkg/vgpu/... -bench=. -benchmem
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package vgpu_test

import (
	"fmt"
	"testing"

	"github.com/wlqtjl/PhoenixGPU/pkg/vgpu"
)

// BenchmarkLRUTouch measures the hot path cost of tracking kernel access.
// Must be < 100ns per Touch (called on every cuLaunchKernel).
func BenchmarkLRUTouch(b *testing.B) {
	lru := vgpu.NewLRUTracker(10000)
	addrs := make([]uint64, 1000)
	for i := range addrs {
		addrs[i] = uint64(i) * vgpu.PageSize
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			lru.Touch(addrs[i%len(addrs)])
			i++
		}
	})

	b.ReportMetric(float64(b.N)/float64(b.Elapsed().Seconds()), "Touch/sec")
}

// BenchmarkOversubAlloc measures the critical path for cuMemAlloc hook.
// Must be < 1µs per Alloc (cuMemAlloc itself takes ~10µs, so < 10% overhead).
func BenchmarkOversubAlloc(b *testing.B) {
	mgr := vgpu.NewOversubManager(8192*1024, 2.0) // 8TiB virtual cap for benchmark

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mgr.Alloc(1)
		mgr.Free(1)
	}
}

// BenchmarkConcurrentAllocFree measures thread safety overhead.
func BenchmarkConcurrentAllocFree(b *testing.B) {
	mgr := vgpu.NewOversubManager(8192*1024, 2.0)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = mgr.Alloc(1)
			mgr.Free(1)
		}
	})
}

// BenchmarkSwapDecision measures NeedsSwap — called before every large alloc.
func BenchmarkSwapDecision(b *testing.B) {
	mgr := vgpu.NewOversubManager(16384, 2.0) // 16GiB physical
	_ = mgr.Alloc(8192)                        // half full

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mgr.NeedsSwap(1024)
	}
}

// TestDensityReport prints a human-readable density analysis.
func TestDensityReport(t *testing.T) {
	cases := []struct {
		physical uint64
		ratio    float64
		tasks    int
		taskSize uint64
	}{
		{24576, 1.0, 1, 16384}, // baseline: 1 task, no oversub
		{24576, 2.0, 2, 16384}, // 2× oversub: 2 tasks share 24GiB
		{24576, 3.0, 3, 16384}, // 3× oversub: 3 tasks (aggressive)
		{80*1024, 2.0, 4, 40*1024}, // A100 80GB, 4 tasks @ 40GB each
	}

	for _, tc := range cases {
		mgr := vgpu.NewOversubManager(tc.physical, tc.ratio)
		placed := 0
		for i := 0; i < tc.tasks; i++ {
			if err := mgr.Alloc(tc.taskSize); err != nil {
				break
			}
			placed++
		}
		swapMiB := mgr.SwapUsed()
		t.Logf("Physical=%dGiB ratio=%.1f tasks=%d/%d taskSize=%dGiB swap=%dGiB",
			tc.physical/1024, tc.ratio,
			placed, tc.tasks,
			tc.taskSize/1024,
			swapMiB/1024)

		if placed < tc.tasks {
			t.Logf("  ↑ Only %d/%d tasks fit (expected with this config)", placed, tc.tasks)
		} else {
			t.Logf("  ↑ All %d tasks fit! Density improvement: %.1f×",
				tc.tasks, float64(tc.tasks)*float64(tc.taskSize)/float64(tc.physical))
		}
		_ = fmt.Sprintf("") // suppress import warning
	}
}
