//go:build billingfull
// +build billingfull

// Package billing implements the PhoenixGPU billing engine.
// It collects GPU usage metrics and produces cost records denominated in TFlops·h.
//
// PhoenixGPU Core — Self-developed
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package billing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// GPUSpec defines the billing properties of a GPU model.
type GPUSpec struct {
	Model        string
	FP16TFlops   float64 // FP16 peak TFlops — the "currency unit"
	PricePerHour float64 // CNY per physical GPU per hour (internal cost)
}

// Well-known GPU specs for TFlops normalisation.
// TFlops·h = AllocRatio × GPUSpec.FP16TFlops × DurationHours
var KnownGPUs = map[string]GPUSpec{
	"NVIDIA-H800":      {Model: "NVIDIA H800", FP16TFlops: 2000, PricePerHour: 55},
	"NVIDIA-A100-80GB": {Model: "NVIDIA A100 80GB", FP16TFlops: 312, PricePerHour: 35},
	"NVIDIA-A100-40GB": {Model: "NVIDIA A100 40GB", FP16TFlops: 312, PricePerHour: 22},
	"NVIDIA-RTX-4090":  {Model: "NVIDIA RTX 4090", FP16TFlops: 165, PricePerHour: 12},
	"Huawei-910B":      {Model: "Huawei Ascend 910B", FP16TFlops: 256, PricePerHour: 28},
}

// UsageRecord represents a single period of GPU usage by one workload.
type UsageRecord struct {
	// Identity
	Namespace  string
	PodName    string
	JobName    string
	Department string
	Project    string
	CostCenter string

	// Resource
	GPUModel   string
	NodeName   string
	AllocRatio float64 // fraction of physical GPU allocated (0.0–1.0)

	// Time
	StartedAt time.Time
	EndedAt   time.Time

	// Computed (filled by Engine.Compute)
	DurationHours float64
	TFlopsHours   float64 // = AllocRatio × GPUSpec.FP16TFlops × DurationHours
	CostCNY       float64 // = AllocRatio × GPUSpec.PricePerHour × DurationHours
	GPUHours      float64 // = AllocRatio × DurationHours (legacy metric)
}

// QuotaPolicy defines limits for a tenant.
type QuotaPolicy struct {
	TenantType string // cluster | dept | project | user
	TenantID   string
	// Soft limit triggers an alert.
	SoftLimitGPUHours float64
	// Hard limit causes scheduling rejection.
	HardLimitGPUHours float64
	Period            string // daily | weekly | monthly
}

// QuotaStatus tracks current usage against a policy.
type QuotaStatus struct {
	Policy       QuotaPolicy
	UsedGPUHours float64
	UsedPct      float64
	AlertFired   bool
}

// Store is the persistence interface for billing records.
// Implemented by PostgreSQL in production, in-memory map in tests.
type Store interface {
	SaveRecord(ctx context.Context, r UsageRecord) error
	GetQuotaStatus(ctx context.Context, tenantID, period string) (*QuotaStatus, error)
	ListRecords(ctx context.Context, filter RecordFilter) ([]UsageRecord, error)
	SumByDepartment(ctx context.Context, period string) (map[string]float64, error)
}

// RecordFilter constrains ListRecords results.
type RecordFilter struct {
	Namespace  string
	Department string
	Project    string
	From, To   time.Time
}

// Engine is the central billing component.
// It computes TFlops·h from raw usage records and enforces quotas.
type Engine struct {
	store      Store
	logger     *zap.Logger
	alertHooks []AlertHook
}

// AlertHook is called when a quota soft limit is exceeded.
// It returns an error so the engine can retry on transient failures.
type AlertHook func(ctx context.Context, status QuotaStatus) error

// NewEngine creates a billing Engine.
func NewEngine(store Store, logger *zap.Logger) *Engine {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Engine{store: store, logger: logger}
}

// RegisterAlertHook adds a callback for quota alerts (email, DingTalk, etc.).
func (e *Engine) RegisterAlertHook(hook AlertHook) {
	e.alertHooks = append(e.alertHooks, hook)
}

// Compute fills the derived fields of a UsageRecord.
// Call this before SaveRecord.
func (e *Engine) Compute(r *UsageRecord) error {
	if r.EndedAt.IsZero() {
		r.EndedAt = time.Now()
	}
	if r.StartedAt.IsZero() || r.EndedAt.Before(r.StartedAt) {
		return fmt.Errorf("invalid time range: start=%v end=%v", r.StartedAt, r.EndedAt)
	}
	if r.AllocRatio <= 0 || r.AllocRatio > 1.0 {
		return fmt.Errorf("invalid alloc ratio %.3f: must be in (0, 1]", r.AllocRatio)
	}

	spec, ok := KnownGPUs[r.GPUModel]
	if !ok {
		// Unknown model — use conservative defaults, log warning
		e.logger.Warn("unknown GPU model, using default pricing",
			zap.String("model", r.GPUModel))
		spec = GPUSpec{FP16TFlops: 100, PricePerHour: 10}
	}

	r.DurationHours = r.EndedAt.Sub(r.StartedAt).Hours()
	r.GPUHours = r.AllocRatio * r.DurationHours
	r.TFlopsHours = r.AllocRatio * spec.FP16TFlops * r.DurationHours
	r.CostCNY = r.AllocRatio * spec.PricePerHour * r.DurationHours

	return nil
}

// Record computes and persists a usage record, then checks quotas.
func (e *Engine) Record(ctx context.Context, r UsageRecord) error {
	if err := e.Compute(&r); err != nil {
		return fmt.Errorf("compute billing record: %w", err)
	}

	if err := e.store.SaveRecord(ctx, r); err != nil {
		return fmt.Errorf("save billing record: %w", err)
	}

	e.logger.Info("billing record saved",
		zap.String("pod", r.PodName),
		zap.String("namespace", r.Namespace),
		zap.String("dept", r.Department),
		zap.Float64("gpuHours", r.GPUHours),
		zap.Float64("tflopsHours", r.TFlopsHours),
		zap.Float64("costCNY", r.CostCNY))

	// Async quota check — don't block the caller, but use a derived context
	go e.checkQuota(ctx, r)

	return nil
}

// checkQuota evaluates usage against the tenant's quota policy and fires alerts.
// It retries transient failures with exponential backoff.
func (e *Engine) checkQuota(ctx context.Context, r UsageRecord) {
	tenantID := r.Department
	if tenantID == "" {
		tenantID = r.Namespace
	}

	// Retry GetQuotaStatus with exponential backoff (3 attempts)
	var status *QuotaStatus
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		status, err = e.store.GetQuotaStatus(ctx, tenantID, "monthly")
		if err == nil {
			break
		}
		e.logger.Warn("quota status query failed, retrying",
			zap.String("tenant", tenantID),
			zap.Int("attempt", attempt+1),
			zap.Error(err))
		select {
		case <-ctx.Done():
			e.logger.Error("quota check cancelled",
				zap.String("tenant", tenantID),
				zap.Error(ctx.Err()))
			return
		case <-time.After(time.Duration(1<<uint(attempt)) * time.Second): // 1s, 2s, 4s
		}
	}
	if err != nil {
		e.logger.Error("failed to get quota status after retries",
			zap.String("tenant", tenantID),
			zap.Error(err))
		return
	}
	if status == nil {
		return
	}

	status.UsedPct = 0
	if status.Policy.SoftLimitGPUHours > 0 {
		status.UsedPct = status.UsedGPUHours / status.Policy.SoftLimitGPUHours * 100
	}

	if status.UsedPct >= 80 && !status.AlertFired {
		e.logger.Warn("quota soft limit approaching",
			zap.String("tenant", tenantID),
			zap.Float64("usedPct", status.UsedPct))
		for i, hook := range e.alertHooks {
			e.invokeHookWithRetry(ctx, i, hook, *status)
		}
	}
}

// invokeHookWithRetry calls an alert hook with panic recovery and retry.
func (e *Engine) invokeHookWithRetry(ctx context.Context, idx int, hook AlertHook, status QuotaStatus) {
	const maxRetries = 2
	for attempt := 0; attempt <= maxRetries; attempt++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					e.logger.Error("alert hook panicked",
						zap.Int("hookIndex", idx),
						zap.Int("attempt", attempt),
						zap.Any("panic", r))
				}
			}()
			if err := hook(ctx, status); err != nil {
				e.logger.Warn("alert hook failed",
					zap.Int("hookIndex", idx),
					zap.Int("attempt", attempt),
					zap.Error(err))
				return
			}
			// success — no more retries needed; set attempt past max to exit loop
			attempt = maxRetries + 1
		}()
		if attempt > maxRetries {
			break
		}
		// Backoff before retry
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(1<<uint(attempt)) * 500 * time.Millisecond): // 500ms, 1s
		}
	}
}

// ── In-memory Store for tests ─────────────────────────────────────

// MemoryStore is an in-memory Store implementation for unit testing.
type MemoryStore struct {
	mu      sync.Mutex
	records []UsageRecord
	quotas  map[string]QuotaPolicy
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{quotas: make(map[string]QuotaPolicy)}
}

func (m *MemoryStore) SaveRecord(_ context.Context, r UsageRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, r)
	return nil
}

func (m *MemoryStore) GetQuotaStatus(_ context.Context, tenantID, _ string) (*QuotaStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	policy, ok := m.quotas[tenantID]
	if !ok {
		return nil, nil
	}
	var used float64
	for _, r := range m.records {
		if r.Department == tenantID || r.Namespace == tenantID {
			used += r.GPUHours
		}
	}
	return &QuotaStatus{Policy: policy, UsedGPUHours: used}, nil
}

func (m *MemoryStore) ListRecords(_ context.Context, f RecordFilter) ([]UsageRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []UsageRecord
	for _, r := range m.records {
		if f.Department != "" && r.Department != f.Department {
			continue
		}
		if f.Namespace != "" && r.Namespace != f.Namespace {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (m *MemoryStore) SumByDepartment(_ context.Context, _ string) (map[string]float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	totals := make(map[string]float64)
	for _, r := range m.records {
		totals[r.Department] += r.GPUHours
	}
	return totals, nil
}
