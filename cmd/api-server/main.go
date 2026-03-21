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
	mock            bool
	enableMigration bool
	promURL         string
}

func main() {
	o := &opts{}
	flag.StringVar(&o.addr, "addr", ":8090", "HTTP listen address")
	flag.BoolVar(&o.mock, "mock", false, "Use fake data")
	flag.BoolVar(&o.enableMigration, "enable-migration", false, "Enable migration APIs")
	flag.StringVar(&o.promURL, "prom-url", "http://prometheus.monitoring.svc:9090", "Prometheus base URL for real K8s mode")
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
		return err
	}

	router := internal.NewRouter(internal.RouterConfig{
		K8sClient:       client,
		Logger:          internal.NewNopLogger(),
		EnableMock:      o.mock,
		EnableMigration: o.enableMigration,
	})

	srv := &http.Server{Addr: o.addr, Handler: router, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
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
