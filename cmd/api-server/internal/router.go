// Package internal — shared types and router for the PhoenixGPU API server.
package internal

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
)

// APIResponse is the unified response envelope for all API endpoints.
type APIResponse struct {
	Data  interface{} `json:"data"`
	Error string      `json:"error,omitempty"`
	Meta  *APIMeta    `json:"meta,omitempty"`
}

type APIMeta struct {
	Total     int       `json:"total,omitempty"`
	Page      int       `json:"page,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

func jsonResponse(w http.ResponseWriter, status int, payload APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
func ok(w http.ResponseWriter, data interface{}) {
	jsonResponse(w, http.StatusOK, APIResponse{Data: data, Meta: &APIMeta{Timestamp: time.Now().UTC()}})
}
func okMeta(w http.ResponseWriter, data interface{}, total int) {
	jsonResponse(w, http.StatusOK, APIResponse{Data: data, Meta: &APIMeta{Total: total, Timestamp: time.Now().UTC()}})
}
func errResp(w http.ResponseWriter, status int, msg string) {
	jsonResponse(w, status, APIResponse{Error: msg})
}

type RouterConfig struct {
	K8sClient         K8sClientInterface
	Logger            Logger
	EnableMock        bool
	EnableMigration   bool
	MigrationExecutor MigrationExecutor
	GovernanceService *GovernanceService
}

type Logger interface {
	Info(msg string, kv ...interface{})
	Warn(msg string, kv ...interface{})
	Error(msg string, kv ...interface{})
}

type stdLogger struct{}

func (stdLogger) Info(msg string, kv ...interface{}) {
	log.Println(append([]interface{}{"INFO", msg}, kv...)...)
}
func (stdLogger) Warn(msg string, kv ...interface{}) {
	log.Println(append([]interface{}{"WARN", msg}, kv...)...)
}
func (stdLogger) Error(msg string, kv ...interface{}) {
	log.Println(append([]interface{}{"ERROR", msg}, kv...)...)
}

func NewRouter(cfg RouterConfig) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = stdLogger{}
	}
	if cfg.K8sClient == nil {
		if cfg.EnableMock {
			cfg.Logger.Warn("nil K8sClient, fallback to FakeK8sClient")
			cfg.K8sClient = NewFakeK8sClient()
		} else {
			cfg.Logger.Error("nil K8sClient in non-mock mode, serving unavailable responses")
			cfg.K8sClient = unavailableK8sClient{}
		}
	}

	h := &handlers{client: cfg.K8sClient, log: cfg.Logger}
	mh := newMigrationHandlers(cfg.MigrationExecutor, cfg.Logger)
	if cfg.GovernanceService == nil {
		cfg.GovernanceService = NewGovernanceService()
	}
	gh := &governanceHandlers{service: cfg.GovernanceService, log: cfg.Logger}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("# metrics endpoint\n"))
	})

	mux.Handle("/api/v1/cluster/summary", method(http.MethodGet, http.HandlerFunc(h.getClusterSummary)))
	mux.Handle("/api/v1/cluster/utilization-history", method(http.MethodGet, http.HandlerFunc(h.getUtilHistory)))
	mux.Handle("/api/v1/nodes", method(http.MethodGet, http.HandlerFunc(h.listNodes)))
	mux.Handle("/api/v1/jobs", method(http.MethodGet, http.HandlerFunc(h.listJobs)))
	mux.Handle("/api/v1/billing/departments", method(http.MethodGet, http.HandlerFunc(h.listBillingDepartments)))
	mux.Handle("/api/v1/billing/records", method(http.MethodGet, http.HandlerFunc(h.listBillingRecords)))
	mux.Handle("/api/v1/alerts", method(http.MethodGet, http.HandlerFunc(h.listAlerts)))

	mux.Handle("/api/v1/control/agents", withMethods([]string{http.MethodGet, http.MethodPost}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gh.listAgents(w, r)
			return
		}
		gh.createAgent(w, r)
	})))
	mux.Handle("/api/v1/control/tasks", withMethods([]string{http.MethodGet, http.MethodPost}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gh.listTasks(w, r)
			return
		}
		gh.createTask(w, r)
	})))
	mux.Handle("/api/v1/control/audit/events", method(http.MethodGet, http.HandlerFunc(gh.listAuditEvents)))
	mux.Handle("/api/v1/control/decision-cards", method(http.MethodGet, http.HandlerFunc(gh.listDecisionCards)))
	mux.Handle("/api/v1/control/budgets", method(http.MethodGet, http.HandlerFunc(gh.listBudgetLedgers)))

	mux.Handle("/api/v1/jobs/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/migrate") {
			if !cfg.EnableMigration {
				http.NotFound(w, r)
				return
			}
			method(http.MethodPost, http.HandlerFunc(mh.triggerMigration)).ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/migration-status") {
			if !cfg.EnableMigration {
				http.NotFound(w, r)
				return
			}
			method(http.MethodGet, http.HandlerFunc(mh.getMigrationStatus)).ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/checkpoint") {
			method(http.MethodPost, http.HandlerFunc(h.triggerCheckpoint)).ServeHTTP(w, r)
			return
		}
		method(http.MethodGet, http.HandlerFunc(h.getJob)).ServeHTTP(w, r)
	}))

	mux.Handle("/api/v1/alerts/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/resolve") {
			http.NotFound(w, r)
			return
		}
		method(http.MethodPost, http.HandlerFunc(h.resolveAlert)).ServeHTTP(w, r)
	}))

	mux.Handle("/api/v1/control/tasks/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/approvals/approve") {
			method(http.MethodPost, http.HandlerFunc(gh.approveTask)).ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	}))

	return withRecovery(requestLogger(cfg.Logger, withMetrics(mux)))
}

func method(m string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != m {
			errResp(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				errResp(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func withMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func requestLogger(log Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, r)
		log.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "dur", time.Since(start))
	})
}

func NewNopLogger() Logger { return nopLogger{} }

type nopLogger struct{}

func (nopLogger) Info(string, ...interface{})  {}
func (nopLogger) Warn(string, ...interface{})  {}
func (nopLogger) Error(string, ...interface{}) {}

// unavailableK8sClient provides explicit errors when no backend client is configured.
type unavailableK8sClient struct{}

func (unavailableK8sClient) GetClusterSummary(context.Context) (*ClusterSummary, error) {
	return nil, errors.New("k8s client unavailable")
}
func (unavailableK8sClient) GetUtilizationHistory(context.Context, int) ([]TimeSeriesPoint, error) {
	return nil, errors.New("k8s client unavailable")
}
func (unavailableK8sClient) ListGPUNodes(context.Context) ([]GPUNode, error) {
	return nil, errors.New("k8s client unavailable")
}
func (unavailableK8sClient) ListPhoenixJobs(context.Context, string) ([]PhoenixJob, error) {
	return nil, errors.New("k8s client unavailable")
}
func (unavailableK8sClient) GetPhoenixJob(context.Context, string, string) (*PhoenixJob, error) {
	return nil, errors.New("k8s client unavailable")
}
func (unavailableK8sClient) TriggerCheckpoint(context.Context, string, string) error {
	return errors.New("k8s client unavailable")
}
func (unavailableK8sClient) GetBillingByDepartment(context.Context, string) ([]DeptBilling, error) {
	return nil, errors.New("k8s client unavailable")
}
func (unavailableK8sClient) GetBillingRecords(context.Context, string) ([]BillingRecord, error) {
	return nil, errors.New("k8s client unavailable")
}
func (unavailableK8sClient) ListAlerts(context.Context) ([]Alert, error) {
	return nil, errors.New("k8s client unavailable")
}
func (unavailableK8sClient) ResolveAlert(context.Context, string) error {
	return errors.New("k8s client unavailable")
}
