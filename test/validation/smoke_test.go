// Package validation -- deployment verification test suite.
//
// These tests validate a live or mock PhoenixGPU deployment.
// Run against mock data: go test ./test/validation/... -v
// Run against live API:  go test ./test/validation/... -v -api-url=http://localhost:8090
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package validation

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func httpGet(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func httpPost(t *testing.T, url string, bodyStr string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if bodyStr != "" {
		reader = strings.NewReader(bodyStr)
	}
	resp, err := http.Post(url, "application/json", reader)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// ── 1. Health Probes ──────────────────────────────────────────────

func TestSmoke_Healthz(t *testing.T) {
	base := testServer(t)
	status, body := httpGet(t, base+"/healthz")
	if status != http.StatusOK {
		t.Errorf("/healthz status=%d want=200, body=%s", status, body)
	}
}

func TestSmoke_Readyz(t *testing.T) {
	base := testServer(t)
	status, body := httpGet(t, base+"/readyz")
	if status != http.StatusOK {
		t.Errorf("/readyz status=%d want=200, body=%s", status, body)
	}
}

func TestSmoke_Metrics(t *testing.T) {
	base := testServer(t)
	status, _ := httpGet(t, base+"/metrics")
	if status != http.StatusOK {
		t.Errorf("/metrics status=%d want=200", status)
	}
}

// ── 2. Cluster Summary ───────────────────────────────────────────

func TestSmoke_ClusterSummary(t *testing.T) {
	base := testServer(t)
	status, body := httpGet(t, base+"/api/v1/cluster/summary")
	if status != http.StatusOK {
		t.Fatalf("cluster/summary status=%d want=200, body=%s", status, body)
	}

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data must be object, got %T", resp.Data)
	}

	required := []string{"totalGPUs", "activeJobs", "avgUtilPct", "alertCount", "totalGPUHours", "totalCostCNY"}
	for _, field := range required {
		if _, ok := data[field]; !ok {
			t.Errorf("cluster summary missing field: %s", field)
		}
	}

	gpus, _ := data["totalGPUs"].(float64)
	if gpus <= 0 {
		t.Errorf("totalGPUs=%v, want > 0", data["totalGPUs"])
	}
}

// ── 3. Utilization History ───────────────────────────────────────

func TestSmoke_UtilizationHistory(t *testing.T) {
	base := testServer(t)
	status, body := httpGet(t, base+"/api/v1/cluster/utilization-history?hours=24")
	if status != http.StatusOK {
		t.Fatalf("utilization-history status=%d, body=%s", status, body)
	}

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	points, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("data must be array, got %T", resp.Data)
	}
	if len(points) == 0 {
		t.Error("expected at least 1 data point in utilization history")
	}

	// Verify each point has ts and value
	first, ok := points[0].(map[string]interface{})
	if !ok {
		t.Fatalf("point must be object, got %T", points[0])
	}
	if _, ok := first["ts"]; !ok {
		t.Error("history point missing 'ts' field")
	}
	if _, ok := first["value"]; !ok {
		t.Error("history point missing 'value' field")
	}
}

// ── 4. Nodes ─────────────────────────────────────────────────────

func TestSmoke_ListNodes(t *testing.T) {
	base := testServer(t)
	status, body := httpGet(t, base+"/api/v1/nodes")
	if status != http.StatusOK {
		t.Fatalf("nodes status=%d, body=%s", status, body)
	}

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	nodes, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("nodes must be array, got %T", resp.Data)
	}
	if len(nodes) == 0 {
		t.Error("expected at least 1 GPU node")
	}

	// Verify node shape
	for i, n := range nodes {
		node, ok := n.(map[string]interface{})
		if !ok {
			continue
		}
		for _, field := range []string{"name", "gpuModel", "gpuCount", "vramTotalMiB", "ready"} {
			if _, ok := node[field]; !ok {
				t.Errorf("node[%d] missing field: %s", i, field)
			}
		}
	}
}

func TestSmoke_NodeMetricsNonNegative(t *testing.T) {
	base := testServer(t)
	_, body := httpGet(t, base+"/api/v1/nodes")

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	nodes, _ := resp.Data.([]interface{})
	for i, n := range nodes {
		node, ok := n.(map[string]interface{})
		if !ok {
			continue
		}
		for _, metric := range []string{"vramTotalMiB", "vramUsedMiB", "smUtilPct", "powerWatt", "tempCelsius"} {
			val, ok := node[metric].(float64)
			if !ok {
				continue
			}
			if val < 0 {
				t.Errorf("node[%d].%s=%v must be >= 0", i, metric, val)
			}
		}
	}
}

// ── 5. Jobs ─────────────────────────────────────────────────────

func TestSmoke_ListJobs(t *testing.T) {
	base := testServer(t)
	status, body := httpGet(t, base+"/api/v1/jobs")
	if status != http.StatusOK {
		t.Fatalf("jobs status=%d, body=%s", status, body)
	}

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	jobs, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("jobs must be array, got %T", resp.Data)
	}
	if len(jobs) == 0 {
		t.Error("expected at least 1 job")
	}

	// Verify job shape
	for i, j := range jobs {
		job, ok := j.(map[string]interface{})
		if !ok {
			continue
		}
		for _, field := range []string{"name", "namespace", "phase", "checkpointCount", "gpuModel"} {
			if _, ok := job[field]; !ok {
				t.Errorf("job[%d] missing field: %s", i, field)
			}
		}
		// Phase must be a valid PhoenixJob phase
		phase, _ := job["phase"].(string)
		validPhases := map[string]bool{
			"Pending": true, "Running": true, "Checkpointing": true,
			"Restoring": true, "Succeeded": true, "Failed": true,
		}
		if !validPhases[phase] {
			t.Errorf("job[%d] has invalid phase: %q", i, phase)
		}
	}
}

func TestSmoke_ListJobsByNamespace(t *testing.T) {
	base := testServer(t)
	_, body := httpGet(t, base+"/api/v1/jobs?namespace=research")

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	jobs, _ := resp.Data.([]interface{})
	for _, j := range jobs {
		job, ok := j.(map[string]interface{})
		if !ok {
			continue
		}
		ns, _ := job["namespace"].(string)
		if ns != "research" {
			t.Errorf("filtered job has namespace=%q, want research", ns)
		}
	}
}

func TestSmoke_GetJob(t *testing.T) {
	base := testServer(t)
	status, body := httpGet(t, base+"/api/v1/jobs/research/llm-pretrain-v3")
	if status != http.StatusOK {
		t.Fatalf("get job status=%d, body=%s", status, body)
	}

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	job, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("job data must be object, got %T", resp.Data)
	}
	if name, _ := job["name"].(string); name != "llm-pretrain-v3" {
		t.Errorf("job name=%q want=llm-pretrain-v3", name)
	}
}

func TestSmoke_GetJobNotFound(t *testing.T) {
	base := testServer(t)
	status, _ := httpGet(t, base+"/api/v1/jobs/default/no-such-job")
	if status != http.StatusNotFound {
		t.Errorf("nonexistent job status=%d want=404", status)
	}
}

func TestSmoke_TriggerCheckpoint(t *testing.T) {
	base := testServer(t)
	status, body := httpPost(t, base+"/api/v1/jobs/research/llm-pretrain-v3/checkpoint", "")
	if status != http.StatusAccepted {
		t.Errorf("checkpoint status=%d want=202, body=%s", status, body)
	}
}

// ── 6. Billing ──────────────────────────────────────────────────

func TestSmoke_BillingDepartments(t *testing.T) {
	base := testServer(t)

	for _, period := range []string{"daily", "weekly", "monthly"} {
		t.Run(period, func(t *testing.T) {
			status, body := httpGet(t, base+"/api/v1/billing/departments?period="+period)
			if status != http.StatusOK {
				t.Fatalf("billing departments status=%d, body=%s", status, body)
			}

			var resp apiResp
			if err := json.Unmarshal(body, &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			depts, ok := resp.Data.([]interface{})
			if !ok {
				t.Fatalf("data must be array, got %T", resp.Data)
			}
			if len(depts) == 0 {
				t.Error("expected at least 1 department")
			}
		})
	}
}

func TestSmoke_BillingDepartments_InvalidPeriod(t *testing.T) {
	base := testServer(t)
	status, _ := httpGet(t, base+"/api/v1/billing/departments?period=yearly")
	if status != http.StatusBadRequest {
		t.Errorf("invalid period status=%d want=400", status)
	}
}

func TestSmoke_BillingRecords(t *testing.T) {
	base := testServer(t)
	status, body := httpGet(t, base+"/api/v1/billing/records")
	if status != http.StatusOK {
		t.Fatalf("billing records status=%d, body=%s", status, body)
	}

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	records, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("records must be array, got %T", resp.Data)
	}

	for i, r := range records {
		rec, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		for _, field := range []string{"jobName", "gpuModel", "allocRatio", "tflopsHours", "costCNY"} {
			if _, ok := rec[field]; !ok {
				t.Errorf("record[%d] missing field: %s", i, field)
			}
		}
	}
}

// ── 7. Alerts ────────────────────────────────────────────────────

func TestSmoke_ListAlerts(t *testing.T) {
	base := testServer(t)
	status, body := httpGet(t, base+"/api/v1/alerts")
	if status != http.StatusOK {
		t.Fatalf("alerts status=%d, body=%s", status, body)
	}

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	alerts, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("alerts must be array, got %T", resp.Data)
	}

	for i, a := range alerts {
		alert, ok := a.(map[string]interface{})
		if !ok {
			continue
		}
		for _, field := range []string{"id", "severity", "message"} {
			if _, ok := alert[field]; !ok {
				t.Errorf("alert[%d] missing field: %s", i, field)
			}
		}
		// Severity must be valid
		sev, _ := alert["severity"].(string)
		if sev != "info" && sev != "warn" && sev != "error" {
			t.Errorf("alert[%d] invalid severity: %q", i, sev)
		}
	}
}

func TestSmoke_ResolveAlert(t *testing.T) {
	base := testServer(t)
	status, body := httpPost(t, base+"/api/v1/alerts/alert-1/resolve", "")
	if status != http.StatusOK {
		t.Errorf("resolve alert status=%d want=200, body=%s", status, body)
	}
}

// ── 8. Migration Routes ─────────────────────────────────────────

func TestSmoke_MigrationTrigger(t *testing.T) {
	base := testServer(t)
	status, body := httpPost(t, base+"/api/v1/jobs/research/llm-pretrain-v3/migrate",
		`{"targetNode":"gpu-node-02"}`)
	if status != http.StatusAccepted {
		t.Errorf("migrate status=%d want=202, body=%s", status, body)
	}
}

func TestSmoke_MigrationStatus(t *testing.T) {
	base := testServer(t)
	status, body := httpGet(t, base+"/api/v1/jobs/research/llm-pretrain-v3/migration-status")
	if status != http.StatusOK {
		t.Fatalf("migration-status status=%d, body=%s", status, body)
	}

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data must be object, got %T", resp.Data)
	}
	if _, ok := data["status"]; !ok {
		t.Error("migration-status response missing 'status' field")
	}
}

// ── 9. Error Handling ───────────────────────────────────────────

func TestSmoke_MethodNotAllowed(t *testing.T) {
	base := testServer(t)
	req, _ := http.NewRequest(http.MethodDelete, base+"/api/v1/nodes", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /nodes: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("method not allowed status=%d want=405", resp.StatusCode)
	}
}

func TestSmoke_ContentTypeJSON(t *testing.T) {
	base := testServer(t)
	endpoints := []string{
		"/api/v1/cluster/summary",
		"/api/v1/nodes",
		"/api/v1/jobs",
		"/api/v1/billing/departments?period=monthly",
		"/api/v1/billing/records",
		"/api/v1/alerts",
	}
	for _, ep := range endpoints {
		resp, err := http.Get(base + ep)
		if err != nil {
			t.Errorf("GET %s: %v", ep, err)
			continue
		}
		resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Errorf("GET %s: Content-Type=%q want application/json", ep, ct)
		}
	}
}

// ── 10. Response Latency ────────────────────────────────────────

func TestSmoke_ResponseTime(t *testing.T) {
	base := testServer(t)

	endpoints := []struct {
		path    string
		maxTime time.Duration
	}{
		{"/healthz", 2 * time.Second},
		{"/api/v1/cluster/summary", 5 * time.Second},
		{"/api/v1/nodes", 5 * time.Second},
		{"/api/v1/jobs", 5 * time.Second},
		{"/api/v1/billing/departments?period=monthly", 5 * time.Second},
		{"/api/v1/alerts", 5 * time.Second},
	}

	for _, ep := range endpoints {
		t.Run(ep.path, func(t *testing.T) {
			start := time.Now()
			status, _ := httpGet(t, base+ep.path)
			elapsed := time.Since(start)

			if status >= 500 {
				t.Errorf("%s returned %d", ep.path, status)
			}
			if elapsed > ep.maxTime {
				t.Errorf("%s took %s, exceeds max %s", ep.path, elapsed, ep.maxTime)
			}
		})
	}
}


