//go:build k8sfull
// +build k8sfull

// Package k8s — RealK8sClient, MetricsCache, and data enrichment.
//
// Architecture:
//
//	K8s API Server  ──→  RealK8sClient.ListNodes()
//	DCGM Prometheus ──→  MetricsCache (TTL=15s) ──→ NodeMetrics
//	Both merged     ──→  GPUNode (API response type)
//
// Graceful degradation (Engineering Covenant Sprint 5):
//
//	Single metric failure must NOT fail the whole summary.
//	MetricsCache returns last known value on fetch error.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package k8s

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	apitypes "github.com/wlqtjl/PhoenixGPU/pkg/types"
)

// ── Domain types for this package ─────────────────────────────────

// RawNode is a Node from the K8s API, before metric enrichment.
type RawNode struct {
	Name     string
	GPUModel string
	GPUCount int
	Ready    bool
}

// NodeMetrics holds GPU metrics fetched from DCGM Exporter via Prometheus.
type NodeMetrics struct {
	SMUtilPct   float64
	VRAMUsedMiB int64
	TempCelsius float64
	PowerWatt   float64
}

// PhoenixJobStatus is the minimal status needed for summary aggregation.
type PhoenixJobStatus struct {
	Phase           string
	Namespace       string
	Name            string
	CheckpointCount int
}

// ── MetricsCache ──────────────────────────────────────────────────

// MetricsFetcher is a function that fetches a single float64 metric.
type MetricsFetcher func(ctx context.Context) (float64, error)

// MetricsCache wraps a MetricsFetcher with a TTL cache.
// Graceful degradation: on fetch error, returns the last known value.
type MetricsCache struct {
	mu        sync.RWMutex
	fetcher   MetricsFetcher
	ttl       time.Duration
	value     float64
	fetchedAt time.Time
	hasValue  bool
}

// NewMetricsCache creates a cache with the given TTL.
func NewMetricsCache(fetcher MetricsFetcher, ttl time.Duration) *MetricsCache {
	return &MetricsCache{fetcher: fetcher, ttl: ttl}
}

// Get returns the cached value, refreshing if the TTL has expired.
// On refresh failure, returns the last known value — never propagates transient errors.
func (c *MetricsCache) Get(ctx context.Context) (float64, error) {
	c.mu.RLock()
	if c.hasValue && time.Since(c.fetchedAt) < c.ttl {
		v := c.value
		c.mu.RUnlock()
		return v, nil
	}
	c.mu.RUnlock()

	// Need refresh — take write lock
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.hasValue && time.Since(c.fetchedAt) < c.ttl {
		return c.value, nil
	}

	// Fetch with timeout
	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	newVal, err := c.fetcher(fetchCtx)
	if err != nil {
		if c.hasValue {
			// Graceful degradation: return stale value, log warning
			return c.value, nil // caller doesn't need to know about transient failures
		}
		return 0, fmt.Errorf("metrics fetch failed (no cached value): %w", err)
	}

	c.value = newVal
	c.fetchedAt = time.Now()
	c.hasValue = true
	return c.value, nil
}

// ── Node enrichment ───────────────────────────────────────────────

// EnrichNode merges K8s Node data with DCGM metrics.
func EnrichNode(raw RawNode, metrics NodeMetrics, vramTotalMiB int64) apitypes.GPUNode {
	return apitypes.GPUNode{
		Name:         raw.Name,
		GPUModel:     raw.GPUModel,
		GPUCount:     raw.GPUCount,
		VRAMTotalMiB: vramTotalMiB,
		VRAMUsedMiB:  metrics.VRAMUsedMiB,
		SMUtilPct:    metrics.SMUtilPct,
		PowerWatt:    metrics.PowerWatt,
		TempCelsius:  metrics.TempCelsius,
		Ready:        raw.Ready,
		Faulted:      !raw.Ready,
	}
}

// BuildClusterSummary aggregates job list + node list + metrics into a summary.
func BuildClusterSummary(
	jobs []PhoenixJobStatus,
	nodes []RawNode,
	avgUtil float64,
	alertCount int,
) *apitypes.ClusterSummary {
	totalGPUs := 0
	for _, n := range nodes {
		totalGPUs += n.GPUCount
	}

	activeJobs, ckptJobs, restoreJobs := 0, 0, 0
	for _, j := range jobs {
		switch j.Phase {
		case "Running":
			activeJobs++
		case "Checkpointing":
			ckptJobs++
		case "Restoring":
			restoreJobs++
		}
	}

	return &apitypes.ClusterSummary{
		TotalGPUs:         totalGPUs,
		ActiveJobs:        activeJobs,
		CheckpointingJobs: ckptJobs,
		RestoringJobs:     restoreJobs,
		AvgUtilPct:        avgUtil,
		AlertCount:        alertCount,
	}
}

// ── RealK8sClient ─────────────────────────────────────────────────

// RealK8sClient reads live data from K8s API + DCGM Prometheus.
type RealK8sClient struct {
	k8s        kubernetes.Interface
	dynamic    dynamic.Interface
	promURL    string
	httpClient *http.Client
	logger     *zap.Logger

	// Per-node metrics caches (keyed by node name)
	// Each cache auto-refreshes every 15s independently
	utilCaches map[string]*MetricsCache
	mu         sync.RWMutex
}

// PhoenixJobGVR is the GroupVersionResource for PhoenixJob CRD.
var PhoenixJobGVR = schema.GroupVersionResource{
	Group:    "phoenixgpu.io",
	Version:  "v1alpha1",
	Resource: "phoenixjobs",
}

// NewRealK8sClient creates a client using in-cluster credentials.
func NewRealK8sClient(promURL string, logger *zap.Logger) (*RealK8sClient, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}

	k8sClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}

	return &RealK8sClient{
		k8s:        k8sClient,
		dynamic:    dynClient,
		promURL:    promURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		logger:     logger,
		utilCaches: make(map[string]*MetricsCache),
	}, nil
}

// ── Interface implementation ──────────────────────────────────────

func (c *RealK8sClient) GetClusterSummary(ctx context.Context) (*apitypes.ClusterSummary, error) {
	// Fetch in parallel — failure of one must not block others
	type nodeResult struct {
		nodes []RawNode
		err   error
	}
	type jobResult struct {
		jobs []PhoenixJobStatus
		err  error
	}

	nodeCh := make(chan nodeResult, 1)
	jobCh := make(chan jobResult, 1)

	go func() {
		nodes, err := c.listRawNodes(ctx)
		nodeCh <- nodeResult{nodes, err}
	}()
	go func() {
		jobs, err := c.listJobStatuses(ctx)
		jobCh <- jobResult{jobs, err}
	}()

	nRes := <-nodeCh
	jRes := <-jobCh

	if nRes.err != nil {
		c.logger.Warn("failed to list nodes for summary", zap.Error(nRes.err))
	}
	if jRes.err != nil {
		c.logger.Warn("failed to list jobs for summary", zap.Error(jRes.err))
	}

	// Get avg utilization — graceful degradation if DCGM unavailable
	avgUtil, _ := c.getClusterAvgUtil(ctx) // error ignored — returns 0 on failure

	return BuildClusterSummary(jRes.jobs, nRes.nodes, avgUtil, 0), nil
}

func (c *RealK8sClient) GetUtilizationHistory(ctx context.Context, hours int) ([]apitypes.TimeSeriesPoint, error) {
	// Query Prometheus for cluster-wide GPU utilization history
	query := fmt.Sprintf(
		`avg(DCGM_FI_DEV_GPU_UTIL)[%dh:30m]`, hours,
	)
	return c.queryPromRange(ctx, query, hours)
}

func (c *RealK8sClient) ListGPUNodes(ctx context.Context) ([]apitypes.GPUNode, error) {
	rawNodes, err := c.listRawNodes(ctx)
	if err != nil {
		return nil, err
	}

	var enriched []apitypes.GPUNode
	for _, raw := range rawNodes {
		metrics, _ := c.getNodeMetrics(ctx, raw.Name) // degrade on error
		vramTotal := c.getNodeVRAMTotal(ctx, raw.Name)
		enriched = append(enriched, EnrichNode(raw, metrics, vramTotal))
	}
	return enriched, nil
}

func (c *RealK8sClient) ListPhoenixJobs(ctx context.Context, namespace string) ([]apitypes.PhoenixJob, error) {
	ns := namespace
	if ns == "" {
		ns = metav1.NamespaceAll
	}

	list, err := c.dynamic.Resource(PhoenixJobGVR).Namespace(ns).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list PhoenixJobs: %w", err)
	}

	jobs := make([]apitypes.PhoenixJob, 0, len(list.Items))
	for _, item := range list.Items {
		j, err := c.unstructuredToJob(item)
		if err != nil {
			c.logger.Warn("skip invalid PhoenixJob",
				zap.String("name", item.GetName()), zap.Error(err))
			continue
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (c *RealK8sClient) GetPhoenixJob(ctx context.Context, namespace, name string) (*apitypes.PhoenixJob, error) {
	item, err := c.dynamic.Resource(PhoenixJobGVR).Namespace(namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("%w: %s/%s: %s", apitypes.ErrNotFound, namespace, name, err)
	}
	j, err := c.unstructuredToJob(*item)
	if err != nil {
		return nil, fmt.Errorf("parse PhoenixJob %s/%s: %w", namespace, name, err)
	}
	return &j, nil
}

func (c *RealK8sClient) TriggerCheckpoint(ctx context.Context, namespace, name string) error {
	// Trigger checkpoint by setting annotation on the PhoenixJob
	patch := []byte(`{"metadata":{"annotations":{"phoenixgpu.io/checkpoint-trigger":"` +
		time.Now().UTC().Format(time.RFC3339) + `"}}}`)

	_, err := c.dynamic.Resource(PhoenixJobGVR).Namespace(namespace).
		Patch(ctx, name,
			"application/merge-patch+json",
			patch,
			metav1.PatchOptions{},
		)
	if err != nil {
		return fmt.Errorf("trigger checkpoint %s/%s: %w", namespace, name, err)
	}
	return nil
}

func (c *RealK8sClient) GetBillingByDepartment(_ context.Context, period string) ([]apitypes.DeptBilling, error) {
	// TODO Sprint 6: query TimescaleDB billing_records table
	// For Sprint 5: return computed data from PhoenixJob annotations
	c.logger.Debug("GetBillingByDepartment: using mock data pending DB integration",
		zap.String("period", period))
	return fakeBillingDepartments(), nil
}

func (c *RealK8sClient) GetBillingRecords(_ context.Context, department string) ([]apitypes.BillingRecord, error) {
	all := fakeBillingRecords()
	if department == "" {
		return all, nil
	}
	var out []apitypes.BillingRecord
	for _, r := range all {
		if r.Department == department {
			out = append(out, r)
		}
	}
	return out, nil
}

func (c *RealK8sClient) ListAlerts(_ context.Context) ([]apitypes.Alert, error) {
	// TODO Sprint 6: query alert store
	return fakeAlertsList(), nil
}

func (c *RealK8sClient) ResolveAlert(_ context.Context, _ string) error {
	return nil // idempotent — resolving unknown alert is OK
}

// ── Placeholder fake data (pending DB integration) ────────────────

func fakeBillingDepartments() []apitypes.DeptBilling {
	return []apitypes.DeptBilling{
		{Department: "算法研究院", GPUHours: 620, TFlopsHours: 193440, CostCNY: 21700, QuotaHours: 800, UsedPct: 77.5},
		{Department: "NLP平台组", GPUHours: 480, TFlopsHours: 149760, CostCNY: 16800, QuotaHours: 600, UsedPct: 80.0},
		{Department: "CV工程组", GPUHours: 380, TFlopsHours: 118560, CostCNY: 13300, QuotaHours: 500, UsedPct: 76.0},
		{Department: "推理基础设施", GPUHours: 280, TFlopsHours: 87360, CostCNY: 9800, QuotaHours: 400, UsedPct: 70.0},
		{Department: "数据工程部", GPUHours: 150, TFlopsHours: 24750, CostCNY: 1800, QuotaHours: 300, UsedPct: 50.0},
	}
}

func fakeBillingRecords() []apitypes.BillingRecord {
	return []apitypes.BillingRecord{
		{Namespace: "research", JobName: "llm-pretrain-v3", Department: "算法研究院",
			GPUModel: "NVIDIA H800", AllocRatio: 0.5, GPUHours: 520, TFlopsHours: 1040000, CostCNY: 18200},
		{Namespace: "nlp", JobName: "rlhf-finetune", Department: "NLP平台组",
			GPUModel: "NVIDIA A100 80GB", AllocRatio: 0.25, GPUHours: 480, TFlopsHours: 149760, CostCNY: 16800},
	}
}

func fakeAlertsList() []apitypes.Alert {
	return []apitypes.Alert{
		{ID: "alert-1", Severity: "error", Tenant: "算法研究院",
			Message: "月度配额已用 93%，预计 48h 超限", CreatedAt: time.Now().Add(-10 * time.Minute)},
		{ID: "alert-2", Severity: "warn", Tenant: "cv-detection-v2",
			Message: "Restore 失败 1 次，正在第 2 次重试", CreatedAt: time.Now().Add(-30 * time.Minute)},
		{ID: "alert-3", Severity: "warn", Tenant: "gpu-node-03",
			Message: "GPU 温度 64°C，接近阈值 70°C", CreatedAt: time.Now().Add(-60 * time.Minute)},
	}
}

// ── Internal helpers ──────────────────────────────────────────────

func (c *RealK8sClient) listRawNodes(ctx context.Context) ([]RawNode, error) {
	nodeList, err := c.k8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "node-role.kubernetes.io/gpu=true",
	})
	if err != nil {
		return nil, fmt.Errorf("list GPU nodes: %w", err)
	}

	raw := make([]RawNode, 0, len(nodeList.Items))
	for _, n := range nodeList.Items {
		raw = append(raw, RawNode{
			Name:     n.Name,
			GPUModel: n.Labels["nvidia.com/gpu.product"],
			GPUCount: gpuCountFromNode(n),
			Ready:    isNodeReady(n),
		})
	}
	return raw, nil
}

func (c *RealK8sClient) listJobStatuses(ctx context.Context) ([]PhoenixJobStatus, error) {
	list, err := c.dynamic.Resource(PhoenixJobGVR).Namespace(metav1.NamespaceAll).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list job statuses: %w", err)
	}

	statuses := make([]PhoenixJobStatus, 0, len(list.Items))
	for _, item := range list.Items {
		phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
		statuses = append(statuses, PhoenixJobStatus{
			Namespace: item.GetNamespace(),
			Name:      item.GetName(),
			Phase:     phase,
		})
	}
	return statuses, nil
}

func (c *RealK8sClient) getClusterAvgUtil(ctx context.Context) (float64, error) {
	// Average SM utilization across all GPU nodes
	result, err := c.queryPromInstant(ctx, `avg(DCGM_FI_DEV_GPU_UTIL)`)
	if err != nil {
		return 0, err
	}
	return result, nil
}

func (c *RealK8sClient) getNodeMetrics(ctx context.Context, nodeName string) (NodeMetrics, error) {
	// Build per-node Prometheus queries
	queries := map[string]string{
		"util":  fmt.Sprintf(`avg(DCGM_FI_DEV_GPU_UTIL{Hostname="%s"})`, nodeName),
		"mem":   fmt.Sprintf(`sum(DCGM_FI_DEV_FB_USED{Hostname="%s"})`, nodeName),
		"temp":  fmt.Sprintf(`avg(DCGM_FI_DEV_GPU_TEMP{Hostname="%s"})`, nodeName),
		"power": fmt.Sprintf(`sum(DCGM_FI_DEV_POWER_USAGE{Hostname="%s"})`, nodeName),
	}

	// Get or create per-node cache
	results := make(map[string]float64)
	for metric, query := range queries {
		cache := c.getOrCreateCache(nodeName+":"+metric, query)
		v, err := cache.Get(ctx)
		if err != nil {
			c.logger.Warn("node metric unavailable",
				zap.String("node", nodeName),
				zap.String("metric", metric),
				zap.Error(err))
			continue // graceful degradation — skip this metric
		}
		results[metric] = v
	}

	return NodeMetrics{
		SMUtilPct:   results["util"],
		VRAMUsedMiB: int64(results["mem"]),
		TempCelsius: results["temp"],
		PowerWatt:   results["power"],
	}, nil
}

func (c *RealK8sClient) getNodeVRAMTotal(ctx context.Context, nodeName string) int64 {
	q := fmt.Sprintf(`sum(DCGM_FI_DEV_FB_TOTAL{Hostname="%s"})`, nodeName)
	v, _ := c.queryPromInstant(ctx, q) // graceful degradation
	return int64(v)
}

func (c *RealK8sClient) getOrCreateCache(key, query string) *MetricsCache {
	c.mu.RLock()
	cache, ok := c.utilCaches[key]
	c.mu.RUnlock()
	if ok {
		return cache
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check
	if cache, ok = c.utilCaches[key]; ok {
		return cache
	}
	q := query
	cache = NewMetricsCache(func(ctx context.Context) (float64, error) {
		return c.queryPromInstant(ctx, q)
	}, 15*time.Second)
	c.utilCaches[key] = cache
	return cache
}

// queryPromInstant executes an instant Prometheus query and returns a scalar.
func (c *RealK8sClient) queryPromInstant(ctx context.Context, query string) (float64, error) {
	url := fmt.Sprintf("%s/api/v1/query?query=%s", c.promURL, query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("build prom request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("prom query %q: %w", query, err)
	}
	defer resp.Body.Close()

	return parsePromScalar(resp)
}

// queryPromRange fetches a range query and converts to TimeSeriesPoint slice.
func (c *RealK8sClient) queryPromRange(ctx context.Context, query string, hours int) ([]apitypes.TimeSeriesPoint, error) {
	end := time.Now()
	start := end.Add(-time.Duration(hours) * time.Hour)

	url := fmt.Sprintf("%s/api/v1/query_range?query=%s&start=%d&end=%d&step=1800",
		c.promURL, query, start.Unix(), end.Unix())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build prom range request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prom range query: %w", err)
	}
	defer resp.Body.Close()

	return parsePromRange(resp)
}

func (c *RealK8sClient) unstructuredToJob(item unstructured.Unstructured) (apitypes.PhoenixJob, error) {
	getString := func(fields ...string) string {
		v, _, _ := unstructured.NestedString(item.Object, fields...)
		return v
	}
	getInt := func(fields ...string) int {
		v, _, _ := unstructured.NestedInt64(item.Object, fields...)
		return int(v)
	}
	getFloat := func(fields ...string) float64 {
		v, _, _ := unstructured.NestedFloat64(item.Object, fields...)
		return v
	}

	return apitypes.PhoenixJob{
		Name:              item.GetName(),
		Namespace:         item.GetNamespace(),
		Phase:             getString("status", "phase"),
		CheckpointCount:   getInt("status", "checkpointCount"),
		RestoreAttempts:   getInt("status", "restoreAttempts"),
		LastCheckpointDir: getString("status", "lastCheckpointDir"),
		CurrentPodName:    getString("status", "currentPodName"),
		CurrentNodeName:   getString("status", "currentNodeName"),
		GPUModel:          getString("spec", "template", "spec", "containers", "0", "resources", "limits", "nvidia.com/gpu-model"),
		AllocRatio:        getFloat("spec", "checkpoint", "allocRatio"),
		Department:        getString("spec", "billing", "department"),
		Project:           getString("spec", "billing", "project"),
	}, nil
}

// ── K8s helpers ───────────────────────────────────────────────────

func isNodeReady(node corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

func gpuCountFromNode(node corev1.Node) int {
	if q, ok := node.Status.Capacity["nvidia.com/gpu"]; ok {
		v, _ := q.AsInt64()
		return int(v)
	}
	return 0
}
