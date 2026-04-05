// Package internal — shared types and router for the PhoenixGPU API server.
package internal

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
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

	// AuthTokens is a set of valid bearer tokens. If empty, authentication is disabled.
	AuthTokens map[string]bool
	// RateLimitRPS is the maximum requests per second per client IP. 0 disables rate limiting.
	RateLimitRPS float64
	// RateLimitBurst is the maximum burst size for rate limiting.
	RateLimitBurst int
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

	return withRecovery(requestLogger(cfg.Logger, withRateLimit(cfg, withAuth(cfg, withMetrics(mux)))))
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

// ─── Rate limiting middleware ────────────────────────────────────────────────

// ipRateLimiter maintains per-IP rate limiters with automatic cleanup of stale entries.
type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rateLimiterEntry
	rps      rate.Limit
	burst    int
}

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newIPRateLimiter(rps float64, burst int) *ipRateLimiter {
	rl := &ipRateLimiter{
		limiters: make(map[string]*rateLimiterEntry),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
	// Periodically clean up stale entries to prevent memory growth
	go rl.cleanupLoop()
	return rl
}

func (i *ipRateLimiter) getLimiter(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()
	entry, exists := i.limiters[ip]
	if !exists {
		entry = &rateLimiterEntry{
			limiter:  rate.NewLimiter(i.rps, i.burst),
			lastSeen: time.Now(),
		}
		i.limiters[ip] = entry
	} else {
		entry.lastSeen = time.Now()
	}
	return entry.limiter
}

// cleanupLoop removes rate limiter entries that haven't been seen for 10 minutes.
func (i *ipRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		i.mu.Lock()
		cutoff := time.Now().Add(-10 * time.Minute)
		for ip, entry := range i.limiters {
			if entry.lastSeen.Before(cutoff) {
				delete(i.limiters, ip)
			}
		}
		i.mu.Unlock()
	}
}

// withRateLimit adds per-IP rate limiting.
// If RateLimitRPS is 0, rate limiting is disabled.
func withRateLimit(cfg RouterConfig, next http.Handler) http.Handler {
	if cfg.RateLimitRPS <= 0 {
		return next // rate limiting disabled
	}
	burst := cfg.RateLimitBurst
	if burst <= 0 {
		// Default burst to 2× RPS (minimum 10) to allow short traffic spikes
		// while still enforcing the average rate.
		burst = int(cfg.RateLimitRPS * 2)
		if burst < 10 {
			burst = 10
		}
	}
	limiter := newIPRateLimiter(cfg.RateLimitRPS, burst)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limiting for health probes
		if publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		ip := r.RemoteAddr
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			ip = strings.Split(fwd, ",")[0]
		}
		ip = strings.TrimSpace(ip)

		if !limiter.getLimiter(ip).Allow() {
			w.Header().Set("Retry-After", "1")
			errResp(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}
