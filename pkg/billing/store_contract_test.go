// Sprint 6 TDD Red phase — tests define contracts before implementation.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package billing_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/wlqtjl/PhoenixGPU/pkg/billing"
)

// ── T49-T51: BillingStore contract ───────────────────────────────

// billingStoreContract runs the full contract against any Store implementation.
// Pass either MemoryStore (unit) or PostgresStore (integration) — same tests.
func billingStoreContract(t *testing.T, store billing.Store) {
	t.Helper()
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	rec := billing.UsageRecord{
		Namespace:  "research",
		PodName:    "llm-pod-1",
		JobName:    "llm-pretrain",
		Department: "算法研究院",
		Project:    "LLM预训练",
		GPUModel:   "NVIDIA-A100-80GB",
		AllocRatio: 0.5,
		StartedAt:  now.Add(-2 * time.Hour),
		EndedAt:    now,
	}

	// ── C1: SaveRecord computes and persists ──────────────────────
	t.Run("save_and_retrieve", func(t *testing.T) {
		engine := billing.NewEngine(store, nil)
		if err := engine.Record(ctx, rec); err != nil {
			t.Fatalf("Record() error: %v", err)
		}

		records, err := store.ListRecords(ctx, billing.RecordFilter{
			Department: "算法研究院",
		})
		if err != nil {
			t.Fatalf("ListRecords: %v", err)
		}
		if len(records) == 0 {
			t.Fatal("expected at least 1 record after save")
		}
		r := records[len(records)-1]
		if math.Abs(r.GPUHours-1.0) > 0.01 {
			t.Errorf("GPUHours = %.3f, want ~1.0", r.GPUHours)
		}
		// TFlopsHours = 0.5 × 312 × 2 = 312
		if math.Abs(r.TFlopsHours-312.0) > 0.5 {
			t.Errorf("TFlopsHours = %.1f, want ~312.0", r.TFlopsHours)
		}
	})

	// ── C2: SumByDepartment aggregates correctly ──────────────────
	t.Run("sum_by_department", func(t *testing.T) {
		engine := billing.NewEngine(store, nil)
		// Record 2 more for same department
		for i := 0; i < 2; i++ {
			r := rec
			r.PodName = "extra-pod-" + string(rune('A'+i))
			_ = engine.Record(ctx, r)
		}

		totals, err := store.SumByDepartment(ctx, "monthly")
		if err != nil {
			t.Fatalf("SumByDepartment: %v", err)
		}
		hours, ok := totals["算法研究院"]
		if !ok {
			t.Fatal("department missing from sum")
		}
		if hours < 1.0 {
			t.Errorf("expected >= 1.0 GPU hours, got %.2f", hours)
		}
	})

	// ── C3: GetQuotaStatus reflects usage ─────────────────────────
	t.Run("quota_status_reflects_usage", func(t *testing.T) {
		if ms, ok := store.(*billing.MemoryStore); ok {
			ms.SetQuota("算法研究院", billing.QuotaPolicy{
				TenantID:          "算法研究院",
				SoftLimitGPUHours: 10,
				HardLimitGPUHours: 20,
				Period:            "monthly",
			})
		}

		status, err := store.GetQuotaStatus(ctx, "算法研究院", "monthly")
		if err != nil {
			t.Fatalf("GetQuotaStatus: %v", err)
		}
		if status == nil {
			t.Skip("quota not set in this store (OK for integration without seed data)")
		}
		if status.UsedGPUHours < 0 {
			t.Error("UsedGPUHours must be non-negative")
		}
	})

	// ── C4: Filter by time range ──────────────────────────────────
	t.Run("filter_by_time_range", func(t *testing.T) {
		records, err := store.ListRecords(ctx, billing.RecordFilter{
			From: now.Add(-3 * time.Hour),
			To:   now.Add(1 * time.Hour),
		})
		if err != nil {
			t.Fatalf("ListRecords with time filter: %v", err)
		}
		for _, r := range records {
			if r.EndedAt.Before(now.Add(-3*time.Hour)) || r.StartedAt.After(now.Add(1*time.Hour)) {
				t.Errorf("record outside time filter: started=%v ended=%v", r.StartedAt, r.EndedAt)
			}
		}
	})
}

// Run contract against MemoryStore (always runs, no DB needed)
func TestMemoryStore_Contract(t *testing.T) {
	billingStoreContract(t, billing.NewMemoryStore())
}

// PostgresStore contract — only runs with DB connection
// go test -tags integration -run TestPostgresStore_Contract
func TestPostgresStore_Contract(t *testing.T) {
	t.Skip("requires TimescaleDB — run with: make test-integration")
	// store, err := billing.NewPostgresStore(os.Getenv("BILLING_DB_DSN"))
	// if err != nil { t.Skipf("DB unavailable: %v", err) }
	// billingStoreContract(t, store)
}

// ── T52-T53: LiveMigration contract ──────────────────────────────

package migration_test

import (
	"context"
	"testing"
	"time"

	"github.com/wlqtjl/PhoenixGPU/pkg/migration"
)

func TestMigrationPlan_ValidatesCorrectly(t *testing.T) {
	plan := migration.Plan{
		JobNamespace: "research",
		JobName:      "llm-pretrain",
		SourceNode:   "gpu-node-01",
		TargetNode:   "gpu-node-02",
	}
	if err := plan.Validate(); err != nil {
		t.Errorf("valid plan rejected: %v", err)
	}
}

func TestMigrationPlan_RejectsIdenticalNodes(t *testing.T) {
	plan := migration.Plan{
		JobNamespace: "research",
		JobName:      "llm-pretrain",
		SourceNode:   "gpu-node-01",
		TargetNode:   "gpu-node-01", // same node — invalid
	}
	if err := plan.Validate(); err == nil {
		t.Error("expected error when source == target node")
	}
}

func TestMigrationPlan_RejectsMissingFields(t *testing.T) {
	cases := []migration.Plan{
		{JobName: "j", SourceNode: "n1", TargetNode: "n2"},           // missing namespace
		{JobNamespace: "ns", SourceNode: "n1", TargetNode: "n2"},     // missing name
		{JobNamespace: "ns", JobName: "j", TargetNode: "n2"},         // missing source
		{JobNamespace: "ns", JobName: "j", SourceNode: "n1"},         // missing target
	}
	for _, p := range cases {
		if err := p.Validate(); err == nil {
			t.Errorf("expected validation error for plan %+v", p)
		}
	}
}

func TestMigrationState_Transitions(t *testing.T) {
	// State machine: Pending → PreDumping → Dumping → Transferring → Restoring → Done
	states := []migration.State{
		migration.StatePending,
		migration.StatePreDumping,
		migration.StateDumping,
		migration.StateTransferring,
		migration.StateRestoring,
		migration.StateDone,
	}
	for i := 1; i < len(states); i++ {
		if !migration.CanTransition(states[i-1], states[i]) {
			t.Errorf("expected valid transition %s → %s", states[i-1], states[i])
		}
	}
}

func TestMigrationState_RejectsInvalidTransitions(t *testing.T) {
	invalid := [][2]migration.State{
		{migration.StateDone, migration.StatePending},        // no going back
		{migration.StateRestoring, migration.StatePreDumping}, // no going back
		{migration.StatePending, migration.StateDone},         // skip steps
	}
	for _, pair := range invalid {
		if migration.CanTransition(pair[0], pair[1]) {
			t.Errorf("expected invalid transition %s → %s", pair[0], pair[1])
		}
	}
}

func TestFreezeWindowEstimate_UnderFiveSeconds(t *testing.T) {
	// Engineering target: freeze window < 5s for models up to 80GB VRAM
	// EstimateFreezeWindow returns estimated seconds based on VRAM size
	cases := []struct {
		vramMiB int64
		maxSecs float64
	}{
		{8192,  2.0},  // 8GB  → < 2s
		{40960, 4.0},  // 40GB → < 4s
		{81920, 5.0},  // 80GB → < 5s (boundary)
	}
	for _, tc := range cases {
		est := migration.EstimateFreezeWindow(tc.vramMiB)
		if est > tc.maxSecs {
			t.Errorf("vram=%dMiB: estimated freeze %.2fs > target %.2fs",
				tc.vramMiB, est, tc.maxSecs)
		}
	}
}

func TestMigrationExecutor_MockRoundtrip(t *testing.T) {
	// Full mock migration: Pending → Done in < 1s (no real CRIU)
	executor := migration.NewMockExecutor()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
		t.Errorf("expected Done state, got %s", result.State)
	}
	if result.TotalDuration > 2*time.Second {
		t.Errorf("mock executor too slow: %s", result.TotalDuration)
	}
}
