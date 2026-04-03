//go:build webhookfull
// +build webhookfull

// phoenix-webhook — PhoenixGPU Admission Webhook
//
// Validates and mutates PhoenixJob CRDs and Pod specs to inject
// vGPU configuration, libvgpu LD_PRELOAD, and billing annotations.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type options struct {
	addr     string
	certFile string
	keyFile  string
}

func main() {
	opts := &options{}

	root := &cobra.Command{
		Use:   "webhook",
		Short: "PhoenixGPU Admission Webhook — validate and mutate GPU workloads",
		Long: `webhook provides Kubernetes admission control for PhoenixGPU:
  1. Validates PhoenixJob CRD specs (allocRatio, checkpoint config)
  2. Mutates Pod specs to inject libvgpu via LD_PRELOAD
  3. Adds billing labels and annotations
  4. Enforces quota hard limits before pod scheduling`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		RunE:    func(cmd *cobra.Command, args []string) error { return run(opts) },
	}

	f := root.Flags()
	f.StringVar(&opts.addr, "addr", ":9443", "HTTPS listen address for webhook")
	f.StringVar(&opts.certFile, "tls-cert-file", "/certs/tls.crt", "TLS certificate file")
	f.StringVar(&opts.keyFile, "tls-key-file", "/certs/tls.key", "TLS private key file")

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(opts *options) error {
	logger, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck

	logger.Info("starting webhook",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("addr", opts.addr))

	// TODO Sprint 7: implement admission webhook handlers
	// POST /validate-phoenixjob — validate PhoenixJob CRD spec
	// POST /mutate-pod           — inject libvgpu LD_PRELOAD and billing annotations
	mux := http.NewServeMux()
	mux.HandleFunc("/validate-phoenixjob", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Allow all requests (pass-through) until real validation logic
		fmt.Fprintf(w, `{"apiVersion":"admission.k8s.io/v1","kind":"AdmissionReview","response":{"uid":"","allowed":true}}`)
	})
	mux.HandleFunc("/mutate-pod", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// No-op mutation until real injection logic
		fmt.Fprintf(w, `{"apiVersion":"admission.k8s.io/v1","kind":"AdmissionReview","response":{"uid":"","allowed":true}}`)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	srv := &http.Server{
		Addr:         opts.addr,
		Handler:      mux,
		TLSConfig:    tlsCfg,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("webhook listening (TLS)", zap.String("addr", opts.addr))
		if err := srv.ListenAndServeTLS(opts.certFile, opts.keyFile); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-quit:
		logger.Info("received signal, shutting down", zap.String("signal", sig.String()))
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}
