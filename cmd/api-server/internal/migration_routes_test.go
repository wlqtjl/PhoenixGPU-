// Unit tests for migration routes — trigger, status, path parsing,
// and concurrent status management.
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
	"strings"
	"sync"
	"testing"
	"time"
)

// ── Test executor mocks ───────────────────────────────────────────

type succeedExecutor struct{}

func (succeedExecutor) Execute(context.Context, string, string, string) error { return nil }

type failExecutor struct{}

func (failExecutor) Execute(context.Context, string, string, string) error {
	return errors.New("migration failed")
}

type slowExecutor struct {
	delay time.Duration
}

func (s slowExecutor) Execute(ctx context.Context, _, _, _ string) error {
	select {
	case <-time.After(s.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ── Path parsers ──────────────────────────────────────────────────

func TestParseMigratePath(t *testing.T) {
	cases := []struct {
		path     string
		wantNS   string
		wantName string
		wantOK   bool
	}{
		{"/api/v1/jobs/research/llm/migrate", "research", "llm", true},
		{"/api/v1/jobs/ns/name/migrate", "ns", "name", true},
		{"/api/v1/jobs/ns/name", "", "", false},
		{"/api/v1/jobs/ns/name/checkpoint", "", "", false},
		{"/api/v1/jobs/migrate", "", "", false},
	}
	for _, tc := range cases {
		ns, name, ok := parseMigratePath(tc.path)
		if ok != tc.wantOK || ns != tc.wantNS || name != tc.wantName {
			t.Errorf("parseMigratePath(%q) = (%q,%q,%v) want (%q,%q,%v)",
				tc.path, ns, name, ok, tc.wantNS, tc.wantName, tc.wantOK)
		}
	}
}

func TestParseMigrationStatusPath(t *testing.T) {
	cases := []struct {
		path     string
		wantNS   string
		wantName string
		wantOK   bool
	}{
		{"/api/v1/jobs/research/llm/migration-status", "research", "llm", true},
		{"/api/v1/jobs/ns/name/migration-status", "ns", "name", true},
		{"/api/v1/jobs/ns/name", "", "", false},
		{"/api/v1/jobs/ns/name/migrate", "", "", false},
	}
	for _, tc := range cases {
		ns, name, ok := parseMigrationStatusPath(tc.path)
		if ok != tc.wantOK || ns != tc.wantNS || name != tc.wantName {
			t.Errorf("parseMigrationStatusPath(%q) = (%q,%q,%v) want (%q,%q,%v)",
				tc.path, ns, name, ok, tc.wantNS, tc.wantName, tc.wantOK)
		}
	}
}

// ── triggerMigration ──────────────────────────────────────────────

func TestTriggerMigration_InvalidPath_Returns400(t *testing.T) {
	mh := newMigrationHandlers(succeedExecutor{}, NewNopLogger())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/bad-path", nil)
	mh.triggerMigration(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d want=400", w.Code)
	}
}

func TestTriggerMigration_MissingTargetNode_Returns400(t *testing.T) {
	mh := newMigrationHandlers(succeedExecutor{}, NewNopLogger())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/ns/name/migrate",
		strings.NewReader(`{}`))
	mh.triggerMigration(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d want=400", w.Code)
	}
}

func TestTriggerMigration_InvalidJSON_Returns400(t *testing.T) {
	mh := newMigrationHandlers(succeedExecutor{}, NewNopLogger())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/ns/name/migrate",
		strings.NewReader(`not json`))
	mh.triggerMigration(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d want=400", w.Code)
	}
}

func TestTriggerMigration_Success_Returns202(t *testing.T) {
	mh := newMigrationHandlers(succeedExecutor{}, NewNopLogger())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/ns/name/migrate",
		strings.NewReader(`{"targetNode":"gpu-node-02"}`))
	mh.triggerMigration(w, r)

	if w.Code != http.StatusAccepted {
		t.Errorf("status=%d want=202", w.Code)
	}

	var resp APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data must be object, got %T", resp.Data)
	}
	if data["targetNode"] != "gpu-node-02" {
		t.Errorf("targetNode=%v want=gpu-node-02", data["targetNode"])
	}
}

// ── getMigrationStatus ────────────────────────────────────────────

func TestGetMigrationStatus_InvalidPath_Returns400(t *testing.T) {
	mh := newMigrationHandlers(succeedExecutor{}, NewNopLogger())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/bad-path", nil)
	mh.getMigrationStatus(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d want=400", w.Code)
	}
}

func TestGetMigrationStatus_NoMigration(t *testing.T) {
	mh := newMigrationHandlers(succeedExecutor{}, NewNopLogger())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/ns/name/migration-status", nil)
	mh.getMigrationStatus(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d want=200", w.Code)
	}

	var resp APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, _ := resp.Data.(map[string]interface{})
	if data["status"] != "no_migration_in_progress" {
		t.Errorf("status=%v want=no_migration_in_progress", data["status"])
	}
}

// ── Status lifecycle ──────────────────────────────────────────────

func TestMigrationStatus_Lifecycle_SuccessfulMigration(t *testing.T) {
	mh := newMigrationHandlers(succeedExecutor{}, NewNopLogger())

	// Trigger migration
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/ns/job1/migrate",
		strings.NewReader(`{"targetNode":"node-2"}`))
	mh.triggerMigration(w, r)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d want=202", w.Code)
	}

	// Wait for async execution
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s := mh.getStatus("ns/job1")
		if s == "done" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("migration did not reach 'done' status within timeout")
}

func TestMigrationStatus_Lifecycle_FailedMigration(t *testing.T) {
	mh := newMigrationHandlers(failExecutor{}, NewNopLogger())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/ns/job2/migrate",
		strings.NewReader(`{"targetNode":"node-3"}`))
	mh.triggerMigration(w, r)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s := mh.getStatus("ns/job2")
		if s == "failed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("migration did not reach 'failed' status within timeout")
}

// ── Concurrent status access ──────────────────────────────────────

func TestMigrationHandlers_ConcurrentStatusAccess(t *testing.T) {
	mh := newMigrationHandlers(succeedExecutor{}, NewNopLogger())
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			mh.setStatus("job-key", "running")
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = mh.getStatus("job-key")
		}(i)
	}
	wg.Wait()
	// If we get here without race detector firing, test passes
}

// ── Nil executor defaults to noop ─────────────────────────────────

func TestNewMigrationHandlers_NilExecutor_DefaultsToNoop(t *testing.T) {
	mh := newMigrationHandlers(nil, NewNopLogger())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/ns/name/migrate",
		strings.NewReader(`{"targetNode":"node-1"}`))
	mh.triggerMigration(w, r)

	if w.Code != http.StatusAccepted {
		t.Errorf("status=%d want=202", w.Code)
	}
}
