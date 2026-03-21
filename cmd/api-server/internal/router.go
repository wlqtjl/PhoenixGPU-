// Package internal — shared types and router for the PhoenixGPU API server.
package internal

import (
	"context"
	"encoding/json"
	"errors"
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
	K8sClient         K8sClientInterface
	Logger            Logger
	EnableMock        bool
	EnableMigration   bool
	MigrationExecutor MigrationExecutor
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
	if cfg.K8sClient == nil {
		if cfg.EnableMock {
			cfg.Logger.Warn("nil K8sClient, fallback to FakeK8sClient because mock is enabled")
			cfg.K8sClient = NewFakeK8sClient()
		} else {
			cfg.Logger.Error("nil K8sClient in non-mock mode, serving unavailable responses")
			cfg.K8sClient = unavailableK8sClient{}
		}
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// Middleware: structured logging + Prometheus + recovery
	r.Use(metricsMiddleware())
	r.Use(requestLogger(cfg.Logger))
	r.Use(gin.RecoveryWithWriter(nil, func(c *gin.Context, err interface{}) {
		cfg.Logger.Error("panic recovered", zap.Any("error", err))
		errResp(c, http.StatusInternalServerError, "internal server error")
	}))

	// Health
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/readyz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	// Prometheus metrics scrape endpoint
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	h := &handlers{client: cfg.K8sClient, log: cfg.Logger}
	mh := newMigrationHandlers(cfg.MigrationExecutor, cfg.Logger)

	v1 := r.Group("/api/v1")
	{
		// Cluster
		v1.GET("/cluster/summary", h.getClusterSummary)
		v1.GET("/cluster/utilization-history", h.getUtilHistory)

		// Nodes
		v1.GET("/nodes", h.listNodes)

		// PhoenixJobs
		v1.GET("/jobs", h.listJobs)
		v1.GET("/jobs/:namespace/:name", h.getJob)
		v1.POST("/jobs/:namespace/:name/checkpoint", h.triggerCheckpoint)

		// Billing
		v1.GET("/billing/departments", h.listBillingDepartments)
		v1.GET("/billing/records", h.listBillingRecords)

		// Alerts
		v1.GET("/alerts", h.listAlerts)
		v1.POST("/alerts/:id/resolve", h.resolveAlert)
	}
}

func withMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		c.Next()

		dur := time.Since(start)
		status := http.StatusText(c.Writer.Status())

func (w *statusWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func requestLogger(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("dur", time.Since(start)),
			zap.String("ip", c.ClientIP()),
		)
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

// unavailableK8sClient provides explicit errors when no backend client is configured.
// This avoids nil-pointer panics in handlers and returns predictable 5xx responses.
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
