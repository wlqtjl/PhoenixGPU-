//go:build billingfull
// +build billingfull

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

	t.Run("save_and_retrieve", func(t *testing.T) {
		engine := billing.NewEngine(store, nil)
		if err := engine.Record(ctx, rec); err != nil {
			t.Fatalf("Record() error: %v", err)
		}

		records, err := store.ListRecords(ctx, billing.RecordFilter{Department: "算法研究院"})
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
		if math.Abs(r.TFlopsHours-312.0) > 0.5 {
			t.Errorf("TFlopsHours = %.1f, want ~312.0", r.TFlopsHours)
		}
	})
}

func TestMemoryStore_Contract(t *testing.T) {
	billingStoreContract(t, billing.NewMemoryStore())
}

func TestPostgresStore_Contract(t *testing.T) {
	t.Skip("requires TimescaleDB — run with: make test-integration")
}
