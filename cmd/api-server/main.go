// phoenix-api-server — PhoenixGPU REST API Server
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/wlqtjl/PhoenixGPU/cmd/api-server/internal"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type opts struct {
	addr            string
	metricsAddr     string
	promURL         string
	mock            bool
	enableMigration bool
	tlsCert         string
	tlsKey          string
	authTokens      string
	rateLimitRPS    float64
}

func main() {
	o := &opts{}
	flag.StringVar(&o.addr, "addr", ":8090", "HTTP listen address")
	flag.StringVar(&o.metricsAddr, "metrics-addr", "", "Legacy metrics listen address flag (accepted for backward compatibility; currently unused)")
	flag.StringVar(&o.promURL, "prometheus-url", "http://prometheus:9090", "Prometheus server URL for DCGM metrics")
	flag.BoolVar(&o.mock, "mock", false, "Use fake data")
	flag.BoolVar(&o.enableMigration, "enable-migration", false, "Enable migration APIs")
	flag.StringVar(&o.tlsCert, "tls-cert", "", "Path to TLS certificate file (enables HTTPS)")
	flag.StringVar(&o.tlsKey, "tls-key", "", "Path to TLS private key file (requires --tls-cert)")
	flag.StringVar(&o.authTokens, "auth-tokens", "", "Comma-separated list of valid bearer tokens (empty disables auth)")
	flag.Float64Var(&o.rateLimitRPS, "rate-limit-rps", 0, "Per-IP rate limit in requests per second (0 disables)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("%s (%s, %s)\n", version, commit, date)
		return
	}
	if err := run(o); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run(o *opts) error {
	client, err := buildK8sClient(o)
	if err != nil {
		log.Printf("warning: %v — falling back to mock data", err)
		client = internal.NewFakeK8sClient()
	}

	// Parse auth tokens
	authTokens := make(map[string]bool)
	if o.authTokens != "" {
		for _, t := range strings.Split(o.authTokens, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				authTokens[t] = true
			}
		}
	}

	router := internal.NewRouter(internal.RouterConfig{
		K8sClient:       client,
		Logger:          internal.NewNopLogger(),
		EnableMock:      o.mock,
		EnableMigration: o.enableMigration,
		AuthTokens:      authTokens,
		RateLimitRPS:    o.rateLimitRPS,
	})

	srv := &http.Server{Addr: o.addr, Handler: router, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		var listenErr error
		if o.tlsCert != "" && o.tlsKey != "" {
			log.Printf("starting HTTPS server on %s", o.addr)
			listenErr = srv.ListenAndServeTLS(o.tlsCert, o.tlsKey)
		} else {
			if o.tlsCert != "" || o.tlsKey != "" {
				log.Printf("warning: both --tls-cert and --tls-key must be set; falling back to HTTP")
			}
			log.Printf("starting HTTP server on %s (use --tls-cert and --tls-key for HTTPS)", o.addr)
			listenErr = srv.ListenAndServe()
		}
		if listenErr != nil && listenErr != http.ErrServerClosed {
			errCh <- listenErr
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-quit:
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}
