// Package validation -- test helper that builds a mock API server
// using only public types from pkg/types.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package validation

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wlqtjl/PhoenixGPU/pkg/types"
)

var apiURL = flag.String("api-url", "", "PhoenixGPU API base URL (empty = start local mock server)")

// apiResp mirrors the unified API response envelope.
type apiResp struct {
	Data  interface{}            `json:"data"`
	Error string                 `json:"error,omitempty"`
	Meta  map[string]interface{} `json:"meta,omitempty"`
}

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

// testServer returns the base URL for the running API server.
func testServer(t *testing.T) string {
	t.Helper()
	if *apiURL != "" {
		return strings.TrimRight(*apiURL, "/")
	}
	srv := httptest.NewServer(newMockRouter())
	t.Cleanup(srv.Close)
	return srv.URL
}

// newMockRouter creates a minimal HTTP router backed by FakeK8sClient.
// This mirrors the real router in cmd/api-server/internal/router.go
// but uses only public packages.
func newMockRouter() http.Handler {
	client := types.NewFakeK8sClient()
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("# metrics endpoint\n"))
	})

	mux.HandleFunc("/api/v1/cluster/summary", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, apiResp{Error: "method not allowed"})
			return
		}
		s, err := client.GetClusterSummary(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, apiResp{Data: s, Meta: map[string]interface{}{"timestamp": time.Now().UTC()}})
	})

	mux.HandleFunc("/api/v1/cluster/utilization-history", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, apiResp{Error: "method not allowed"})
			return
		}
		hours := 24
		if v := r.URL.Query().Get("hours"); v != "" {
			if n := parseInt(v); n > 0 {
				if n > 720 {
					n = 720
				}
				hours = n
			}
		}
		pts, err := client.GetUtilizationHistory(r.Context(), hours)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, apiResp{Data: pts, Meta: map[string]interface{}{"total": len(pts), "timestamp": time.Now().UTC()}})
	})

	mux.HandleFunc("/api/v1/nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, apiResp{Error: "method not allowed"})
			return
		}
		nodes, err := client.ListGPUNodes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, apiResp{Data: nodes, Meta: map[string]interface{}{"total": len(nodes), "timestamp": time.Now().UTC()}})
	})

	mux.HandleFunc("/api/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, apiResp{Error: "method not allowed"})
			return
		}
		ns := r.URL.Query().Get("namespace")
		jobs, err := client.ListPhoenixJobs(r.Context(), ns)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, apiResp{Data: jobs, Meta: map[string]interface{}{"total": len(jobs), "timestamp": time.Now().UTC()}})
	})

	mux.HandleFunc("/api/v1/billing/departments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, apiResp{Error: "method not allowed"})
			return
		}
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "monthly"
		}
		if period != "daily" && period != "weekly" && period != "monthly" {
			writeJSON(w, http.StatusBadRequest, apiResp{Error: "period must be daily, weekly, or monthly"})
			return
		}
		depts, err := client.GetBillingByDepartment(r.Context(), period)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, apiResp{Data: depts, Meta: map[string]interface{}{"total": len(depts), "timestamp": time.Now().UTC()}})
	})

	mux.HandleFunc("/api/v1/billing/records", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, apiResp{Error: "method not allowed"})
			return
		}
		dept := r.URL.Query().Get("department")
		records, err := client.GetBillingRecords(r.Context(), dept)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, apiResp{Data: records, Meta: map[string]interface{}{"total": len(records), "timestamp": time.Now().UTC()}})
	})

	mux.HandleFunc("/api/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, apiResp{Error: "method not allowed"})
			return
		}
		alerts, err := client.ListAlerts(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Error: err.Error()})
			return
		}
		sort.Slice(alerts, func(i, j int) bool {
			return alerts[i].CreatedAt.After(alerts[j].CreatedAt)
		})
		writeJSON(w, http.StatusOK, apiResp{Data: alerts, Meta: map[string]interface{}{"total": len(alerts), "timestamp": time.Now().UTC()}})
	})

	// Dynamic job routes: /api/v1/jobs/{namespace}/{name}[/checkpoint|/migrate|/migration-status]
	mux.HandleFunc("/api/v1/jobs/", func(w http.ResponseWriter, r *http.Request) {
		parts := splitPath(r.URL.Path)

		// POST /api/v1/jobs/{ns}/{name}/checkpoint
		if len(parts) == 6 && parts[5] == "checkpoint" && r.Method == http.MethodPost {
			ns, name := parts[3], parts[4]
			if err := client.TriggerCheckpoint(r.Context(), ns, name); err != nil {
				if types.IsNotFound(err) {
					writeJSON(w, http.StatusNotFound, apiResp{Error: "job not found"})
					return
				}
				writeJSON(w, http.StatusInternalServerError, apiResp{Error: err.Error()})
				return
			}
			writeJSON(w, http.StatusAccepted, apiResp{Data: map[string]string{"message": "checkpoint triggered"}})
			return
		}

		// POST /api/v1/jobs/{ns}/{name}/migrate
		if len(parts) == 6 && parts[5] == "migrate" && r.Method == http.MethodPost {
			var req struct {
				TargetNode string `json:"targetNode"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetNode == "" {
				writeJSON(w, http.StatusBadRequest, apiResp{Error: "targetNode is required"})
				return
			}
			writeJSON(w, http.StatusAccepted, apiResp{Data: map[string]string{
				"message":    "migration started",
				"job":        parts[3] + "/" + parts[4],
				"targetNode": req.TargetNode,
			}})
			return
		}

		// GET /api/v1/jobs/{ns}/{name}/migration-status
		if len(parts) == 6 && parts[5] == "migration-status" && r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, apiResp{Data: map[string]string{
				"job":    parts[3] + "/" + parts[4],
				"status": "no_migration_in_progress",
			}})
			return
		}

		// GET /api/v1/jobs/{ns}/{name}
		if len(parts) == 5 && r.Method == http.MethodGet {
			ns, name := parts[3], parts[4]
			job, err := client.GetPhoenixJob(r.Context(), ns, name)
			if err != nil {
				if types.IsNotFound(err) {
					writeJSON(w, http.StatusNotFound, apiResp{Error: "job not found"})
					return
				}
				writeJSON(w, http.StatusInternalServerError, apiResp{Error: err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, apiResp{Data: job})
			return
		}

		writeJSON(w, http.StatusBadRequest, apiResp{Error: "invalid path"})
	})

	// Dynamic alert routes: /api/v1/alerts/{id}/resolve
	mux.HandleFunc("/api/v1/alerts/", func(w http.ResponseWriter, r *http.Request) {
		parts := splitPath(r.URL.Path)
		if len(parts) == 5 && parts[4] == "resolve" && r.Method == http.MethodPost {
			id := parts[3]
			if err := client.ResolveAlert(context.Background(), id); err != nil {
				writeJSON(w, http.StatusInternalServerError, apiResp{Error: err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, apiResp{Data: map[string]string{"message": "alert resolved"}})
			return
		}
		http.NotFound(w, r)
	})

	return mux
}

func writeJSON(w http.ResponseWriter, status int, payload apiResp) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func splitPath(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
