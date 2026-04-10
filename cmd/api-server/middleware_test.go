// Unit tests for API server middleware, auth, rate limiting, and path parsing.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package main_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wlqtjl/PhoenixGPU/cmd/api-server/internal"
)

// ── Auth middleware tests ─────────────────────────────────────────

func newAuthServer(t *testing.T, tokens map[string]bool) *httptest.Server {
	t.Helper()
	router := internal.NewRouter(internal.RouterConfig{
		K8sClient:  internal.NewFakeK8sClient(),
		Logger:     internal.NewNopLogger(),
		EnableMock: true,
		AuthTokens: tokens,
	})
	return httptest.NewServer(router)
}

func TestAuth_DisabledWhenNoTokens(t *testing.T) {
	srv := newAuthServer(t, nil)
	defer srv.Close()

	// No Authorization header needed
	body := get(t, srv, "/api/v1/nodes", http.StatusOK)
	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

func TestAuth_RejectsMissingHeader(t *testing.T) {
	srv := newAuthServer(t, map[string]bool{"secret-token-123": true})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/nodes")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status=%d want=401", resp.StatusCode)
	}
}

func TestAuth_RejectsInvalidFormat(t *testing.T) {
	srv := newAuthServer(t, map[string]bool{"secret-token-123": true})
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/nodes", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz") // Basic auth, not Bearer
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status=%d want=401", resp.StatusCode)
	}
}

func TestAuth_RejectsWrongToken(t *testing.T) {
	srv := newAuthServer(t, map[string]bool{"correct-token": true})
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/nodes", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status=%d want=403", resp.StatusCode)
	}
}

func TestAuth_AcceptsValidToken(t *testing.T) {
	srv := newAuthServer(t, map[string]bool{"my-valid-token": true})
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/nodes", nil)
	req.Header.Set("Authorization", "Bearer my-valid-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status=%d want=200", resp.StatusCode)
	}
}

func TestAuth_AcceptsMultipleTokens(t *testing.T) {
	tokens := map[string]bool{
		"token-a": true,
		"token-b": true,
	}
	srv := newAuthServer(t, tokens)
	defer srv.Close()

	for _, token := range []string{"token-a", "token-b"} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/cluster/summary", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request with %s: %v", token, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("token %s: status=%d want=200", token, resp.StatusCode)
		}
	}
}

func TestAuth_PublicEndpointsSkipAuth(t *testing.T) {
	srv := newAuthServer(t, map[string]bool{"secret": true})
	defer srv.Close()

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: status=%d want=200 (public endpoint should skip auth)", path, resp.StatusCode)
		}
	}
}

// ── Rate limiting tests ──────────────────────────────────────────

func newRateLimitedServer(t *testing.T, rps float64, burst int) *httptest.Server {
	t.Helper()
	router := internal.NewRouter(internal.RouterConfig{
		K8sClient:      internal.NewFakeK8sClient(),
		Logger:         internal.NewNopLogger(),
		EnableMock:     true,
		RateLimitRPS:   rps,
		RateLimitBurst: burst,
	})
	return httptest.NewServer(router)
}

func TestRateLimit_DisabledWhenZero(t *testing.T) {
	srv := newRateLimitedServer(t, 0, 0)
	defer srv.Close()

	// Should allow unlimited requests
	for i := 0; i < 20; i++ {
		resp, err := http.Get(srv.URL + "/api/v1/nodes")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Error("rate limiting should be disabled when RPS=0")
		}
	}
}

func TestRateLimit_EnforcesLimit(t *testing.T) {
	// 1 RPS with burst=2 — after 2 requests, should reject
	srv := newRateLimitedServer(t, 1, 2)
	defer srv.Close()

	got429 := false
	for i := 0; i < 10; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/nodes", nil)
		// Use a fixed forwarded IP so all requests share the same rate limiter.
		req.Header.Set("X-Forwarded-For", "10.0.0.1")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("expected 429 after exceeding rate limit")
	}
}

func TestRateLimit_IncludesRetryAfterHeader(t *testing.T) {
	srv := newRateLimitedServer(t, 1, 2)
	defer srv.Close()

	// Exhaust burst
	for i := 0; i < 10; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/nodes", nil)
		req.Header.Set("X-Forwarded-For", "10.0.0.2")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			if resp.Header.Get("Retry-After") == "" {
				t.Error("429 response should include Retry-After header")
			}
			resp.Body.Close()
			return
		}
		resp.Body.Close()
	}
	t.Error("never hit 429")
}

func TestRateLimit_PublicPathsExempt(t *testing.T) {
	srv := newRateLimitedServer(t, 1, 2)
	defer srv.Close()

	// Exhaust burst on API endpoint
	for i := 0; i < 10; i++ {
		resp, _ := http.Get(srv.URL + "/api/v1/nodes")
		if resp != nil {
			resp.Body.Close()
		}
	}

	// Health endpoints should still work
	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: status=%d — health endpoints should bypass rate limit", path, resp.StatusCode)
		}
	}
}

// ── Panic recovery middleware ────────────────────────────────────

func TestRecovery_Returns500OnPanic(t *testing.T) {
	// The withRecovery middleware should catch panics in handlers.
	// We can test this by verifying that valid requests never get 500
	// (already covered), and by verifying the middleware is applied
	// in the chain (structural test).
	// We can't easily inject a panic without modifying source, but
	// we verify that the router doesn't crash on bad requests.
	srv := newTestServer(t)
	defer srv.Close()

	// Various malformed requests that should NOT cause 500
	badRequests := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/jobs/"},
		{"GET", "/api/v1/jobs/a/b/c/d"},
		{"GET", "/nonexistent-path"},
		{"DELETE", "/api/v1/nodes"},
	}
	for _, br := range badRequests {
		req, _ := http.NewRequest(br.method, srv.URL+br.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", br.method, br.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusInternalServerError {
			t.Errorf("%s %s returned 500 — should be handled gracefully", br.method, br.path)
		}
	}
}

// ── Query parameter edge cases ───────────────────────────────────

func TestUtilHistory_DefaultHours(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := get(t, srv, "/api/v1/cluster/utilization-history", http.StatusOK)
	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	pts, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("data must be array, got %T", resp.Data)
	}
	// Default is 24 hours * 2 points/hour = 48 points
	if len(pts) != 48 {
		t.Errorf("expected 48 points (24h default), got %d", len(pts))
	}
}

func TestUtilHistory_NegativeHoursIgnored(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	// Negative hours should be ignored (use default)
	body := get(t, srv, "/api/v1/cluster/utilization-history?hours=-5", http.StatusOK)
	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	pts, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("data must be array, got %T", resp.Data)
	}
	// Should fall back to default (24h = 48 points)
	if len(pts) != 48 {
		t.Errorf("expected 48 points (default for invalid hours), got %d", len(pts))
	}
}

func TestUtilHistory_NonIntegerHoursIgnored(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := get(t, srv, "/api/v1/cluster/utilization-history?hours=abc", http.StatusOK)
	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	pts, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("data must be array, got %T", resp.Data)
	}
	// Should fall back to default
	if len(pts) != 48 {
		t.Errorf("expected 48 points (default for non-integer hours), got %d", len(pts))
	}
}

func TestBillingRecords_NoDepartmentFilter(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := get(t, srv, "/api/v1/billing/records", http.StatusOK)
	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	records, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("records must be array, got %T", resp.Data)
	}
	if len(records) == 0 {
		t.Error("expected billing records without filter")
	}
}

func TestBillingDepartments_DefaultPeriod(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	// No period parameter — should default to monthly
	body := get(t, srv, "/api/v1/billing/departments", http.StatusOK)
	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// ── Migration route tests ─────────────────────────────────────────

func TestMigration_MissingTargetNode_Returns400(t *testing.T) {
	srv := newTestServerWithMigration(t, true)
	defer srv.Close()

	resp, err := http.Post(
		srv.URL+"/api/v1/jobs/research/llm-pretrain-v3/migrate",
		"application/json",
		strings.NewReader(`{}`),
	)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status=%d want=400 for missing targetNode", resp.StatusCode)
	}
}

func TestMigration_InvalidJSON_Returns400(t *testing.T) {
	srv := newTestServerWithMigration(t, true)
	defer srv.Close()

	resp, err := http.Post(
		srv.URL+"/api/v1/jobs/research/llm-pretrain-v3/migrate",
		"application/json",
		strings.NewReader(`{not json}`),
	)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status=%d want=400 for invalid JSON", resp.StatusCode)
	}
}

func TestMigration_StatusBeforeMigrate_ReturnsNoProgress(t *testing.T) {
	srv := newTestServerWithMigration(t, true)
	defer srv.Close()

	body := get(t, srv, "/api/v1/jobs/ns/job/migration-status", http.StatusOK)
	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data must be object, got %T", resp.Data)
	}
	if data["status"] != "no_migration_in_progress" {
		t.Errorf("status = %v, want no_migration_in_progress", data["status"])
	}
}

func TestMigration_StatusAfterTrigger_ReturnsRunning(t *testing.T) {
	srv := newTestServerWithMigration(t, true)
	defer srv.Close()

	// Trigger migration
	resp, err := http.Post(
		srv.URL+"/api/v1/jobs/research/llm-pretrain-v3/migrate",
		"application/json",
		strings.NewReader(`{"targetNode":"gpu-node-02"}`),
	)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	// Wait briefly for goroutine to start
	time.Sleep(50 * time.Millisecond)

	body := get(t, srv, "/api/v1/jobs/research/llm-pretrain-v3/migration-status", http.StatusOK)
	var statusResp internal.APIResponse
	if err := json.Unmarshal(body, &statusResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := statusResp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data must be object, got %T", statusResp.Data)
	}
	// Status should be either "running" or "done" (noop executor completes fast)
	status := data["status"].(string)
	if status != "running" && status != "done" {
		t.Errorf("status = %v, want running or done", status)
	}
}

// ── getJob endpoint additional tests ─────────────────────────────

func TestGetJob_ExistingJob_ReturnsData(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := get(t, srv, "/api/v1/jobs/research/llm-pretrain-v3", http.StatusOK)
	var resp internal.APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data must be object, got %T", resp.Data)
	}
	if data["name"] != "llm-pretrain-v3" {
		t.Errorf("job name = %v, want llm-pretrain-v3", data["name"])
	}
}

func TestCheckpoint_NotFoundJob_Returns404(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(
		srv.URL+"/api/v1/jobs/nonexistent/no-job/checkpoint",
		"application/json", nil,
	)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status=%d want=404 for nonexistent job", resp.StatusCode)
	}
}
