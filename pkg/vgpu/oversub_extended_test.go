// Additional vgpu oversubscription tests — edge cases and extended coverage.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package vgpu_test

import (
	"sync"
	"testing"

	"github.com/wlqtjl/PhoenixGPU/pkg/vgpu"
)

// ── LRU extended tests ───────────────────────────────────────────

func TestLRU_Remove_ExistingEntry(t *testing.T) {
	lru := vgpu.NewLRUTracker(5)
	lru.Touch(0x1000)
	lru.Touch(0x2000)
	lru.Touch(0x3000)

	lru.Remove(0x2000)

	if lru.Len() != 2 {
		t.Errorf("Len() = %d, want 2 after Remove", lru.Len())
	}

	// Eviction should skip removed entry
	victim := lru.Evict()
	if victim != 0x1000 {
		t.Errorf("Evict() = 0x%X, want 0x1000 (oldest remaining)", victim)
	}
}

func TestLRU_Remove_NonExistentEntry(t *testing.T) {
	lru := vgpu.NewLRUTracker(5)
	lru.Touch(0x1000)

	// Should not panic or corrupt state
	lru.Remove(0xDEAD)

	if lru.Len() != 1 {
		t.Errorf("Len() = %d, want 1 after removing non-existent", lru.Len())
	}
}

func TestLRU_Remove_EmptyTracker(t *testing.T) {
	lru := vgpu.NewLRUTracker(5)
	// Should not panic
	lru.Remove(0x1000)
	if lru.Len() != 0 {
		t.Errorf("Len() = %d, want 0", lru.Len())
	}
}

func TestLRU_Len(t *testing.T) {
	lru := vgpu.NewLRUTracker(10)
	if lru.Len() != 0 {
		t.Errorf("empty tracker Len() = %d, want 0", lru.Len())
	}
	lru.Touch(0x1000)
	lru.Touch(0x2000)
	if lru.Len() != 2 {
		t.Errorf("Len() = %d, want 2", lru.Len())
	}
	lru.Evict()
	if lru.Len() != 1 {
		t.Errorf("Len() after Evict = %d, want 1", lru.Len())
	}
}

func TestLRU_MultipleEvictions(t *testing.T) {
	lru := vgpu.NewLRUTracker(10)
	addrs := []uint64{0xA000, 0xB000, 0xC000, 0xD000, 0xE000}
	for _, addr := range addrs {
		lru.Touch(addr)
	}

	// Evict all in LRU order (oldest first)
	for _, want := range addrs {
		got := lru.Evict()
		if got != want {
			t.Errorf("Evict() = 0x%X, want 0x%X", got, want)
		}
	}

	// Empty after all evictions
	if got := lru.Evict(); got != 0 {
		t.Errorf("Evict() on empty = 0x%X, want 0", got)
	}
}

func TestLRU_ConcurrentRemoveAndTouch(t *testing.T) {
	lru := vgpu.NewLRUTracker(1000)
	var wg sync.WaitGroup

	// Concurrent Touch
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := uint64(0); i < 500; i++ {
			lru.Touch(i * 0x1000)
		}
	}()

	// Concurrent Remove
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := uint64(0); i < 500; i++ {
			lru.Remove(i * 0x1000)
		}
	}()

	// Concurrent Evict
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			lru.Evict()
		}
	}()

	wg.Wait()
	// No race condition = pass
}

// ── OversubManager extended tests ─────────────────────────────────

func TestOversubManager_AllocFreeSymmetry(t *testing.T) {
	mgr := vgpu.NewOversubManager(8192, 2.0)

	// Alloc 100 chunks, free all, should be back to zero
	chunk := uint64(100)
	for i := 0; i < 100; i++ {
		if err := mgr.Alloc(chunk); err != nil {
			t.Fatalf("alloc %d failed: %v", i, err)
		}
	}
	for i := 0; i < 100; i++ {
		mgr.Free(chunk)
	}

	if used := mgr.VirtualAllocated(); used != 0 {
		t.Errorf("VirtualAllocated = %d after freeing all, want 0", used)
	}
}

func TestOversubManager_ExactCapacitySucceeds(t *testing.T) {
	mgr := vgpu.NewOversubManager(1000, 1.0) // exact capacity = 1000
	if err := mgr.Alloc(1000); err != nil {
		t.Errorf("exact capacity allocation should succeed: %v", err)
	}
	// One more byte should fail
	if err := mgr.Alloc(1); err == nil {
		t.Error("should fail when over exact capacity")
	}
}

func TestOversubManager_ConcurrentAlloc(t *testing.T) {
	mgr := vgpu.NewOversubManager(10000, 2.0) // 20000 virtual cap
	var wg sync.WaitGroup
	errs := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mgr.Alloc(100); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent alloc error: %v", err)
	}

	// 100 * 100 = 10000 MiB virtual
	if used := mgr.VirtualAllocated(); used != 10000 {
		t.Errorf("VirtualAllocated = %d, want 10000", used)
	}
}

func TestOversubManager_SwapUsedTracking(t *testing.T) {
	mgr := vgpu.NewOversubManager(4096, 2.0) // 4GiB physical, 8GiB virtual

	// Fill physical
	_ = mgr.Alloc(4096)
	if mgr.SwapUsed() != 0 {
		t.Errorf("SwapUsed = %d, want 0 (within physical)", mgr.SwapUsed())
	}

	// Next alloc goes to swap
	_ = mgr.Alloc(2048)
	if mgr.SwapUsed() != 2048 {
		t.Errorf("SwapUsed = %d, want 2048", mgr.SwapUsed())
	}

	// Physical should be at capacity
	if mgr.PhysicalUsed() != 4096 {
		t.Errorf("PhysicalUsed = %d, want 4096", mgr.PhysicalUsed())
	}
}

func TestOversubManager_NeedsSwap_BoundaryCase(t *testing.T) {
	mgr := vgpu.NewOversubManager(4096, 2.0)
	_ = mgr.Alloc(4095)

	// Exact 1 MiB remaining in physical
	if mgr.NeedsSwap(1) {
		t.Error("should NOT need swap for 1 MiB when 1 MiB physical remains")
	}
	if !mgr.NeedsSwap(2) {
		t.Error("should need swap for 2 MiB when only 1 MiB physical remains")
	}
}

func TestOversubManager_Ratio1x_NoSwapAllowed(t *testing.T) {
	mgr := vgpu.NewOversubManager(4096, 1.0) // no oversubscription

	if err := mgr.Alloc(4096); err != nil {
		t.Fatalf("full physical alloc should succeed: %v", err)
	}
	if err := mgr.Alloc(1); err == nil {
		t.Error("should fail — ratio 1.0 allows no oversubscription")
	}
}

// ── FakeSwapEngine extended tests ─────────────────────────────────

func TestFakeSwapEngine_ConcurrentSwapOps(t *testing.T) {
	engine := vgpu.NewFakeSwapEngine()
	var wg sync.WaitGroup

	// Concurrent SwapOut
	for i := uintptr(0); i < 100; i++ {
		wg.Add(1)
		go func(addr uintptr) {
			defer wg.Done()
			_ = engine.SwapOut(addr*0x1000, 4096)
		}(i)
	}
	wg.Wait()

	// Concurrent SwapIn
	for i := uintptr(0); i < 100; i++ {
		wg.Add(1)
		go func(addr uintptr) {
			defer wg.Done()
			_ = engine.SwapIn(addr * 0x1000)
		}(i)
	}
	wg.Wait()

	// All should be swapped back in
	for i := uintptr(0); i < 100; i++ {
		if engine.IsSwapped(i * 0x1000) {
			t.Errorf("address 0x%X should not be swapped after SwapIn", i*0x1000)
		}
	}
}

func TestFakeSwapEngine_DoubleSwapOut_Overwrites(t *testing.T) {
	engine := vgpu.NewFakeSwapEngine()
	addr := uintptr(0xA000)

	_ = engine.SwapOut(addr, 4096)
	_ = engine.SwapOut(addr, 8192) // overwrite

	if !engine.IsSwapped(addr) {
		t.Error("address should be marked as swapped")
	}
}

// ── PageSize constant test ────────────────────────────────────────

func TestPageSize_Is2MiB(t *testing.T) {
	expected := 2 << 20 // 2 * 1024 * 1024 = 2097152
	if vgpu.PageSize != expected {
		t.Errorf("PageSize = %d, want %d (2MiB)", vgpu.PageSize, expected)
	}
}
