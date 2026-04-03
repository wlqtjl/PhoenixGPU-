//go:build billingfull
// +build billingfull

package billing

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"time"
)

func newEngine(t *testing.T) (*Engine, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	e := NewEngine(store, nil) // nil logger OK for tests
	return e, store
}

// ─── Compute tests ────────────────────────────────────────────────

func TestCompute_BasicA100(t *testing.T) {
	e, _ := newEngine(t)
	now := time.Now()
	r := UsageRecord{
		GPUModel:   "NVIDIA-A100-80GB",
		AllocRatio: 0.5,
		StartedAt:  now.Add(-2 * time.Hour),
		EndedAt:    now,
	}
	if err := e.Compute(&r); err != nil {
		t.Fatalf("Compute error: %v", err)
	}

	// DurationHours ≈ 2.0
	if math.Abs(r.DurationHours-2.0) > 0.01 {
		t.Errorf("DurationHours = %.4f, want ~2.0", r.DurationHours)
	}
	// GPUHours = 0.5 × 2 = 1.0
	if math.Abs(r.GPUHours-1.0) > 0.001 {
		t.Errorf("GPUHours = %.4f, want 1.0", r.GPUHours)
	}
	// TFlopsHours = 0.5 × 312 × 2 = 312
	want := 0.5 * 312.0 * 2.0
	if math.Abs(r.TFlopsHours-want) > 0.01 {
		t.Errorf("TFlopsHours = %.2f, want %.2f", r.TFlopsHours, want)
	}
	// CostCNY = 0.5 × 35 × 2 = 35
	wantCost := 0.5 * 35.0 * 2.0
	if math.Abs(r.CostCNY-wantCost) > 0.01 {
		t.Errorf("CostCNY = %.2f, want %.2f", r.CostCNY, wantCost)
	}
}

func TestCompute_H800Full(t *testing.T) {
	e, _ := newEngine(t)
	now := time.Now()
	r := UsageRecord{
		GPUModel:   "NVIDIA-H800",
		AllocRatio: 1.0,
		StartedAt:  now.Add(-1 * time.Hour),
		EndedAt:    now,
	}
	if err := e.Compute(&r); err != nil {
		t.Fatal(err)
	}
	// TFlopsHours = 1.0 × 2000 × 1.0 = 2000
	if math.Abs(r.TFlopsHours-2000.0) > 0.1 {
		t.Errorf("TFlopsHours = %.2f, want 2000.0", r.TFlopsHours)
	}
	// CostCNY = 1.0 × 55 × 1.0 = 55
	if math.Abs(r.CostCNY-55.0) > 0.01 {
		t.Errorf("CostCNY = %.2f, want 55.0", r.CostCNY)
	}
}

func TestCompute_InvalidAllocRatio(t *testing.T) {
	e, _ := newEngine(t)
	now := time.Now()
	cases := []float64{0.0, -0.1, 1.1, 2.0}
	for _, ratio := range cases {
		r := UsageRecord{
			GPUModel:   "NVIDIA-A100-80GB",
			AllocRatio: ratio,
			StartedAt:  now.Add(-1 * time.Hour),
			EndedAt:    now,
		}
		if err := e.Compute(&r); err == nil {
			t.Errorf("expected error for AllocRatio=%.2f, got nil", ratio)
		}
	}
}

func TestCompute_InvalidTimeRange(t *testing.T) {
	e, _ := newEngine(t)
	now := time.Now()
	r := UsageRecord{
		GPUModel:   "NVIDIA-A100-80GB",
		AllocRatio: 0.5,
		StartedAt:  now,
		EndedAt:    now.Add(-1 * time.Hour), // end before start
	}
	if err := e.Compute(&r); err == nil {
		t.Error("expected error when EndedAt < StartedAt")
	}
}

func TestCompute_UnknownGPUUsesDefaults(t *testing.T) {
	e, _ := newEngine(t)
	now := time.Now()
	r := UsageRecord{
		GPUModel:   "Unknown-GPU-XYZ",
		AllocRatio: 1.0,
		StartedAt:  now.Add(-1 * time.Hour),
		EndedAt:    now,
	}
	// Should not error — falls back to defaults
	if err := e.Compute(&r); err != nil {
		t.Fatalf("unexpected error for unknown GPU: %v", err)
	}
	if r.TFlopsHours <= 0 {
		t.Error("TFlopsHours should be positive even for unknown GPU")
	}
}

func TestCompute_EndedAtDefaultsToNow(t *testing.T) {
	e, _ := newEngine(t)
	r := UsageRecord{
		GPUModel:   "NVIDIA-RTX-4090",
		AllocRatio: 1.0,
		StartedAt:  time.Now().Add(-30 * time.Minute),
		// EndedAt intentionally zero
	}
	before := time.Now()
	if err := e.Compute(&r); err != nil {
		t.Fatal(err)
	}
	if r.EndedAt.Before(before) {
		t.Error("EndedAt should default to approximately now")
	}
}

// ─── Record + Store tests ─────────────────────────────────────────

func TestRecord_SavesCorrectly(t *testing.T) {
	e, store := newEngine(t)
	ctx := context.Background()

	now := time.Now()
	err := e.Record(ctx, UsageRecord{
		Namespace:  "research",
		PodName:    "train-pod-1",
		Department: "NLP Lab",
		GPUModel:   "NVIDIA-A100-80GB",
		AllocRatio: 0.25,
		StartedAt:  now.Add(-4 * time.Hour),
		EndedAt:    now,
	})
	if err != nil {
		t.Fatalf("Record() error: %v", err)
	}

	records, _ := store.ListRecords(ctx, RecordFilter{Department: "NLP Lab"})
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	r := records[0]
	// 0.25 × 312 × 4 = 312
	wantTF := 0.25 * 312.0 * 4.0
	if math.Abs(r.TFlopsHours-wantTF) > 0.1 {
		t.Errorf("TFlopsHours = %.2f, want %.2f", r.TFlopsHours, wantTF)
	}
}

func TestRecord_QuotaAlertFired(t *testing.T) {
	e, store := newEngine(t)
	store.quotas["CV Lab"] = QuotaPolicy{
		TenantID:          "CV Lab",
		SoftLimitGPUHours: 100, // 100 GPU hour soft limit
	}

	var alertCount int64
	e.RegisterAlertHook(func(_ context.Context, _ QuotaStatus) {
		atomic.AddInt64(&alertCount, 1)
	})

	ctx := context.Background()
	now := time.Now()

	// Record 9 GPU hours — should NOT fire alert (9/100 = 9%, well under 80%)
	_ = e.Record(ctx, UsageRecord{
		Department: "CV Lab",
		GPUModel:   "NVIDIA-RTX-4090",
		AllocRatio: 1.0,
		StartedAt:  now.Add(-9 * time.Hour),
		EndedAt:    now,
	})
	time.Sleep(50 * time.Millisecond)
	if c := atomic.LoadInt64(&alertCount); c != 0 {
		t.Errorf("expected 0 alerts at 9%% usage, got %d", c)
	}

	// Record 80 more GPU hours — pushes to 89/100 = 89% (>= 80% soft limit)
	_ = e.Record(ctx, UsageRecord{
		Department: "CV Lab",
		GPUModel:   "NVIDIA-RTX-4090",
		AllocRatio: 1.0,
		StartedAt:  now.Add(-80 * time.Hour),
		EndedAt:    now,
	})
	time.Sleep(50 * time.Millisecond)
	if c := atomic.LoadInt64(&alertCount); c < 1 {
		t.Errorf("expected at least 1 quota alert, got %d", c)
	}
}

// ─── KnownGPUs coverage ───────────────────────────────────────────

func TestKnownGPUs_AllHavePositiveTFlops(t *testing.T) {
	for model, spec := range KnownGPUs {
		if spec.FP16TFlops <= 0 {
			t.Errorf("GPU %s has non-positive FP16TFlops: %.1f", model, spec.FP16TFlops)
		}
		if spec.PricePerHour <= 0 {
			t.Errorf("GPU %s has non-positive PricePerHour: %.2f", model, spec.PricePerHour)
		}
	}
}

// ─── SumByDepartment ─────────────────────────────────────────────

func TestSumByDepartment(t *testing.T) {
	e, store := newEngine(t)
	ctx := context.Background()
	now := time.Now()

	depts := []string{"NLP", "CV", "NLP", "Infra"}
	for _, d := range depts {
		_ = e.Record(ctx, UsageRecord{
			Department: d,
			GPUModel:   "NVIDIA-A100-40GB",
			AllocRatio: 1.0,
			StartedAt:  now.Add(-1 * time.Hour),
			EndedAt:    now,
		})
	}

	totals, err := store.SumByDepartment(ctx, "monthly")
	if err != nil {
		t.Fatal(err)
	}
	if totals["NLP"] < 1.9 {
		t.Errorf("NLP should have ~2 GPU hours, got %.2f", totals["NLP"])
	}
	if math.Abs(totals["CV"]-1.0) > 0.01 {
		t.Errorf("CV should have ~1 GPU hour, got %.2f", totals["CV"])
	}
}
