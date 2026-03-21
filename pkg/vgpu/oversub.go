// Package vgpu — VRAM oversubscription engine.
//
// Allows virtual VRAM allocation to exceed physical GPU memory by
// transparently swapping cold tensors to CPU DRAM (host memory).
//
// Architecture:
//
//	cuMemAlloc hook  ──→  OversubManager.Alloc()
//	     │                    │
//	     │              Physical VRAM available?
//	     │                 Yes → allocate normally
//	     │                 No  → LRU.Evict() → SwapEngine.SwapOut()
//	     │                       → allocate in physical VRAM
//	     │
//	cuMemFree hook   ──→  OversubManager.Free()
//	                      SwapEngine.Remove() if was swapped
//
//	cuLaunchKernel hook → LRU.Touch(all accessed addrs)
//	                      If addr is swapped → SwapEngine.SwapIn()
//
// Engineering Covenant (Sprint 9):
//   - Swap ops MUST NOT hold global mutex (blocks all CUDA calls)
//   - LRU metadata uses atomic operations only
//   - Swap failure must be recoverable (no SIGSEGV)
//   - Page granularity: 2MiB (matches CUDA large page size)
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package vgpu

import (
	"container/list"
	"fmt"
	"sync"
	"sync/atomic"
)

const PageSize = 2 << 20 // 2MiB — CUDA large page granularity

// ── LRUTracker ────────────────────────────────────────────────────

// LRUTracker tracks access recency of GPU memory addresses.
// Uses a doubly-linked list + map for O(1) Touch and O(1) Evict.
//
// Thread safety: protected by a fine-grained mutex (not atomic)
// because the linked list cannot use atomic ops. However, the
// hot path (cuLaunchKernel Touch) is batched to minimize lock contention.
type LRUTracker struct {
	mu       sync.Mutex
	list     *list.List
	items    map[uint64]*list.Element
	capacity int
}

type lruEntry struct {
	addr uint64
}

// NewLRUTracker creates a tracker with the given capacity.
func NewLRUTracker(capacity int) *LRUTracker {
	return &LRUTracker{
		list:     list.New(),
		items:    make(map[uint64]*list.Element, capacity),
		capacity: capacity,
	}
}

// Touch marks addr as most recently used. O(1).
func (l *LRUTracker) Touch(addr uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if el, ok := l.items[addr]; ok {
		l.list.MoveToFront(el)
		return
	}
	el := l.list.PushFront(&lruEntry{addr: addr})
	l.items[addr] = el
}

// Evict removes and returns the least recently used address. O(1).
// Returns 0 if the tracker is empty.
func (l *LRUTracker) Evict() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	back := l.list.Back()
	if back == nil {
		return 0
	}
	entry := l.list.Remove(back).(*lruEntry)
	delete(l.items, entry.addr)
	return entry.addr
}

// Remove explicitly removes an address (on cuMemFree). O(1).
func (l *LRUTracker) Remove(addr uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if el, ok := l.items[addr]; ok {
		l.list.Remove(el)
		delete(l.items, addr)
	}
}

// Len returns the current number of tracked addresses.
func (l *LRUTracker) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.list.Len()
}

// ── OversubManager ────────────────────────────────────────────────

// OversubManager tracks physical and virtual VRAM allocations.
// It decides when swapping is necessary and maintains counters.
//
// All counters use atomic operations — no mutex needed for the
// critical path (cuMemAlloc hook).
type OversubManager struct {
	physicalCapMiB uint64
	oversubRatio   float64
	virtualCapMiB  uint64

	// Atomic counters (safe for concurrent cuMemAlloc/Free)
	physicalUsedMiB atomic.Uint64
	virtualUsedMiB  atomic.Uint64
	swapUsedMiB     atomic.Uint64
}

// NewOversubManager creates a manager.
// physicalMiB: actual GPU VRAM capacity.
// oversubRatio: e.g. 2.0 = allow up to 2× physical as virtual.
func NewOversubManager(physicalMiB uint64, oversubRatio float64) *OversubManager {
	return &OversubManager{
		physicalCapMiB: physicalMiB,
		oversubRatio:   oversubRatio,
		virtualCapMiB:  uint64(float64(physicalMiB) * oversubRatio),
	}
}

// Alloc requests sizeMiB of (virtual) VRAM.
// Returns error if virtual capacity would be exceeded.
func (m *OversubManager) Alloc(sizeMiB uint64) error {
	for {
		current := m.virtualUsedMiB.Load()
		if current+sizeMiB > m.virtualCapMiB {
			return fmt.Errorf(
				"VRAM oversubscription limit: requested %dMiB, used %dMiB, cap %dMiB",
				sizeMiB, current, m.virtualCapMiB)
		}
		// CAS to avoid TOCTOU race
		if m.virtualUsedMiB.CompareAndSwap(current, current+sizeMiB) {
			break
		}
		// Another goroutine won the CAS — retry
	}

	// Track physical vs swap
	for {
		phys := m.physicalUsedMiB.Load()
		if phys+sizeMiB <= m.physicalCapMiB {
			if m.physicalUsedMiB.CompareAndSwap(phys, phys+sizeMiB) {
				break
			}
		} else {
			// Partially consume any remaining physical VRAM, rest goes to swap.
			// This avoids over-counting swap when there is still physical headroom.
			if phys >= m.physicalCapMiB {
				m.swapUsedMiB.Add(sizeMiB)
				break
			}
			remainingPhys := m.physicalCapMiB - phys
			if m.physicalUsedMiB.CompareAndSwap(phys, m.physicalCapMiB) {
				m.swapUsedMiB.Add(sizeMiB - remainingPhys)
				break
			}
		}
	}
	return nil
}

// Free releases sizeMiB of virtual VRAM.
func (m *OversubManager) Free(sizeMiB uint64) {
	m.virtualUsedMiB.Add(^(sizeMiB - 1)) // atomic subtract

	// Adjust physical/swap counters
	swap := m.swapUsedMiB.Load()
	if swap >= sizeMiB {
		m.swapUsedMiB.Add(^(sizeMiB - 1))
	} else {
		if swap > 0 {
			m.swapUsedMiB.Store(0)
			remainder := sizeMiB - swap
			m.physicalUsedMiB.Add(^(remainder - 1))
		} else {
			m.physicalUsedMiB.Add(^(sizeMiB - 1))
		}
	}
}

// NeedsSwap returns true if the next allocation of sizeMiB
// would exceed physical VRAM capacity.
func (m *OversubManager) NeedsSwap(sizeMiB uint64) bool {
	return m.physicalUsedMiB.Load()+sizeMiB > m.physicalCapMiB
}

// VirtualAllocated returns current virtual VRAM usage in MiB.
func (m *OversubManager) VirtualAllocated() uint64 { return m.virtualUsedMiB.Load() }

// PhysicalUsed returns current physical VRAM usage in MiB.
func (m *OversubManager) PhysicalUsed() uint64 { return m.physicalUsedMiB.Load() }

// SwapUsed returns current swap (CPU DRAM) usage in MiB.
func (m *OversubManager) SwapUsed() uint64 { return m.swapUsedMiB.Load() }

// ── SwapEngine interface ──────────────────────────────────────────

// SwapEngine moves GPU memory pages to/from CPU host memory.
// Production: RealSwapEngine (uses cudaMemcpy host↔device)
// Tests:      FakeSwapEngine (in-memory map)
type SwapEngine interface {
	// SwapOut moves the GPU memory at addr (of size bytes) to CPU DRAM.
	// The GPU page is freed after copying.
	SwapOut(addr uintptr, size uint64) error

	// SwapIn restores a previously swapped-out page back to GPU memory.
	// The page must be valid for the original pointer to work again.
	SwapIn(addr uintptr) error

	// IsSwapped returns true if addr is currently in CPU DRAM.
	IsSwapped(addr uintptr) bool
}

// ── FakeSwapEngine (for tests) ────────────────────────────────────

// FakeSwapEngine simulates swapping without real GPU memory operations.
type FakeSwapEngine struct {
	mu      sync.RWMutex
	swapped map[uintptr]uint64 // addr → size
}

func NewFakeSwapEngine() *FakeSwapEngine {
	return &FakeSwapEngine{swapped: make(map[uintptr]uint64)}
}

func (f *FakeSwapEngine) SwapOut(addr uintptr, size uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.swapped[addr] = size
	return nil
}

func (f *FakeSwapEngine) SwapIn(addr uintptr) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.swapped[addr]; !ok {
		return fmt.Errorf("address 0x%X is not swapped out", addr)
	}
	delete(f.swapped, addr)
	return nil
}

func (f *FakeSwapEngine) IsSwapped(addr uintptr) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.swapped[addr]
	return ok
}

// ── RealSwapEngine (production) ──────────────────────────────────
// TODO Sprint 10: implement using cudaMemcpy + pinned host memory

// ── libvgpu C integration stubs ───────────────────────────────────
// These functions are called from hook.c via cgo.
// The swap decision and LRU tracking happen in Go;
// only the actual cudaMemcpy calls happen in C.

// OnCuMemAlloc is called from libvgpu's cuMemAlloc hook.
// Returns the virtual address (may differ from physical if swap needed).
// CGo export omitted here — see libvgpu/src/oversub_bridge.c
func OnCuMemAlloc(sizeMiB uint64) error {
	// Global instance set at init time
	if globalMgr == nil {
		return nil // oversub not enabled
	}
	return globalMgr.Alloc(sizeMiB)
}

// OnCuMemFree is called from libvgpu's cuMemFree hook.
func OnCuMemFree(sizeMiB uint64) {
	if globalMgr != nil {
		globalMgr.Free(sizeMiB)
	}
}

// OnCuLaunchKernel is called on every kernel launch to update LRU.
func OnCuLaunchKernel(addrs []uintptr) {
	if globalLRU == nil {
		return
	}
	for _, addr := range addrs {
		// Align to page boundary
		page := uint64(addr) &^ (PageSize - 1)
		globalLRU.Touch(page)
	}
}

// Package-level singletons (initialized by Device Plugin env vars)
var (
	globalMgr *OversubManager
	globalLRU *LRUTracker
)

// InitOversubscription initialises the package-level state.
// Called once at libvgpu constructor time from C via cgo.
func InitOversubscription(physicalMiB uint64, ratio float64, lruCap int) {
	globalMgr = NewOversubManager(physicalMiB, ratio)
	globalLRU = NewLRUTracker(lruCap)
}
