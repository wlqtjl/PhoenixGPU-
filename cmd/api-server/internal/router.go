// Package internal — shared types and router for the PhoenixGPU API server.
package internal

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
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
	K8sClient  K8sClientInterface
	Logger     Logger
	EnableMock bool
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
			cfg.K8sClient = unavailableK8sClient{}
		}
	}
	h := &handlers{client: cfg.K8sClient, log: cfg.Logger}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/api/v1/cluster/summary", method(http.MethodGet, h.getClusterSummary))
	mux.HandleFunc("/api/v1/cluster/utilization-history", method(http.MethodGet, h.getUtilHistory))
	mux.HandleFunc("/api/v1/nodes", method(http.MethodGet, h.listNodes))
	mux.HandleFunc("/api/v1/jobs", method(http.MethodGet, h.listJobs))
	mux.HandleFunc("/api/v1/billing/departments", method(http.MethodGet, h.listBillingDepartments))
	mux.HandleFunc("/api/v1/billing/records", method(http.MethodGet, h.listBillingRecords))
	mux.HandleFunc("/api/v1/alerts", method(http.MethodGet, h.listAlerts))

	// dynamic routes
	mux.HandleFunc("/api/v1/jobs/", func(w http.ResponseWriter, r *http.Request) {
		if stringsHasSuffix(r.URL.Path, "/checkpoint") {
			method(http.MethodPost, h.triggerCheckpoint)(w, r)
			return
		}
		method(http.MethodGet, h.getJob)(w, r)
	})
	mux.HandleFunc("/api/v1/alerts/", func(w http.ResponseWriter, r *http.Request) {
		if stringsHasSuffix(r.URL.Path, "/resolve") {
			method(http.MethodPost, h.resolveAlert)(w, r)
			return
		}
		http.NotFound(w, r)
	})

	return withMetrics(mux)
}

func method(want string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != want {
			w.Header().Set("Allow", want)
			errResp(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		next(w, r)
	}
}

func withMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		_ = start
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

func stringsHasSuffix(s, suffix string) bool {
	if len(suffix) > len(s) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
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

func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
