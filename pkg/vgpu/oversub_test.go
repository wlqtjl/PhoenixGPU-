// Sprint 9 TDD Red — VRAM oversubscription contract tests.
// Written before implementation. Defines exact behavior expected.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package vgpu_test

import (
	"testing"

	"github.com/wlqtjl/PhoenixGPU/pkg/vgpu"
)

// ── LRU Policy ────────────────────────────────────────────────────

func TestLRU_EvictsLeastRecentlyUsed(t *testing.T) {
	lru := vgpu.NewLRUTracker(3) // capacity: 3 entries

	// Access pages in order: A, B, C
	lru.Touch(0xA000)
	lru.Touch(0xB000)
	lru.Touch(0xC000)

	// Now access A again — B becomes LRU
	lru.Touch(0xA000)

	victim := lru.Evict()
	if victim != 0xB000 {
		t.Errorf("expected LRU victim 0xB000, got 0x%X", victim)
	}
}

func TestLRU_ReturnsZeroWhenEmpty(t *testing.T) {
	lru := vgpu.NewLRUTracker(5)
	if lru.Evict() != 0 {
		t.Error("Evict on empty LRU should return 0")
	}
}

func TestLRU_HandlesRepeatedTouch(t *testing.T) {
	lru := vgpu.NewLRUTracker(2)
	lru.Touch(0x1000)
	lru.Touch(0x1000) // repeated — should not create duplicate
	lru.Touch(0x2000)
	lru.Touch(0x1000) // 0x2000 is now LRU

	victim := lru.Evict()
	if victim != 0x2000 {
		t.Errorf("expected 0x2000, got 0x%X", victim)
	}
}

func TestLRU_ConcurrentTouchIsSafe(t *testing.T) {
	// LRU must be safe for concurrent use (atomic ops, no Mutex)
	lru := vgpu.NewLRUTracker(100)
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func(base uint64) {
			for j := uint64(0); j < 100; j++ {
				lru.Touch(base + j*0x1000)
			}
			done <- struct{}{}
		}(uint64(i) * 0x100000)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	// If we get here without race detector firing, test passes
}

// ── Oversubscription manager ──────────────────────────────────────

func TestOversubManager_AllowsUpTo2xPhysical(t *testing.T) {
	// 8GiB physical, allow up to 16GiB virtual (2× oversubscription)
	mgr := vgpu.NewOversubManager(8192, 2.0)

	// First 8GiB: from physical VRAM
	if err := mgr.Alloc(4096); err != nil {
		t.Errorf("first 4GiB alloc failed: %v", err)
	}
	if err := mgr.Alloc(4096); err != nil {
		t.Errorf("second 4GiB alloc failed: %v", err)
	}

	// Next 8GiB: from swap (CPU DRAM)
	if err := mgr.Alloc(4096); err != nil {
		t.Errorf("swap alloc failed: %v", err)
	}
}

func TestOversubManager_RejectsExceedingOversubRatio(t *testing.T) {
	mgr := vgpu.NewOversubManager(4096, 1.5) // max 6GiB virtual

	if err := mgr.Alloc(4096); err != nil {
		t.Fatalf("first alloc: %v", err)
	}
	if err := mgr.Alloc(2048); err != nil {
		t.Fatalf("second alloc: %v", err)
	}
	// 6144 MiB used = limit, next alloc should fail
	if err := mgr.Alloc(1); err == nil {
		t.Error("expected error when exceeding oversubscription limit")
	}
}

func TestOversubManager_FreeReducesUsage(t *testing.T) {
	mgr := vgpu.NewOversubManager(4096, 2.0)
	_ = mgr.Alloc(4096)
	mgr.Free(4096)

	// Should be able to alloc again
	if err := mgr.Alloc(4096); err != nil {
		t.Errorf("alloc after free failed: %v", err)
	}
}

func TestOversubManager_SwapDecision(t *testing.T) {
	mgr := vgpu.NewOversubManager(4096, 2.0) // 4GiB physical

	// Fill physical VRAM
	_ = mgr.Alloc(4096)

	// Next alloc: needs swap
	if !mgr.NeedsSwap(1024) {
		t.Error("expected NeedsSwap=true when physical VRAM is full")
	}

	// Within physical limit: no swap needed
	mgr2 := vgpu.NewOversubManager(4096, 2.0)
	_ = mgr2.Alloc(2048)
	if mgr2.NeedsSwap(1024) {
		t.Error("expected NeedsSwap=false when physical VRAM has space")
	}
}

// ── Swap engine ───────────────────────────────────────────────────

func TestSwapEngine_RecordsSwappedOut(t *testing.T) {
	engine := vgpu.NewFakeSwapEngine()

	addr := uintptr(0xA000000)
	size := uint64(4096 * 1024) // 4MiB

	if err := engine.SwapOut(addr, size); err != nil {
		t.Fatalf("SwapOut: %v", err)
	}
	if !engine.IsSwapped(addr) {
		t.Error("address should be marked as swapped out")
	}
}

func TestSwapEngine_SwapInRestoresAddress(t *testing.T) {
	engine := vgpu.NewFakeSwapEngine()
	addr := uintptr(0xB000000)
	size := uint64(4096 * 1024)

	_ = engine.SwapOut(addr, size)
	if err := engine.SwapIn(addr); err != nil {
		t.Fatalf("SwapIn: %v", err)
	}
	if engine.IsSwapped(addr) {
		t.Error("address should NOT be marked as swapped after SwapIn")
	}
}

func TestSwapEngine_SwapInNonExistentReturnsError(t *testing.T) {
	engine := vgpu.NewFakeSwapEngine()
	err := engine.SwapIn(0xDEADBEEF)
	if err == nil {
		t.Error("SwapIn of non-swapped address should return error")
	}
}

// ── Density benchmark (proxy test) ───────────────────────────────

func TestDensity_TwoTasksShareOneGPU(t *testing.T) {
	// Simulate: physical VRAM = 24GiB, two tasks each requesting 16GiB
	// Without oversubscription: only 1 task fits
	// With 2× oversubscription: both tasks fit (one uses swap)
	physicalMiB := uint64(24576) // 24GiB
	mgr := vgpu.NewOversubManager(physicalMiB, 2.0)

	task1 := uint64(16384) // 16GiB
	task2 := uint64(16384)

	if err := mgr.Alloc(task1); err != nil {
		t.Fatalf("task1 alloc: %v", err)
	}
	if err := mgr.Alloc(task2); err != nil {
		t.Errorf("task2 should fit with oversubscription, but got: %v", err)
	}

	// Both tasks allocated
	if mgr.VirtualAllocated() != task1+task2 {
		t.Errorf("virtual allocated = %d, want %d", mgr.VirtualAllocated(), task1+task2)
	}
	if mgr.SwapUsed() != task1+task2-physicalMiB {
		t.Errorf("swap used = %d, want %d", mgr.SwapUsed(), task1+task2-physicalMiB)
	}
}
