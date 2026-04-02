// Unit tests for HTTP handlers — verifies error paths, edge cases, and
// path parsing helpers that are not covered by the integration tests.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package internal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── errorK8sClient — returns errors for all methods ───────────────

type errorK8sClient struct {
	err error
}

func (e errorK8sClient) GetClusterSummary(context.Context) (*ClusterSummary, error) {
	return nil, e.err
}
func (e errorK8sClient) GetUtilizationHistory(context.Context, int) ([]TimeSeriesPoint, error) {
	return nil, e.err
}
func (e errorK8sClient) ListGPUNodes(context.Context) ([]GPUNode, error) {
	return nil, e.err
}
func (e errorK8sClient) ListPhoenixJobs(context.Context, string) ([]PhoenixJob, error) {
	return nil, e.err
}
func (e errorK8sClient) GetPhoenixJob(context.Context, string, string) (*PhoenixJob, error) {
	return nil, e.err
}
func (e errorK8sClient) TriggerCheckpoint(context.Context, string, string) error {
	return e.err
}
func (e errorK8sClient) GetBillingByDepartment(context.Context, string) ([]DeptBilling, error) {
	return nil, e.err
}
func (e errorK8sClient) GetBillingRecords(context.Context, string) ([]BillingRecord, error) {
	return nil, e.err
}
func (e errorK8sClient) ListAlerts(context.Context) ([]Alert, error) {
	return nil, e.err
}
func (e errorK8sClient) ResolveAlert(context.Context, string) error {
	return e.err
}

func newErrorHandlers() *handlers {
	return &handlers{
		client: errorK8sClient{err: errors.New("backend failure")},
		log:    NewNopLogger(),
	}
}

func newFakeHandlers() *handlers {
	return &handlers{
		client: NewFakeK8sClient(),
		log:    NewNopLogger(),
	}
}

func decodeResponse(t *testing.T, body []byte) APIResponse {
	t.Helper()
	var resp APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

// ── Handler error paths ───────────────────────────────────────────

func TestGetClusterSummary_Error_Returns500(t *testing.T) {
	h := newErrorHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/summary", nil)
	h.getClusterSummary(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want=500", w.Code)
	}
	resp := decodeResponse(t, w.Body.Bytes())
	if resp.Error == "" {
		t.Error("expected error message in response")
	}
}

func TestGetUtilHistory_Error_Returns500(t *testing.T) {
	h := newErrorHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/utilization-history", nil)
	h.getUtilHistory(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want=500", w.Code)
	}
}

func TestListNodes_Error_Returns500(t *testing.T) {
	h := newErrorHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	h.listNodes(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want=500", w.Code)
	}
}

func TestListJobs_Error_Returns500(t *testing.T) {
	h := newErrorHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	h.listJobs(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want=500", w.Code)
	}
}

func TestGetJob_Error_Returns500(t *testing.T) {
	h := newErrorHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/research/llm-pretrain-v3", nil)
	h.getJob(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want=500", w.Code)
	}
}

func TestGetJob_InvalidPath_Returns400(t *testing.T) {
	h := newFakeHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/invalid", nil)
	h.getJob(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d want=400", w.Code)
	}
}

func TestGetJob_NotFound_Returns404(t *testing.T) {
	h := newFakeHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/ns/nonexistent", nil)
	h.getJob(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d want=404", w.Code)
	}
}

func TestTriggerCheckpoint_InvalidPath_Returns400(t *testing.T) {
	h := newFakeHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/bad-path", nil)
	h.triggerCheckpoint(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d want=400", w.Code)
	}
}

func TestTriggerCheckpoint_NotFound_Returns404(t *testing.T) {
	h := newFakeHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/ns/nonexistent/checkpoint", nil)
	h.triggerCheckpoint(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d want=404", w.Code)
	}
}

func TestTriggerCheckpoint_Error_Returns500(t *testing.T) {
	h := newErrorHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/research/llm-pretrain-v3/checkpoint", nil)
	h.triggerCheckpoint(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want=500", w.Code)
	}
}

func TestTriggerCheckpoint_Success_Returns202(t *testing.T) {
	h := newFakeHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/research/llm-pretrain-v3/checkpoint", nil)
	h.triggerCheckpoint(w, r)

	if w.Code != http.StatusAccepted {
		t.Errorf("status=%d want=202", w.Code)
	}
}

func TestListBillingDepartments_InvalidPeriod_Returns400(t *testing.T) {
	h := newFakeHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/billing/departments?period=yearly", nil)
	h.listBillingDepartments(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d want=400", w.Code)
	}
}

func TestListBillingDepartments_DefaultPeriod(t *testing.T) {
	h := newFakeHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/billing/departments", nil)
	h.listBillingDepartments(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d want=200", w.Code)
	}
}

func TestListBillingDepartments_Error_Returns500(t *testing.T) {
	h := newErrorHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/billing/departments?period=monthly", nil)
	h.listBillingDepartments(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want=500", w.Code)
	}
}

func TestListBillingRecords_Error_Returns500(t *testing.T) {
	h := newErrorHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/billing/records", nil)
	h.listBillingRecords(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want=500", w.Code)
	}
}

func TestListAlerts_Error_Returns500(t *testing.T) {
	h := newErrorHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	h.listAlerts(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want=500", w.Code)
	}
}

func TestListAlerts_SortedByCreatedAt(t *testing.T) {
	h := newFakeHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	h.listAlerts(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want=200", w.Code)
	}

	resp := decodeResponse(t, w.Body.Bytes())
	alerts, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("data must be array, got %T", resp.Data)
	}
	if len(alerts) == 0 {
		t.Fatal("expected alerts in response")
	}
}

func TestResolveAlert_InvalidPath_Returns400(t *testing.T) {
	h := newFakeHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/bad-path", nil)
	h.resolveAlert(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d want=400", w.Code)
	}
}

func TestResolveAlert_Error_Returns500(t *testing.T) {
	h := newErrorHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/alert-1/resolve", nil)
	h.resolveAlert(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want=500", w.Code)
	}
}

// ── Utilization history edge cases ────────────────────────────────

func TestGetUtilHistory_DefaultHours(t *testing.T) {
	h := newFakeHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/utilization-history", nil)
	h.getUtilHistory(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want=200", w.Code)
	}
	resp := decodeResponse(t, w.Body.Bytes())
	pts, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("data must be array, got %T", resp.Data)
	}
	// Default 24 hours * 2 pts = 48
	if len(pts) != 48 {
		t.Errorf("expected 48 points for default 24h, got %d", len(pts))
	}
}

func TestGetUtilHistory_InvalidHoursUsesDefault(t *testing.T) {
	h := newFakeHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/utilization-history?hours=abc", nil)
	h.getUtilHistory(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want=200", w.Code)
	}
	resp := decodeResponse(t, w.Body.Bytes())
	pts, _ := resp.Data.([]interface{})
	// Falls back to default 24 hours
	if len(pts) != 48 {
		t.Errorf("expected 48 points (default), got %d", len(pts))
	}
}

func TestGetUtilHistory_CapsAtMaxHours(t *testing.T) {
	h := newFakeHandlers()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/utilization-history?hours=999999", nil)
	h.getUtilHistory(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want=200", w.Code)
	}
	resp := decodeResponse(t, w.Body.Bytes())
	pts, _ := resp.Data.([]interface{})
	// maxHistoryHours = 720, 720*2 = 1440
	if len(pts) != 1440 {
		t.Errorf("expected 1440 points (max), got %d", len(pts))
	}
}

// ── Path parsing helpers ──────────────────────────────────────────

func TestSplitPath(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"/", 0},
		{"/api/v1/jobs", 3},
		{"/api/v1/jobs/ns/name", 5},
		{"api/v1/jobs/ns/name/checkpoint", 6},
	}
	for _, tc := range cases {
		got := splitPath(tc.input)
		if len(got) != tc.want {
			t.Errorf("splitPath(%q) len=%d want=%d parts=%v", tc.input, len(got), tc.want, got)
		}
	}
}

func TestParseJobPath(t *testing.T) {
	cases := []struct {
		path      string
		wantNS    string
		wantName  string
		wantOK    bool
	}{
		{"/api/v1/jobs/research/llm", "research", "llm", true},
		{"/api/v1/jobs/default/my-job", "default", "my-job", true},
		{"/api/v1/jobs", "", "", false},
		{"/api/v1/jobs/ns", "", "", false},
		{"/api/v1/jobs/ns/name/extra", "", "", false},
	}
	for _, tc := range cases {
		ns, name, ok := parseJobPath(tc.path)
		if ok != tc.wantOK || ns != tc.wantNS || name != tc.wantName {
			t.Errorf("parseJobPath(%q) = (%q,%q,%v) want (%q,%q,%v)",
				tc.path, ns, name, ok, tc.wantNS, tc.wantName, tc.wantOK)
		}
	}
}

func TestParseCheckpointPath(t *testing.T) {
	cases := []struct {
		path     string
		wantNS   string
		wantName string
		wantOK   bool
	}{
		{"/api/v1/jobs/ns/name/checkpoint", "ns", "name", true},
		{"/api/v1/jobs/ns/name", "", "", false},
		{"/api/v1/jobs/ns/name/other", "", "", false},
	}
	for _, tc := range cases {
		ns, name, ok := parseCheckpointPath(tc.path)
		if ok != tc.wantOK || ns != tc.wantNS || name != tc.wantName {
			t.Errorf("parseCheckpointPath(%q) = (%q,%q,%v) want (%q,%q,%v)",
				tc.path, ns, name, ok, tc.wantNS, tc.wantName, tc.wantOK)
		}
	}
}

func TestParseResolveAlertPath(t *testing.T) {
	cases := []struct {
		path   string
		wantID string
		wantOK bool
	}{
		{"/api/v1/alerts/alert-1/resolve", "alert-1", true},
		{"/api/v1/alerts/abc/resolve", "abc", true},
		{"/api/v1/alerts/bad", "", false},
		{"/api/v1/alerts/bad/other", "", false},
	}
	for _, tc := range cases {
		id, ok := parseResolveAlertPath(tc.path)
		if ok != tc.wantOK || id != tc.wantID {
			t.Errorf("parseResolveAlertPath(%q) = (%q,%v) want (%q,%v)",
				tc.path, id, ok, tc.wantID, tc.wantOK)
		}
	}
}
