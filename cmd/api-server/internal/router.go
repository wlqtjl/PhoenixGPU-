// Package internal — shared types and router for the PhoenixGPU API server.
//
// All HTTP handlers live here. cmd/api-server/main.go is the entrypoint.
//
// Engineering Covenant §Sprint4:
//   - All handlers have request timeout (10s default)
//   - Unified APIResponse envelope: {data, error, meta}
//   - Structured zap logging on every request
//   - Prometheus metrics: request count, latency, error rate
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package internal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// ── API envelope ──────────────────────────────────────────────────

// APIResponse is the unified response envelope for all API endpoints.
type APIResponse struct {
	Data  interface{} `json:"data"`
	Error string      `json:"error,omitempty"`
	Meta  *APIMeta    `json:"meta,omitempty"`
}

// APIMeta carries pagination and timing metadata.
type APIMeta struct {
	Total     int       `json:"total,omitempty"`
	Page      int       `json:"page,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

func ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, APIResponse{
		Data: data,
		Meta: &APIMeta{Timestamp: time.Now().UTC()},
	})
}

func okMeta(c *gin.Context, data interface{}, total int) {
	c.JSON(http.StatusOK, APIResponse{
		Data: data,
		Meta: &APIMeta{Total: total, Timestamp: time.Now().UTC()},
	})
}

func errResp(c *gin.Context, status int, msg string) {
	c.JSON(status, APIResponse{Error: msg})
}

// ── Router config ────────────────────────────────────────────────

type RouterConfig struct {
	K8sClient  K8sClientInterface
	Logger     *zap.Logger
	EnableMock bool
}

// ── Prometheus metrics ────────────────────────────────────────────

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "phoenixgpu_api_requests_total",
		Help: "Total HTTP requests by method, path, and status",
	}, []string{"method", "path", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "phoenixgpu_api_duration_seconds",
		Help:    "HTTP request duration",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2},
	}, []string{"method", "path"})
)

// ── Router ────────────────────────────────────────────────────────

// NewRouter builds the Gin router with all routes registered.
func NewRouter(cfg RouterConfig) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
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

	return r
}

// ── Middleware ────────────────────────────────────────────────────

func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		c.Next()

		dur := time.Since(start)
		status := http.StatusText(c.Writer.Status())

		httpRequests.WithLabelValues(c.Request.Method, path, status).Inc()
		httpDuration.WithLabelValues(c.Request.Method, path).Observe(dur.Seconds())
	}
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
}

// ── Nop logger helper for tests ───────────────────────────────────

func NewNopLogger() *zap.Logger { return zap.NewNop() }

// ── JSON helpers ──────────────────────────────────────────────────

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
