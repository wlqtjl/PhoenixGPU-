// Package internal — K8sClientInterface and fake implementation.
//
// The interface decouples handlers from real K8s/Prometheus dependencies.
// FakeK8sClient is used in unit tests; RealK8sClient in production.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package internal

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ── Domain types (mirror webui/src/api/client.ts) ─────────────────

type ClusterSummary struct {
	TotalGPUs         int     `json:"totalGPUs"`
	ActiveJobs        int     `json:"activeJobs"`
	CheckpointingJobs int     `json:"checkpointingJobs"`
	RestoringJobs     int     `json:"restoringJobs"`
	AvgUtilPct        float64 `json:"avgUtilPct"`
	AlertCount        int     `json:"alertCount"`
	TotalGPUHours     float64 `json:"totalGPUHours"`
	TotalCostCNY      float64 `json:"totalCostCNY"`
}

type GPUNode struct {
	Name         string  `json:"name"`
	GPUModel     string  `json:"gpuModel"`
	GPUCount     int     `json:"gpuCount"`
	VRAMTotalMiB int64   `json:"vramTotalMiB"`
	VRAMUsedMiB  int64   `json:"vramUsedMiB"`
	SMUtilPct    float64 `json:"smUtilPct"`
	PowerWatt    float64 `json:"powerWatt"`
	TempCelsius  float64 `json:"tempCelsius"`
	Ready        bool    `json:"ready"`
	Faulted      bool    `json:"faulted"`
}

type PhoenixJob struct {
	Name               string       `json:"name"`
	Namespace          string       `json:"namespace"`
	Phase              string       `json:"phase"`
	CheckpointCount    int          `json:"checkpointCount"`
	RestoreAttempts    int          `json:"restoreAttempts"`
	LastCheckpointTime *time.Time   `json:"lastCheckpointTime"`
	LastCheckpointDir  string       `json:"lastCheckpointDir"`
	CurrentPodName     string       `json:"currentPodName"`
	CurrentNodeName    string       `json:"currentNodeName"`
	GPUModel           string       `json:"gpuModel"`
	AllocRatio         float64      `json:"allocRatio"`
	Department         string       `json:"department"`
	Project            string       `json:"project"`
	StartedAt          time.Time    `json:"startedAt"`
	Snapshots          []SnapshotSummary `json:"snapshots"`
}

type SnapshotSummary struct {
	Seq        int       `json:"seq"`
	CreatedAt  time.Time `json:"createdAt"`
	DurationMS int64     `json:"durationMS"`
	SizeBytes  int64     `json:"sizeBytes"`
}

type DeptBilling struct {
	Department string  `json:"department"`
	GPUHours   float64 `json:"gpuHours"`
	TFlopsHours float64 `json:"tflopsHours"`
	CostCNY    float64 `json:"costCNY"`
	QuotaHours float64 `json:"quotaHours"`
	UsedPct    float64 `json:"usedPct"`
}

type BillingRecord struct {
	Namespace     string    `json:"namespace"`
	PodName       string    `json:"podName"`
	JobName       string    `json:"jobName"`
	Department    string    `json:"department"`
	GPUModel      string    `json:"gpuModel"`
	AllocRatio    float64   `json:"allocRatio"`
	StartedAt     time.Time `json:"startedAt"`
	EndedAt       time.Time `json:"endedAt"`
	DurationHours float64   `json:"durationHours"`
	TFlopsHours   float64   `json:"tflopsHours"`
	CostCNY       float64   `json:"costCNY"`
	GPUHours      float64   `json:"gpuHours"`
}

type Alert struct {
	ID        string    `json:"id"`
	Severity  string    `json:"severity"` // info | warn | error
	Tenant    string    `json:"tenant"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
	Resolved  bool      `json:"resolved"`
}

type TimeSeriesPoint struct {
	TS    time.Time `json:"ts"`
	Value float64   `json:"value"`
}

// ── Interface ─────────────────────────────────────────────────────

// K8sClientInterface abstracts all data access.
// Implemented by RealK8sClient (production) and FakeK8sClient (tests).
type K8sClientInterface interface {
	GetClusterSummary(ctx context.Context) (*ClusterSummary, error)
	GetUtilizationHistory(ctx context.Context, hours int) ([]TimeSeriesPoint, error)
	ListGPUNodes(ctx context.Context) ([]GPUNode, error)
	ListPhoenixJobs(ctx context.Context, namespace string) ([]PhoenixJob, error)
	GetPhoenixJob(ctx context.Context, namespace, name string) (*PhoenixJob, error)
	TriggerCheckpoint(ctx context.Context, namespace, name string) error
	GetBillingByDepartment(ctx context.Context, period string) ([]DeptBilling, error)
	GetBillingRecords(ctx context.Context, department string) ([]BillingRecord, error)
	ListAlerts(ctx context.Context) ([]Alert, error)
	ResolveAlert(ctx context.Context, id string) error
}

// ── ErrNotFound ───────────────────────────────────────────────────

var ErrNotFound = errors.New("not found")

func isNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// ── FakeK8sClient (for tests) ─────────────────────────────────────

type FakeK8sClient struct {
	alerts map[string]*Alert
}

func NewFakeK8sClient() *FakeK8sClient {
	f := &FakeK8sClient{alerts: make(map[string]*Alert)}
	// Seed some alerts
	for _, a := range fakeAlerts() {
		a := a
		f.alerts[a.ID] = &a
	}
	return f
}

func (f *FakeK8sClient) GetClusterSummary(_ context.Context) (*ClusterSummary, error) {
	return &ClusterSummary{
		TotalGPUs: 32, ActiveJobs: 18, CheckpointingJobs: 2,
		RestoringJobs: 1, AvgUtilPct: 74.2, AlertCount: 3,
		TotalGPUHours: 1840, TotalCostCNY: 64400,
	}, nil
}

func (f *FakeK8sClient) GetUtilizationHistory(_ context.Context, hours int) ([]TimeSeriesPoint, error) {
	pts := make([]TimeSeriesPoint, hours*2)
	for i := range pts {
		pts[i] = TimeSeriesPoint{
			TS:    time.Now().Add(-time.Duration(len(pts)-i) * 30 * time.Minute),
			Value: 55 + math.Sin(float64(i)*0.3)*18,
		}
	}
	return pts, nil
}

func (f *FakeK8sClient) ListGPUNodes(_ context.Context) ([]GPUNode, error) {
	return []GPUNode{
		{Name:"gpu-node-01", GPUModel:"NVIDIA A100 80GB", GPUCount:8,
			VRAMTotalMiB:81920, VRAMUsedMiB:61440, SMUtilPct:82, PowerWatt:380, TempCelsius:72, Ready:true},
		{Name:"gpu-node-02", GPUModel:"NVIDIA A100 80GB", GPUCount:8,
			VRAMTotalMiB:81920, VRAMUsedMiB:49152, SMUtilPct:65, PowerWatt:310, TempCelsius:68, Ready:true},
		{Name:"gpu-node-03", GPUModel:"NVIDIA A100 40GB", GPUCount:8,
			VRAMTotalMiB:40960, VRAMUsedMiB:32768, SMUtilPct:71, PowerWatt:270, TempCelsius:64, Ready:true},
		{Name:"gpu-node-04", GPUModel:"NVIDIA H800", GPUCount:8,
			VRAMTotalMiB:81920, VRAMUsedMiB:73728, SMUtilPct:91, PowerWatt:420, TempCelsius:79, Ready:true},
	}, nil
}

func (f *FakeK8sClient) ListPhoenixJobs(_ context.Context, namespace string) ([]PhoenixJob, error) {
	all := fakeJobs()
	if namespace == "" {
		return all, nil
	}
	var filtered []PhoenixJob
	for _, j := range all {
		if j.Namespace == namespace {
			filtered = append(filtered, j)
		}
	}
	return filtered, nil
}

func (f *FakeK8sClient) GetPhoenixJob(_ context.Context, namespace, name string) (*PhoenixJob, error) {
	for _, j := range fakeJobs() {
		if j.Namespace == namespace && j.Name == name {
			return &j, nil
		}
	}
	return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, namespace, name)
}

func (f *FakeK8sClient) TriggerCheckpoint(_ context.Context, namespace, name string) error {
	for _, j := range fakeJobs() {
		if j.Namespace == namespace && j.Name == name {
			return nil
		}
	}
	return fmt.Errorf("%w: %s/%s", ErrNotFound, namespace, name)
}

func (f *FakeK8sClient) GetBillingByDepartment(_ context.Context, _ string) ([]DeptBilling, error) {
	return []DeptBilling{
		{Department:"算法研究院", GPUHours:620, TFlopsHours:193440, CostCNY:21700, QuotaHours:800, UsedPct:77.5},
		{Department:"NLP平台组",  GPUHours:480, TFlopsHours:149760, CostCNY:16800, QuotaHours:600, UsedPct:80.0},
		{Department:"CV工程组",   GPUHours:380, TFlopsHours:118560, CostCNY:13300, QuotaHours:500, UsedPct:76.0},
		{Department:"推理基础设施",GPUHours:280, TFlopsHours:87360,  CostCNY:9800,  QuotaHours:400, UsedPct:70.0},
		{Department:"数据工程部", GPUHours:150, TFlopsHours:24750,  CostCNY:1800,  QuotaHours:300, UsedPct:50.0},
	}, nil
}

func (f *FakeK8sClient) GetBillingRecords(_ context.Context, department string) ([]BillingRecord, error) {
	all := []BillingRecord{
		{Namespace:"research", JobName:"llm-pretrain-v3", Department:"算法研究院",
			GPUModel:"NVIDIA H800", AllocRatio:0.5, GPUHours:520, TFlopsHours:1040000, CostCNY:18200},
		{Namespace:"nlp", JobName:"rlhf-finetune", Department:"NLP平台组",
			GPUModel:"NVIDIA A100 80GB", AllocRatio:0.25, GPUHours:480, TFlopsHours:149760, CostCNY:16800},
	}
	if department == "" {
		return all, nil
	}
	var out []BillingRecord
	for _, r := range all {
		if r.Department == department {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *FakeK8sClient) ListAlerts(_ context.Context) ([]Alert, error) {
	var out []Alert
	for _, a := range f.alerts {
		out = append(out, *a)
	}
	return out, nil
}

func (f *FakeK8sClient) ResolveAlert(_ context.Context, id string) error {
	if a, ok := f.alerts[id]; ok {
		a.Resolved = true
		return nil
	}
	return nil // idempotent — resolving unknown alert is OK
}

// ── Fake data helpers ─────────────────────────────────────────────

func fakeJobs() []PhoenixJob {
	t := time.Now().Add(-30 * time.Minute)
	return []PhoenixJob{
		{Name:"llm-pretrain-v3", Namespace:"research", Phase:"Running",
			CheckpointCount:24, GPUModel:"NVIDIA H800", AllocRatio:0.5,
			Department:"算法研究院", Project:"LLM预训练",
			LastCheckpointTime:&t, CurrentNodeName:"gpu-node-04",
			StartedAt: time.Now().Add(-72*time.Hour),
			Snapshots: []SnapshotSummary{
				{Seq:24, CreatedAt:t, DurationMS:8200, SizeBytes:2_800_000_000},
				{Seq:23, CreatedAt:t.Add(-5*time.Minute), DurationMS:8150, SizeBytes:2_780_000_000},
			}},
		{Name:"rlhf-finetune", Namespace:"nlp", Phase:"Checkpointing",
			CheckpointCount:8, GPUModel:"NVIDIA A100 80GB", AllocRatio:0.25,
			Department:"NLP平台组", Project:"RLHF微调",
			LastCheckpointTime:&t, CurrentNodeName:"gpu-node-01",
			StartedAt: time.Now().Add(-24*time.Hour), Snapshots:nil},
		{Name:"cv-detection-v2", Namespace:"cv", Phase:"Restoring",
			CheckpointCount:12, RestoreAttempts:1,
			GPUModel:"NVIDIA A100 40GB", AllocRatio:0.5,
			Department:"CV工程组", Project:"目标检测2.0",
			CurrentNodeName:"gpu-node-02",
			StartedAt: time.Now().Add(-48*time.Hour), Snapshots:nil},
	}
}

func fakeAlerts() []Alert {
	return []Alert{
		{ID:"alert-1", Severity:"error", Tenant:"算法研究院",
			Message:"月度配额已用 93%，预计 48h 超限", CreatedAt:time.Now().Add(-10*time.Minute)},
		{ID:"alert-2", Severity:"warn", Tenant:"cv-detection-v2",
			Message:"Restore 失败 1 次，正在第 2 次重试", CreatedAt:time.Now().Add(-30*time.Minute)},
		{ID:"alert-3", Severity:"warn", Tenant:"gpu-node-03",
			Message:"GPU 温度 64°C，接近阈值 70°C", CreatedAt:time.Now().Add(-60*time.Minute)},
	}
}

// ── Utilities ─────────────────────────────────────────────────────

func parsePositiveInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid positive int: %q", s)
	}
	return n, nil
}
