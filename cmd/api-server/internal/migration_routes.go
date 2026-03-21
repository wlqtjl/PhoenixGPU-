package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type MigrationExecutor interface {
	Execute(ctx context.Context, namespace, name, targetNode string) error
}

type noopMigrationExecutor struct{}

func (noopMigrationExecutor) Execute(context.Context, string, string, string) error { return nil }

type MigrationStatus struct {
	Job         string     `json:"job"`
	Namespace   string     `json:"namespace"`
	Name        string     `json:"name"`
	TargetNode  string     `json:"targetNode"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"startedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type MigrationStatusStore interface {
	Save(ctx context.Context, status MigrationStatus) error
	Get(ctx context.Context, namespace, name string) (MigrationStatus, bool, error)
}

type MigrationAuditSink interface {
	Write(ctx context.Context, event MigrationAuditEvent) error
}

type MigrationAuditEvent struct {
	When       time.Time `json:"when"`
	Phase      string    `json:"phase"`
	Job        string    `json:"job"`
	Namespace  string    `json:"namespace"`
	Name       string    `json:"name"`
	TargetNode string    `json:"targetNode"`
	Error      string    `json:"error,omitempty"`
}

type memoryMigrationStatusStore struct {
	mu   sync.RWMutex
	data map[string]MigrationStatus
}

func newMemoryMigrationStatusStore() *memoryMigrationStatusStore {
	return &memoryMigrationStatusStore{data: make(map[string]MigrationStatus)}
}

func (s *memoryMigrationStatusStore) Save(_ context.Context, status MigrationStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[status.Job] = status
	return nil
}

func (s *memoryMigrationStatusStore) Get(_ context.Context, namespace, name string) (MigrationStatus, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job := namespace + "/" + name
	st, ok := s.data[job]
	return st, ok, nil
}

type fileMigrationStatusStore struct {
	path string
	mu   sync.Mutex
}

func newFileMigrationStatusStore(path string) (*fileMigrationStatusStore, error) {
	if path == "" {
		return nil, errors.New("empty migration status file path")
	}
	return &fileMigrationStatusStore{path: path}, nil
}

func NewFileMigrationStatusStore(path string) (MigrationStatusStore, error) {
	return newFileMigrationStatusStore(path)
}

func (s *fileMigrationStatusStore) Save(_ context.Context, status MigrationStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.loadAll()
	if err != nil {
		return err
	}
	all[status.Job] = status
	return s.persist(all)
}

func (s *fileMigrationStatusStore) Get(_ context.Context, namespace, name string) (MigrationStatus, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.loadAll()
	if err != nil {
		return MigrationStatus{}, false, err
	}
	job := namespace + "/" + name
	st, ok := all[job]
	return st, ok, nil
}

func (s *fileMigrationStatusStore) loadAll() (map[string]MigrationStatus, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]MigrationStatus{}, nil
		}
		return nil, fmt.Errorf("read migration status file: %w", err)
	}
	if len(b) == 0 {
		return map[string]MigrationStatus{}, nil
	}
	var out map[string]MigrationStatus
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode migration status file: %w", err)
	}
	return out, nil
}

func (s *fileMigrationStatusStore) persist(all map[string]MigrationStatus) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir for migration status file: %w", err)
	}
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return fmt.Errorf("encode migration status file: %w", err)
	}
	return os.WriteFile(s.path, b, 0o644)
}

type fileMigrationAuditSink struct {
	path string
	mu   sync.Mutex
}

func newFileMigrationAuditSink(path string) (*fileMigrationAuditSink, error) {
	if path == "" {
		return nil, errors.New("empty migration audit file path")
	}
	return &fileMigrationAuditSink{path: path}, nil
}

func NewFileMigrationAuditSink(path string) (MigrationAuditSink, error) {
	return newFileMigrationAuditSink(path)
}

func (s *fileMigrationAuditSink) Write(_ context.Context, event MigrationAuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir for migration audit file: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open migration audit file: %w", err)
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(event)
}

type loggerMigrationAuditSink struct {
	log Logger
}

func (s loggerMigrationAuditSink) Write(_ context.Context, event MigrationAuditEvent) error {
	if s.log != nil {
		s.log.Info("migration audit",
			"phase", event.Phase,
			"job", event.Job,
			"target", event.TargetNode,
			"error", event.Error)
	}
	return nil
}

type migrationHandlers struct {
	exec  MigrationExecutor
	log   Logger
	store MigrationStatusStore
	audit MigrationAuditSink
}

func newMigrationHandlers(exec MigrationExecutor, log Logger, store MigrationStatusStore, audit MigrationAuditSink) *migrationHandlers {
	if exec == nil {
		exec = noopMigrationExecutor{}
	}
	if store == nil {
		store = newMemoryMigrationStatusStore()
	}
	if audit == nil {
		audit = loggerMigrationAuditSink{log: log}
	}
	return &migrationHandlers{
		exec:  exec,
		log:   log,
		store: store,
		audit: audit,
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
	now := time.Now().UTC()
	initial := MigrationStatus{
		Job:        job,
		Namespace:  ns,
		Name:       name,
		TargetNode: req.TargetNode,
		Status:     "running",
		StartedAt:  now,
		UpdatedAt:  now,
	}
	if err := h.store.Save(r.Context(), initial); err != nil {
		h.log.Error("persist migration running status failed", "job", job, "err", err)
		errResp(w, http.StatusInternalServerError, "failed to persist migration status")
		return
	}
	_ = h.audit.Write(r.Context(), MigrationAuditEvent{
		When:       now,
		Phase:      "started",
		Job:        job,
		Namespace:  ns,
		Name:       name,
		TargetNode: req.TargetNode,
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		finished := time.Now().UTC()
		if err := h.exec.Execute(ctx, ns, name, req.TargetNode); err != nil {
			h.log.Error("migration failed", "job", job, "target", req.TargetNode, "err", err)
			failed := MigrationStatus{
				Job:         job,
				Namespace:   ns,
				Name:        name,
				TargetNode:  req.TargetNode,
				Status:      "failed",
				StartedAt:   initial.StartedAt,
				UpdatedAt:   finished,
				CompletedAt: &finished,
				Error:       err.Error(),
			}
			if saveErr := h.store.Save(context.Background(), failed); saveErr != nil {
				h.log.Error("persist migration failed status failed", "job", job, "err", saveErr)
			}
			_ = h.audit.Write(context.Background(), MigrationAuditEvent{
				When:       finished,
				Phase:      "failed",
				Job:        job,
				Namespace:  ns,
				Name:       name,
				TargetNode: req.TargetNode,
				Error:      err.Error(),
			})
			return
		}
		done := MigrationStatus{
			Job:         job,
			Namespace:   ns,
			Name:        name,
			TargetNode:  req.TargetNode,
			Status:      "done",
			StartedAt:   initial.StartedAt,
			UpdatedAt:   finished,
			CompletedAt: &finished,
		}
		if err := h.store.Save(context.Background(), done); err != nil {
			h.log.Error("persist migration done status failed", "job", job, "err", err)
		}
		_ = h.audit.Write(context.Background(), MigrationAuditEvent{
			When:       finished,
			Phase:      "done",
			Job:        job,
			Namespace:  ns,
			Name:       name,
			TargetNode: req.TargetNode,
		})
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
	status, found, err := h.store.Get(r.Context(), ns, name)
	if err != nil {
		h.log.Error("load migration status failed", "job", job, "err", err)
		errResp(w, http.StatusInternalServerError, "failed to load migration status")
		return
	}
	if !found {
		ok(w, map[string]string{
			"job":    job,
			"status": "no_migration_in_progress",
		})
		return
	}
	ok(w, status)
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
