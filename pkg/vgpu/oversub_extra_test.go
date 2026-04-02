// Additional coverage tests for vgpu package.
// Covers LRU Remove/Len, global hooks, InitOversubscription,
// and edge cases not in the original TDD suite.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package vgpu_test

import (
	"testing"

	"github.com/wlqtjl/PhoenixGPU/pkg/vgpu"
)

// ── LRU Remove ────────────────────────────────────────────────────

func TestLRU_Remove_ExistingEntry(t *testing.T) {
	lru := vgpu.NewLRUTracker(10)
	lru.Touch(0x1000)
	lru.Touch(0x2000)
	lru.Touch(0x3000)

	lru.Remove(0x2000)
	if lru.Len() != 2 {
		t.Errorf("Len after Remove = %d, want 2", lru.Len())
	}

	// Evict should return 0x1000 (oldest remaining)
	victim := lru.Evict()
	if victim != 0x1000 {
		t.Errorf("Evict after Remove = 0x%X, want 0x1000", victim)
	}
}

func TestLRU_Remove_NonExistentEntry(t *testing.T) {
	lru := vgpu.NewLRUTracker(10)
	lru.Touch(0x1000)
	lru.Remove(0xDEAD) // should not panic
	if lru.Len() != 1 {
		t.Errorf("Len = %d, want 1", lru.Len())
	}
}

// ── LRU Len ───────────────────────────────────────────────────────

func TestLRU_Len_Empty(t *testing.T) {
	lru := vgpu.NewLRUTracker(5)
	if lru.Len() != 0 {
		t.Errorf("Len of empty LRU = %d, want 0", lru.Len())
	}
}

func TestLRU_Len_AfterTouches(t *testing.T) {
	lru := vgpu.NewLRUTracker(10)
	lru.Touch(0x1000)
	lru.Touch(0x2000)
	lru.Touch(0x3000)
	if lru.Len() != 3 {
		t.Errorf("Len = %d, want 3", lru.Len())
	}

	// Repeated touch should not increase length
	lru.Touch(0x1000)
	if lru.Len() != 3 {
		t.Errorf("Len after repeated touch = %d, want 3", lru.Len())
	}
}

func TestLRU_Len_AfterEvict(t *testing.T) {
	lru := vgpu.NewLRUTracker(5)
	lru.Touch(0x1000)
	lru.Touch(0x2000)
	lru.Evict()
	if lru.Len() != 1 {
		t.Errorf("Len after evict = %d, want 1", lru.Len())
	}
}

// ── OversubManager edge cases ─────────────────────────────────────

func TestOversubManager_ZeroSizeAlloc(t *testing.T) {
	mgr := vgpu.NewOversubManager(1024, 2.0)
	if err := mgr.Alloc(0); err != nil {
		t.Errorf("zero-size alloc should succeed: %v", err)
	}
}

func TestOversubManager_ExactlyAtLimit(t *testing.T) {
	mgr := vgpu.NewOversubManager(1024, 1.0) // limit = 1024 MiB
	if err := mgr.Alloc(1024); err != nil {
		t.Errorf("allocation at exact limit should succeed: %v", err)
	}
	// Next alloc of even 1 MiB should fail
	if err := mgr.Alloc(1); err == nil {
		t.Error("expected error when exceeding limit by 1 MiB")
	}
}

func TestOversubManager_FreeAndReallocate(t *testing.T) {
	mgr := vgpu.NewOversubManager(1024, 1.0)
	_ = mgr.Alloc(1024)
	mgr.Free(512)
	// Should be able to reallocate freed space
	if err := mgr.Alloc(512); err != nil {
		t.Errorf("alloc after partial free should succeed: %v", err)
	}
}

func TestOversubManager_PhysicalUsed(t *testing.T) {
	mgr := vgpu.NewOversubManager(4096, 2.0)
	_ = mgr.Alloc(2048)
	if mgr.PhysicalUsed() != 2048 {
		t.Errorf("PhysicalUsed = %d, want 2048", mgr.PhysicalUsed())
	}
}

func TestOversubManager_SwapUsed_WithinPhysical(t *testing.T) {
	mgr := vgpu.NewOversubManager(4096, 2.0)
	_ = mgr.Alloc(2048) // within physical capacity
	if mgr.SwapUsed() != 0 {
		t.Errorf("SwapUsed = %d, want 0 (within physical)", mgr.SwapUsed())
	}
}

func TestOversubManager_SwapUsed_ExceedsPhysical(t *testing.T) {
	mgr := vgpu.NewOversubManager(4096, 2.0)
	_ = mgr.Alloc(4096) // fills physical
	_ = mgr.Alloc(1024) // goes to swap
	if mgr.SwapUsed() != 1024 {
		t.Errorf("SwapUsed = %d, want 1024", mgr.SwapUsed())
	}
}

func TestOversubManager_MultipleFrees(t *testing.T) {
	mgr := vgpu.NewOversubManager(4096, 2.0) // 8192 total virtual
	_ = mgr.Alloc(4096)
	_ = mgr.Alloc(2048) // swap used = 2048
	mgr.Free(2048)       // free from swap first
	mgr.Free(2048)       // free from physical

	// Should be back to 2048 used
	if mgr.VirtualAllocated() != 2048 {
		t.Errorf("VirtualAllocated = %d, want 2048", mgr.VirtualAllocated())
	}
}

// ── FakeSwapEngine edge cases ─────────────────────────────────────

func TestFakeSwapEngine_DoubleSwapOut(t *testing.T) {
	engine := vgpu.NewFakeSwapEngine()
	addr := uintptr(0xA000000)
	_ = engine.SwapOut(addr, 4096)
	// Second swapout overwrites — should not error
	if err := engine.SwapOut(addr, 8192); err != nil {
		t.Errorf("double SwapOut should not error: %v", err)
	}
	if !engine.IsSwapped(addr) {
		t.Error("address should still be swapped")
	}
}

func TestFakeSwapEngine_ConcurrentAccess(t *testing.T) {
	engine := vgpu.NewFakeSwapEngine()
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func(base uintptr) {
			for j := uintptr(0); j < 50; j++ {
				addr := base + j*0x1000
				_ = engine.SwapOut(addr, 4096)
				engine.IsSwapped(addr)
				_ = engine.SwapIn(addr)
			}
			done <- struct{}{}
		}(uintptr(i) * 0x100000)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

// ── PageSize constant ─────────────────────────────────────────────

func TestPageSize_Is2MiB(t *testing.T) {
	if vgpu.PageSize != 2<<20 {
		t.Errorf("PageSize = %d, want %d (2MiB)", vgpu.PageSize, 2<<20)
	}
}

// ── InitOversubscription ──────────────────────────────────────────

func TestInitOversubscription_DoesNotPanic(t *testing.T) {
	// Just verify it doesn't panic — singletons are package-level
	vgpu.InitOversubscription(4096, 2.0, 1000)
}

// ── Global hook functions ─────────────────────────────────────────

func TestOnCuMemAlloc_AfterInit(t *testing.T) {
	vgpu.InitOversubscription(8192, 2.0, 100)
	if err := vgpu.OnCuMemAlloc(1024); err != nil {
		t.Errorf("OnCuMemAlloc: %v", err)
	}
}

func TestOnCuMemFree_AfterInit(t *testing.T) {
	vgpu.InitOversubscription(8192, 2.0, 100)
	_ = vgpu.OnCuMemAlloc(1024)
	vgpu.OnCuMemFree(1024) // should not panic
}

func TestOnCuLaunchKernel_AfterInit(t *testing.T) {
	vgpu.InitOversubscription(8192, 2.0, 100)
	addrs := []uintptr{0x1000000, 0x2000000, 0x3000000}
	vgpu.OnCuLaunchKernel(addrs) // should not panic
}

func TestOnCuLaunchKernel_EmptyAddrs(t *testing.T) {
	vgpu.InitOversubscription(8192, 2.0, 100)
	vgpu.OnCuLaunchKernel(nil) // should not panic
}

// ── NeedsSwap boundary ────────────────────────────────────────────

func TestNeedsSwap_ExactlyAtPhysicalBoundary(t *testing.T) {
	mgr := vgpu.NewOversubManager(4096, 2.0)
	_ = mgr.Alloc(4096) // exactly at physical limit

	// Allocating 0 more shouldn't need swap (boundary case)
	if mgr.NeedsSwap(0) {
		t.Error("NeedsSwap(0) should be false at exact boundary")
	}
	// Allocating 1 more does need swap
	if !mgr.NeedsSwap(1) {
		t.Error("NeedsSwap(1) should be true when physical is full")
	}
}
