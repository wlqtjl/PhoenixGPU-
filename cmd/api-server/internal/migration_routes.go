package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type MigrationExecutor interface {
	Execute(ctx context.Context, namespace, name, targetNode string) error
}

type noopMigrationExecutor struct{}

func (noopMigrationExecutor) Execute(context.Context, string, string, string) error { return nil }

type migrationHandlers struct {
	exec   MigrationExecutor
	log    Logger
	mu     sync.RWMutex
	status map[string]string
}

func newMigrationHandlers(exec MigrationExecutor, log Logger) *migrationHandlers {
	if exec == nil {
		exec = noopMigrationExecutor{}
	}
	return &migrationHandlers{
		exec:   exec,
		log:    log,
		status: make(map[string]string),
	}
}

type migrateRequest struct {
	TargetNode string `json:"targetNode"`
}

func (h *migrationHandlers) triggerMigration(w http.ResponseWriter, r *http.Request) {
	ns, name, ok := parseMigratePath(r.URL.Path)
	if !ok {
		errResp(w, http.StatusBadRequest, "invalid migrate path")
		return
	}
	var req migrateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetNode == "" {
		errResp(w, http.StatusBadRequest, "invalid request body: targetNode is required")
		return
	}

	job := ns + "/" + name
	h.setStatus(job, "running")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := h.exec.Execute(ctx, ns, name, req.TargetNode); err != nil {
			h.log.Error("migration failed", "job", job, "target", req.TargetNode, "err", err)
			h.setStatus(job, "failed")
			return
		}
		h.setStatus(job, "done")
	}()

	jsonResponse(w, http.StatusAccepted, APIResponse{
		Data: map[string]string{
			"message":    "migration started",
			"job":        job,
			"targetNode": req.TargetNode,
		},
	})
}

func (h *migrationHandlers) getMigrationStatus(w http.ResponseWriter, r *http.Request) {
	ns, name, matched := parseMigrationStatusPath(r.URL.Path)
	if !matched {
		errResp(w, http.StatusBadRequest, "invalid migration-status path")
		return
	}
	job := ns + "/" + name
	status := h.getStatus(job)
	if status == "" {
		status = "no_migration_in_progress"
	}
	ok(w, map[string]string{
		"job":    job,
		"status": status,
	})
}

func (h *migrationHandlers) setStatus(job, status string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status[job] = status
}

func (h *migrationHandlers) getStatus(job string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.status[job]
}

func parseMigratePath(path string) (namespace, name string, ok bool) {
	parts := splitPath(path)
	if len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "jobs" && parts[5] == "migrate" {
		return parts[3], parts[4], true
	}
	return "", "", false
}

func parseMigrationStatusPath(path string) (namespace, name string, ok bool) {
	parts := splitPath(path)
	if len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "jobs" && parts[5] == "migration-status" {
		return parts[3], parts[4], true
	}
	return "", "", false
}
