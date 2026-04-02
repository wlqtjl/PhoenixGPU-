// Unit tests for router configuration, middleware, and response helpers.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package internal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── NewRouter configuration ───────────────────────────────────────

func TestNewRouter_NilClient_MockEnabled_FallsBackToFake(t *testing.T) {
	router := NewRouter(RouterConfig{
		K8sClient:  nil,
		Logger:     NewNopLogger(),
		EnableMock: true,
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/cluster/summary")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status=%d want=200", resp.StatusCode)
	}
}

func TestNewRouter_NilClient_MockDisabled_ReturnsErrors(t *testing.T) {
	router := NewRouter(RouterConfig{
		K8sClient:  nil,
		Logger:     NewNopLogger(),
		EnableMock: false,
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/cluster/summary")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status=%d want=500", resp.StatusCode)
	}
}

func TestNewRouter_NilLogger_DefaultsToStd(t *testing.T) {
	router := NewRouter(RouterConfig{
		K8sClient: NewFakeK8sClient(),
		Logger:    nil,
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status=%d want=200", resp.StatusCode)
	}
}

// ── Health endpoints ──────────────────────────────────────────────

func TestHealthz(t *testing.T) {
	router := NewRouter(RouterConfig{K8sClient: NewFakeK8sClient(), Logger: NewNopLogger()})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d want=200", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("body=%q want=ok", w.Body.String())
	}
}

func TestReadyz(t *testing.T) {
	router := NewRouter(RouterConfig{K8sClient: NewFakeK8sClient(), Logger: NewNopLogger()})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d want=200", w.Code)
	}
}

func TestMetrics(t *testing.T) {
	router := NewRouter(RouterConfig{K8sClient: NewFakeK8sClient(), Logger: NewNopLogger()})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d want=200", w.Code)
	}
}

// ── Method enforcement ────────────────────────────────────────────

func TestMethodMiddleware_BlocksWrongMethod(t *testing.T) {
	h := method(http.MethodGet, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/test", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want=405", w.Code)
	}
}

func TestMethodMiddleware_AllowsCorrectMethod(t *testing.T) {
	h := method(http.MethodGet, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d want=200", w.Code)
	}
}

// ── Panic recovery ────────────────────────────────────────────────

func TestWithRecovery_RecoversPanic(t *testing.T) {
	h := withRecovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went wrong")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want=500", w.Code)
	}
}

func TestWithRecovery_PassesNormalRequests(t *testing.T) {
	h := withRecovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d want=200", w.Code)
	}
}

// ── Response helpers ──────────────────────────────────────────────

func TestJsonResponse_SetsContentType(t *testing.T) {
	w := httptest.NewRecorder()
	jsonResponse(w, http.StatusOK, APIResponse{Data: "test"})

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type=%q want application/json", ct)
	}
}

func TestOk_IncludesTimestamp(t *testing.T) {
	w := httptest.NewRecorder()
	ok(w, map[string]string{"hello": "world"})

	var resp APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Meta == nil {
		t.Fatal("meta should not be nil")
	}
	if resp.Meta.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
}

func TestOkMeta_IncludesTotalCount(t *testing.T) {
	w := httptest.NewRecorder()
	okMeta(w, []string{"a", "b", "c"}, 3)

	var resp APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Meta == nil || resp.Meta.Total != 3 {
		t.Errorf("meta.total=%v want=3", resp.Meta)
	}
}

func TestErrResp_SetsStatusAndMessage(t *testing.T) {
	w := httptest.NewRecorder()
	errResp(w, http.StatusBadRequest, "bad input")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d want=400", w.Code)
	}
	var resp APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != "bad input" {
		t.Errorf("error=%q want=bad input", resp.Error)
	}
}

// ── NopLogger ─────────────────────────────────────────────────────

func TestNopLogger_DoesNotPanic(t *testing.T) {
	log := NewNopLogger()
	log.Info("test", "key", "val")
	log.Warn("test", "key", "val")
	log.Error("test", "key", "val")
}

// ── stdLogger ─────────────────────────────────────────────────────

func TestStdLogger_DoesNotPanic(t *testing.T) {
	log := stdLogger{}
	log.Info("test", "key", "val")
	log.Warn("test", "key", "val")
	log.Error("test", "key", "val")
}

// ── statusWriter ──────────────────────────────────────────────────

func TestStatusWriter_CapturesStatusCode(t *testing.T) {
	inner := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: inner, status: http.StatusOK}
	sw.WriteHeader(http.StatusNotFound)

	if sw.status != http.StatusNotFound {
		t.Errorf("status=%d want=404", sw.status)
	}
}

// ── Migration routes disabled ─────────────────────────────────────

func TestRouter_MigrationDisabled_MigrateReturns404(t *testing.T) {
	router := NewRouter(RouterConfig{
		K8sClient:       NewFakeK8sClient(),
		Logger:          NewNopLogger(),
		EnableMigration: false,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/ns/name/migrate",
		strings.NewReader(`{"targetNode":"n1"}`))
	router.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d want=404", w.Code)
	}
}

func TestRouter_MigrationDisabled_StatusReturns404(t *testing.T) {
	router := NewRouter(RouterConfig{
		K8sClient:       NewFakeK8sClient(),
		Logger:          NewNopLogger(),
		EnableMigration: false,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/ns/name/migration-status", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d want=404", w.Code)
	}
}

// ── Alert sub-routes ──────────────────────────────────────────────

func TestRouter_AlertWithoutResolve_Returns404(t *testing.T) {
	router := NewRouter(RouterConfig{
		K8sClient: NewFakeK8sClient(),
		Logger:    NewNopLogger(),
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/alert-1", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d want=404", w.Code)
	}
}

// ── Request logger ────────────────────────────────────────────────

func TestRequestLogger_DoesNotBreakResponse(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	})
	h := requestLogger(NewNopLogger(), inner)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d want=200", w.Code)
	}
	if w.Body.String() != "hello" {
		t.Errorf("body=%q want=hello", w.Body.String())
	}
}
