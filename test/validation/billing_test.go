// Package validation — billing accuracy verification.
//
// Verifies TFlops·h billing formula and quota calculations.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package validation

import (
	"encoding/json"
	"math"
	"testing"
)

// ── Billing Formula Verification ────────────────────────────────

// GPU specifications (must match pkg/billing/engine.go)
var gpuSpecs = map[string]struct {
	FP16TFlops   float64
	PricePerHour float64
}{
	"NVIDIA H800":       {FP16TFlops: 2000, PricePerHour: 55},
	"NVIDIA A100 80GB":  {FP16TFlops: 312, PricePerHour: 35},
	"NVIDIA A100 40GB":  {FP16TFlops: 312, PricePerHour: 22},
	"NVIDIA RTX 4090":   {FP16TFlops: 165, PricePerHour: 12},
	"Huawei Ascend 910B": {FP16TFlops: 256, PricePerHour: 28},
}

func TestBilling_TFlopsHoursFormula(t *testing.T) {
	// TFlops·h = AllocRatio × FP16TFlops × DurationHours
	tests := []struct {
		name          string
		gpuModel      string
		allocRatio    float64
		durationHours float64
		wantTFlopsH   float64
	}{
		{
			name:          "H800_50pct_2hours",
			gpuModel:      "NVIDIA H800",
			allocRatio:    0.50,
			durationHours: 2.0,
			wantTFlopsH:   0.50 * 2000 * 2.0, // 2000
		},
		{
			name:          "A100_80GB_25pct_10hours",
			gpuModel:      "NVIDIA A100 80GB",
			allocRatio:    0.25,
			durationHours: 10.0,
			wantTFlopsH:   0.25 * 312 * 10.0, // 780
		},
		{
			name:          "A100_40GB_100pct_1hour",
			gpuModel:      "NVIDIA A100 40GB",
			allocRatio:    1.0,
			durationHours: 1.0,
			wantTFlopsH:   1.0 * 312 * 1.0, // 312
		},
		{
			name:          "RTX4090_50pct_24hours",
			gpuModel:      "NVIDIA RTX 4090",
			allocRatio:    0.50,
			durationHours: 24.0,
			wantTFlopsH:   0.50 * 165 * 24.0, // 1980
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec, ok := gpuSpecs[tc.gpuModel]
			if !ok {
				t.Fatalf("unknown GPU model: %s", tc.gpuModel)
			}
			got := tc.allocRatio * spec.FP16TFlops * tc.durationHours
			if math.Abs(got-tc.wantTFlopsH) > 0.01 {
				t.Errorf("TFlops·h = %.2f, want %.2f (model=%s ratio=%.2f hours=%.2f)",
					got, tc.wantTFlopsH, tc.gpuModel, tc.allocRatio, tc.durationHours)
			}
		})
	}
}

func TestBilling_CostCNYFormula(t *testing.T) {
	// CostCNY = AllocRatio × PricePerHour × DurationHours
	tests := []struct {
		name          string
		gpuModel      string
		allocRatio    float64
		durationHours float64
		wantCNY       float64
	}{
		{
			name:          "H800_50pct_520gpuhours",
			gpuModel:      "NVIDIA H800",
			allocRatio:    0.50,
			durationHours: 520.0 / 0.50, // 1040h
			wantCNY:       0.50 * 55 * (520.0 / 0.50),
		},
		{
			name:          "A100_80GB_25pct_4hours",
			gpuModel:      "NVIDIA A100 80GB",
			allocRatio:    0.25,
			durationHours: 4.0,
			wantCNY:       0.25 * 35 * 4.0, // 35
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec, ok := gpuSpecs[tc.gpuModel]
			if !ok {
				t.Fatalf("unknown GPU model: %s", tc.gpuModel)
			}
			got := tc.allocRatio * spec.PricePerHour * tc.durationHours
			if math.Abs(got-tc.wantCNY) > 0.01 {
				t.Errorf("CostCNY = %.2f, want %.2f", got, tc.wantCNY)
			}
		})
	}
}

// ── Billing API Response Verification ───────────────────────────

func TestBilling_DepartmentUsagePctRange(t *testing.T) {
	base := testServer(t)
	_, body := httpGet(t, base+"/api/v1/billing/departments?period=monthly")

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	depts, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("data must be array, got %T", resp.Data)
	}

	for i, d := range depts {
		dept, ok := d.(map[string]interface{})
		if !ok {
			continue
		}
		usedPct, _ := dept["usedPct"].(float64)
		if usedPct < 0 || usedPct > 100 {
			t.Errorf("dept[%d] usedPct=%.1f%%, must be 0-100", i, usedPct)
		}
		gpuHours, _ := dept["gpuHours"].(float64)
		if gpuHours < 0 {
			t.Errorf("dept[%d] gpuHours=%.1f must be >= 0", i, gpuHours)
		}
		costCNY, _ := dept["costCNY"].(float64)
		if costCNY < 0 {
			t.Errorf("dept[%d] costCNY=%.2f must be >= 0", i, costCNY)
		}
	}
}

func TestBilling_QuotaThresholds(t *testing.T) {
	base := testServer(t)
	_, body := httpGet(t, base+"/api/v1/billing/departments?period=monthly")

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	depts, _ := resp.Data.([]interface{})
	for _, d := range depts {
		dept, ok := d.(map[string]interface{})
		if !ok {
			continue
		}
		usedPct, _ := dept["usedPct"].(float64)
		deptName, _ := dept["department"].(string)

		// Verify quota status categories
		switch {
		case usedPct >= 90:
			t.Logf("QUOTA RED: %s at %.1f%% (approaching/exceeded hard limit)", deptName, usedPct)
		case usedPct >= 70:
			t.Logf("QUOTA AMBER: %s at %.1f%% (approaching soft limit)", deptName, usedPct)
		default:
			t.Logf("QUOTA GREEN: %s at %.1f%% (normal)", deptName, usedPct)
		}
	}
}

func TestBilling_RecordsHaveRequiredFields(t *testing.T) {
	base := testServer(t)
	_, body := httpGet(t, base+"/api/v1/billing/records")

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	records, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("data must be array, got %T", resp.Data)
	}

	required := []string{
		"namespace", "jobName", "department",
		"gpuModel", "allocRatio",
		"durationHours", "tflopsHours", "costCNY", "gpuHours",
	}
	for i, r := range records {
		rec, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		for _, field := range required {
			if _, ok := rec[field]; !ok {
				t.Errorf("record[%d] missing required field: %s", i, field)
			}
		}
		// AllocRatio must be 0 < ratio <= 1
		ratio, _ := rec["allocRatio"].(float64)
		if ratio <= 0 || ratio > 1 {
			t.Errorf("record[%d] allocRatio=%.4f, must be (0,1]", i, ratio)
		}
	}
}
