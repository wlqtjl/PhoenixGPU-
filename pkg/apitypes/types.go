package apitypes

import (
	"errors"
	"time"
)

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
	Name               string            `json:"name"`
	Namespace          string            `json:"namespace"`
	Phase              string            `json:"phase"`
	CheckpointCount    int               `json:"checkpointCount"`
	RestoreAttempts    int               `json:"restoreAttempts"`
	LastCheckpointTime *time.Time        `json:"lastCheckpointTime"`
	LastCheckpointDir  string            `json:"lastCheckpointDir"`
	CurrentPodName     string            `json:"currentPodName"`
	CurrentNodeName    string            `json:"currentNodeName"`
	GPUModel           string            `json:"gpuModel"`
	AllocRatio         float64           `json:"allocRatio"`
	Department         string            `json:"department"`
	Project            string            `json:"project"`
	StartedAt          time.Time         `json:"startedAt"`
	Snapshots          []SnapshotSummary `json:"snapshots"`
}

type SnapshotSummary struct {
	Seq        int       `json:"seq"`
	CreatedAt  time.Time `json:"createdAt"`
	DurationMS int64     `json:"durationMS"`
	SizeBytes  int64     `json:"sizeBytes"`
}

type DeptBilling struct {
	Department  string  `json:"department"`
	GPUHours    float64 `json:"gpuHours"`
	TFlopsHours float64 `json:"tflopsHours"`
	CostCNY     float64 `json:"costCNY"`
	QuotaHours  float64 `json:"quotaHours"`
	UsedPct     float64 `json:"usedPct"`
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
	Severity  string    `json:"severity"`
	Tenant    string    `json:"tenant"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
	Resolved  bool      `json:"resolved"`
}

type TimeSeriesPoint struct {
	TS    time.Time `json:"ts"`
	Value float64   `json:"value"`
}

var ErrNotFound = errors.New("not found")
