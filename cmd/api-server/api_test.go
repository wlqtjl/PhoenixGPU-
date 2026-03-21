// API integration tests — written BEFORE handler implementation (TDD Red).
// Uses net/http/httptest so no real server is needed.
//
// Contract: every endpoint must satisfy these tests to be considered done.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package main_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wlqtjl/PhoenixGPU/cmd/api-server/internal"
)

// newTestServer returns a test HTTP server backed by a fake K8s client.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	router := internal.NewRouter(internal.RouterConfig{
		K8sClient:  internal.NewFakeK8sClient(),
		Logger:     internal.NewNopLogger(),
		EnableMock: true,
	})
	return httptest.NewServer(router)
}

// get performs a GET request and asserts status code.
func get(t *testing.T, srv *httptest.Server, path string, wantStatus int) []byte {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Errorf("GET %s: status=%d want=%d", path, resp.StatusCode, wantStatus)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body for %s: %v", path, err)
	}
	return body
}

// ─── T39-1: Health endpoints ──────────────────────────────────────

func TestHealthz(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	get(t, srv, "/healthz", http.StatusOK)
}

func TestReadyz(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	get(t, srv, "/readyz", http.StatusOK)
}

// ─── T39-2: Cluster summary ───────────────────────────────────────

func TestGetClusterSummary_ReturnsValidShape(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := get(t, srv, "/api/v1/cluster/summary", http.StatusOK)

	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != "" {
		t.Errorf("unexpected error in response: %s", resp.Error)
	}

	// Data must be present and have required fields
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data field must be an object, got %T", resp.Data)
	}
	for _, field := range []string{"totalGPUs", "activeJobs", "avgUtilPct"} {
		if _, ok := data[field]; !ok {
			t.Errorf("cluster summary missing field: %s", field)
		}
	}
}

// ─── T39-3: Nodes ─────────────────────────────────────────────────

func TestListNodes_ReturnsArray(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := get(t, srv, "/api/v1/nodes", http.StatusOK)

	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	nodes, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("nodes data must be array, got %T", resp.Data)
	}
	if len(nodes) == 0 {
		t.Error("expected at least one node in mock response")
	}
}

// ─── T39-4: Jobs ─────────────────────────────────────────────────

func TestListJobs_ReturnsArray(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	body := get(t, srv, "/api/v1/jobs", http.StatusOK)

	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	jobs, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("jobs must be array, got %T", resp.Data)
	}
	if len(jobs) == 0 {
		t.Error("expected at least one job in mock response")
	}
}

func TestListJobs_FilterByNamespace(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	body := get(t, srv, "/api/v1/jobs?namespace=research", http.StatusOK)

	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// All returned jobs must be in requested namespace
	jobs, _ := resp.Data.([]interface{})
	for _, j := range jobs {
		jm, ok := j.(map[string]interface{})
		if !ok {
			continue
		}
		if ns, _ := jm["namespace"].(string); ns != "research" {
			t.Errorf("filtered job has namespace=%q, want research", ns)
		}
	}
}

func TestGetJob_NotFound(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	get(t, srv, "/api/v1/jobs/nonexistent/no-such-job", http.StatusNotFound)
}

func TestTriggerCheckpoint_ReturnsAccepted(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(
		srv.URL+"/api/v1/jobs/research/llm-pretrain-v3/checkpoint",
		"application/json", nil,
	)
	if err != nil {
		t.Fatalf("POST checkpoint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("checkpoint trigger: status=%d want=202", resp.StatusCode)
	}
}

// ─── T39-5: Billing ──────────────────────────────────────────────

func TestListBillingDepartments_ReturnsArray(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	body := get(t, srv, "/api/v1/billing/departments?period=monthly", http.StatusOK)

	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	depts, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("billing must be array, got %T", resp.Data)
	}
	if len(depts) == 0 {
		t.Error("expected at least one department in mock response")
	}
}

func TestListBillingDepartments_InvalidPeriod(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	get(t, srv, "/api/v1/billing/departments?period=yearly", http.StatusBadRequest)
}

func TestGetUtilHistory_LimitsMaxHours(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := get(t, srv, "/api/v1/cluster/utilization-history?hours=999999", http.StatusOK)
	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	points, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("history must be array, got %T", resp.Data)
	}
	// maxHistoryHours (720) * 2 points per hour in FakeK8sClient.
	if got, want := len(points), 1440; got != want {
		t.Fatalf("unexpected history length: got=%d want=%d", got, want)
	}
}

// ─── T39-6: Alerts ───────────────────────────────────────────────

func TestListAlerts_ReturnsArray(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	body := get(t, srv, "/api/v1/alerts", http.StatusOK)

	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	alerts, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("alerts must be array, got %T", resp.Data)
	}
	_ = alerts
}

func TestResolveAlert_ReturnsOK(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost,
		srv.URL+"/api/v1/alerts/alert-1/resolve", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST resolve: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("resolve alert: status=%d want=200", resp.StatusCode)
	}
}

// ─── T39-7: Response structure contract ──────────────────────────

func TestAllEndpoints_ReturnJSONContentType(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	endpoints := []string{
		"/api/v1/cluster/summary",
		"/api/v1/nodes",
		"/api/v1/jobs",
		"/api/v1/billing/departments?period=monthly",
		"/api/v1/alerts",
	}
	for _, ep := range endpoints {
		resp, err := http.Get(srv.URL + ep)
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

func TestMethodNotAllowed_Returns405(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/nodes", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/v1/nodes: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestDynamicRoute_InvalidJobPath_Returns400(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	get(t, srv, "/api/v1/jobs/research", http.StatusBadRequest)
}

func TestDynamicRoute_InvalidAlertPath_Returns404(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	get(t, srv, "/api/v1/alerts/alert-1", http.StatusNotFound)
}

func TestMigrationRoutes_Disabled_Return404(t *testing.T) {
	srv := newTestServerWithMigration(t, false)
	defer srv.Close()

	get(t, srv, "/api/v1/jobs/research/llm-pretrain-v3/migration-status", http.StatusNotFound)

	resp, err := http.Post(
		srv.URL+"/api/v1/jobs/research/llm-pretrain-v3/migrate",
		"application/json",
		strings.NewReader(`{"targetNode":"gpu-node-02"}`),
	)
	if err != nil {
		t.Fatalf("POST migrate: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestMigrationRoutes_Enabled_Workflow(t *testing.T) {
	srv := newTestServerWithMigration(t, true)
	defer srv.Close()

	resp, err := http.Post(
		srv.URL+"/api/v1/jobs/research/llm-pretrain-v3/migrate",
		"application/json",
		strings.NewReader(`{"targetNode":"gpu-node-02"}`),
	)
	if err != nil {
		t.Fatalf("POST migrate: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusAccepted)
	}

	body := get(t, srv, "/api/v1/jobs/research/llm-pretrain-v3/migration-status", http.StatusOK)
	var r internal.APIResponse
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := r.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("migration status must be object, got %T", r.Data)
	}
	if _, ok := data["status"]; !ok {
		t.Fatalf("status field missing")
	}
}

func TestAllEndpoints_NeverReturn500OnValidRequest(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	endpoints := []string{
		"/api/v1/cluster/summary",
		"/api/v1/nodes",
		"/api/v1/jobs",
		"/api/v1/billing/departments?period=monthly",
		"/api/v1/alerts",
	}
	for _, ep := range endpoints {
		resp, err := http.Get(srv.URL + ep)
		if err != nil {
			t.Errorf("GET %s: %v", ep, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusInternalServerError {
			t.Errorf("GET %s returned 500 — all valid requests must succeed", ep)
		}
	}
}
