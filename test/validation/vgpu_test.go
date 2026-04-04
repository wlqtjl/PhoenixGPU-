// Package validation -- vGPU oversubscription verification.
//
// Verifies vGPU density, swap engine, and LRU behavior meet targets.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package validation

import (
	"encoding/json"
	"testing"

	"github.com/wlqtjl/PhoenixGPU/pkg/vgpu"
)

// -- Density Verification -------------------------------------------------

func TestVGPU_A100_80GB_2xDensity(t *testing.T) {
	// A100 80GB with 2x oversubscription: 160GB virtual capacity
	// 4 tasks @ 40GB each should all fit
	physicalMiB := uint64(81920) // 80 GiB
	mgr := vgpu.NewOversubManager(physicalMiB, 2.0)

	taskSize := uint64(40960) // 40 GiB per task
	for i := 0; i < 4; i++ {
		if err := mgr.Alloc(taskSize); err != nil {
			t.Fatalf("task %d alloc failed (should fit with 2x oversub): %v", i+1, err)
		}
	}

	t.Logf("4 x 40GiB tasks on 80GiB GPU with 2x oversub: OK")
	t.Logf("  Virtual allocated: %d MiB", mgr.VirtualAllocated())
	t.Logf("  Swap used: %d MiB", mgr.SwapUsed())

	expectedSwap := uint64(4*40960) - physicalMiB // 163840 - 81920 = 81920
	if mgr.SwapUsed() != expectedSwap {
		t.Errorf("swap used = %d MiB, want %d MiB", mgr.SwapUsed(), expectedSwap)
	}
}

func TestVGPU_A100_40GB_1_5xDensity(t *testing.T) {
	// A100 40GB with 1.5x oversubscription: 60GB virtual capacity
	physicalMiB := uint64(40960) // 40 GiB
	mgr := vgpu.NewOversubManager(physicalMiB, 1.5)

	// 3 tasks @ 20GB each = 60GB total (exactly at limit)
	taskSize := uint64(20480) // 20 GiB
	for i := 0; i < 3; i++ {
		if err := mgr.Alloc(taskSize); err != nil {
			t.Fatalf("task %d alloc failed: %v", i+1, err)
		}
	}

	// 4th task should fail (would exceed 1.5x)
	if err := mgr.Alloc(taskSize); err == nil {
		t.Error("4th task should fail (exceeds 1.5x oversubscription limit)")
	}
}

// -- Swap Decision Verification -------------------------------------------

func TestVGPU_SwapDecision_PhysicalFull(t *testing.T) {
	mgr := vgpu.NewOversubManager(8192, 2.0) // 8GiB physical
	_ = mgr.Alloc(8192)                       // Fill physical

	if !mgr.NeedsSwap(1024) {
		t.Error("NeedsSwap should return true when physical VRAM is exhausted")
	}
}

func TestVGPU_SwapDecision_PhysicalAvailable(t *testing.T) {
	mgr := vgpu.NewOversubManager(8192, 2.0)
	_ = mgr.Alloc(4096) // Half full

	if mgr.NeedsSwap(1024) {
		t.Error("NeedsSwap should return false when physical VRAM has space")
	}
}

func TestVGPU_SwapDecision_AfterFree(t *testing.T) {
	mgr := vgpu.NewOversubManager(8192, 2.0)
	_ = mgr.Alloc(8192) // Fill physical
	mgr.Free(4096)       // Free half

	if mgr.NeedsSwap(1024) {
		t.Error("NeedsSwap should return false after freeing physical VRAM")
	}
}

// -- LRU Tracker Verification ---------------------------------------------

func TestVGPU_LRUEvictionOrder(t *testing.T) {
	lru := vgpu.NewLRUTracker(4)

	// Access order: A, B, C, D
	addrs := []uint64{0xA000, 0xB000, 0xC000, 0xD000}
	for _, addr := range addrs {
		lru.Touch(addr)
	}

	// Re-access A and B (C and D are now least recently used)
	lru.Touch(0xA000)
	lru.Touch(0xB000)

	// Eviction should return C first (least recently used)
	victim := lru.Evict()
	if victim != 0xC000 {
		t.Errorf("first eviction = 0x%X, want 0xC000", victim)
	}

	// Then D
	victim = lru.Evict()
	if victim != 0xD000 {
		t.Errorf("second eviction = 0x%X, want 0xD000", victim)
	}
}

func TestVGPU_LRUEmptyEviction(t *testing.T) {
	lru := vgpu.NewLRUTracker(10)
	if lru.Evict() != 0 {
		t.Error("Evict on empty LRU should return 0")
	}
}

// -- Swap Engine E2E ------------------------------------------------------

func TestVGPU_SwapEngineRoundTrip(t *testing.T) {
	engine := vgpu.NewFakeSwapEngine()

	addr := uintptr(0xCAFE0000)
	size := uint64(4 * 1024 * 1024) // 4 MiB

	// Swap out
	if err := engine.SwapOut(addr, size); err != nil {
		t.Fatalf("SwapOut: %v", err)
	}
	if !engine.IsSwapped(addr) {
		t.Error("address should be swapped after SwapOut")
	}

	// Swap in
	if err := engine.SwapIn(addr); err != nil {
		t.Fatalf("SwapIn: %v", err)
	}
	if engine.IsSwapped(addr) {
		t.Error("address should not be swapped after SwapIn")
	}
}

// -- Node vGPU Resources via API ------------------------------------------

func TestVGPU_NodesHaveVRAMMetrics(t *testing.T) {
	base := testServer(t)
	_, body := httpGet(t, base+"/api/v1/nodes")

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	nodes, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("data must be array, got %T", resp.Data)
	}

	for i, n := range nodes {
		node, ok := n.(map[string]interface{})
		if !ok {
			continue
		}
		vramTotal, _ := node["vramTotalMiB"].(float64)
		vramUsed, _ := node["vramUsedMiB"].(float64)

		if vramTotal <= 0 {
			t.Errorf("node[%d] vramTotalMiB=%.0f, must be > 0", i, vramTotal)
		}
		if vramUsed > vramTotal {
			t.Errorf("node[%d] vramUsedMiB (%.0f) > vramTotalMiB (%.0f)", i, vramUsed, vramTotal)
		}

		utilPct := vramUsed / vramTotal * 100
		t.Logf("node[%d] %s: VRAM %.0f/%.0f MiB (%.1f%%)",
			i, node["name"], vramUsed, vramTotal, utilPct)
	}
}
