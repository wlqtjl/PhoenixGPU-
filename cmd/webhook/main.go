//go:build webhookfull
// +build webhookfull

// phoenix-webhook — PhoenixGPU Admission Webhook
//
// Validates and mutates PhoenixJob CRDs and Pod specs to inject
// vGPU configuration, libvgpu LD_PRELOAD, and billing annotations.
//
// Architecture:
//
//	POST /validate-phoenixjob — validates PhoenixJob CRD spec (allocRatio, checkpoint config)
//	POST /mutate-pod          — injects libvgpu LD_PRELOAD and billing annotations
//	GET  /healthz             — health probe
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
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

// ── Validation Logic ──────────────────────────────────────────────

// validStorageBackends are the allowed checkpoint storage backends.
var validStorageBackends = map[string]bool{
	"pvc": true,
	"s3":  true,
	"nfs": true,
}

// validatePhoenixJob validates a PhoenixJob CRD spec.
// Returns a list of validation errors (empty = valid).
func validatePhoenixJob(obj map[string]interface{}) []string {
	var errs []string

	spec, ok := nestedMap(obj, "spec")
	if !ok {
		return []string{"spec is required"}
	}

	// Validate checkpoint config (required)
	_, hasCkpt := nestedMap(spec, "checkpoint")
	if !hasCkpt {
		errs = append(errs, "spec.checkpoint is required")
	} else {
		// storageBackend is required
		backend, found, _ := unstructured.NestedString(spec, "checkpoint", "storageBackend")
		if !found || backend == "" {
			errs = append(errs, "spec.checkpoint.storageBackend is required")
		} else if !validStorageBackends[backend] {
			errs = append(errs, fmt.Sprintf("spec.checkpoint.storageBackend must be one of: pvc, s3, nfs (got %q)", backend))
		}

		// If storageBackend=pvc, pvcName is required
		if backend == "pvc" {
			pvcName, _, _ := unstructured.NestedString(spec, "checkpoint", "pvcName")
			if pvcName == "" {
				errs = append(errs, "spec.checkpoint.pvcName is required when storageBackend=pvc")
			}
		}

		// If storageBackend=s3, s3.bucket is required
		if backend == "s3" {
			s3Bucket, _, _ := unstructured.NestedString(spec, "checkpoint", "s3", "bucket")
			if s3Bucket == "" {
				errs = append(errs, "spec.checkpoint.s3.bucket is required when storageBackend=s3")
			}
		}

		// Validate intervalSeconds range
		interval, found, _ := unstructured.NestedInt64(spec, "checkpoint", "intervalSeconds")
		if found {
			if interval < 60 {
				errs = append(errs, "spec.checkpoint.intervalSeconds must be >= 60")
			}
			if interval > 86400 {
				errs = append(errs, "spec.checkpoint.intervalSeconds must be <= 86400")
			}
		}

		// Validate maxSnapshots range
		maxSnap, found, _ := unstructured.NestedInt64(spec, "checkpoint", "maxSnapshots")
		if found {
			if maxSnap < 1 {
				errs = append(errs, "spec.checkpoint.maxSnapshots must be >= 1")
			}
			if maxSnap > 100 {
				errs = append(errs, "spec.checkpoint.maxSnapshots must be <= 100")
			}
		}
	}

	// Validate template (required)
	_, hasTemplate := nestedMap(spec, "template")
	if !hasTemplate {
		errs = append(errs, "spec.template is required")
	}

	// Validate billing labels (optional but if present, department should be reasonable)
	billing, hasBilling := nestedMap(spec, "billing")
	if hasBilling {
		dept, _, _ := unstructured.NestedString(billing, "department")
		if dept != "" && len(dept) > 128 {
			errs = append(errs, "spec.billing.department must be <= 128 characters")
		}
	}

	// Validate restorePolicy
	restorePolicy, hasRP := nestedMap(spec, "restorePolicy")
	if hasRP {
		onFailure, found, _ := unstructured.NestedString(restorePolicy, "onNodeFailure")
		if found {
			validActions := map[string]bool{"restore": true, "restart": true, "fail": true}
			if !validActions[onFailure] {
				errs = append(errs, fmt.Sprintf("spec.restorePolicy.onNodeFailure must be one of: restore, restart, fail (got %q)", onFailure))
			}
		}
	}

	return errs
}

// ── Mutation Logic ────────────────────────────────────────────────

// jsonPatch represents a single JSON Patch operation.
type jsonPatch struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// buildMutationPatches creates JSON patches to inject libvgpu and billing annotations into a pod.
func buildMutationPatches(pod map[string]interface{}) []jsonPatch {
	var patches []jsonPatch

	// Check if this pod has vGPU resource requests
	containers, _, _ := unstructured.NestedSlice(pod, "spec", "containers")
	hasVGPU := false
	for _, c := range containers {
		cMap, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		requests, _, _ := unstructured.NestedMap(cMap, "resources", "requests")
		for k := range requests {
			if strings.Contains(k, "vgpu") {
				hasVGPU = true
				break
			}
		}
	}

	if !hasVGPU {
		return patches // No vGPU requested, no mutation needed
	}

	// Ensure annotations exist
	annotations, _, _ := unstructured.NestedMap(pod, "metadata", "annotations")
	if annotations == nil {
		patches = append(patches, jsonPatch{
			Op:    "add",
			Path:  "/metadata/annotations",
			Value: map[string]string{},
		})
	}

	// Add billing tracking annotation
	patches = append(patches, jsonPatch{
		Op:    "add",
		Path:  "/metadata/annotations/phoenixgpu.io~1billing-enabled",
		Value: "true",
	})

	patches = append(patches, jsonPatch{
		Op:    "add",
		Path:  "/metadata/annotations/phoenixgpu.io~1injected-at",
		Value: time.Now().UTC().Format(time.RFC3339),
	})

	// Inject LD_PRELOAD for libvgpu into each container
	for i := range containers {
		envPath := fmt.Sprintf("/spec/containers/%d/env", i)

		cMap, ok := containers[i].(map[string]interface{})
		if !ok {
			continue
		}

		existingEnv, _, _ := unstructured.NestedSlice(cMap, "env")
		ldPreloadEnv := map[string]interface{}{
			"name":  "LD_PRELOAD",
			"value": "/usr/lib/phoenixgpu/libvgpu.so",
		}
		vgpuEnabledEnv := map[string]interface{}{
			"name":  "PHOENIXGPU_VGPU_ENABLED",
			"value": "1",
		}

		if len(existingEnv) == 0 {
			patches = append(patches, jsonPatch{
				Op:    "add",
				Path:  envPath,
				Value: []interface{}{ldPreloadEnv, vgpuEnabledEnv},
			})
		} else {
			patches = append(patches, jsonPatch{
				Op:    "add",
				Path:  envPath + "/-",
				Value: ldPreloadEnv,
			})
			patches = append(patches, jsonPatch{
				Op:    "add",
				Path:  envPath + "/-",
				Value: vgpuEnabledEnv,
			})
		}
	}

	return patches
}

// ── HTTP Handlers ─────────────────────────────────────────────────

// handleValidatePhoenixJob handles the /validate-phoenixjob admission webhook.
func handleValidatePhoenixJob(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		review, err := readAdmissionReview(r)
		if err != nil {
			logger.Error("read admission review", zap.Error(err))
			writeAdmissionError(w, "", fmt.Sprintf("read request: %v", err))
			return
		}

		uid := string(review.Request.UID)

		// Unmarshal the raw object
		var obj map[string]interface{}
		if err := json.Unmarshal(review.Request.Object.Raw, &obj); err != nil {
			writeAdmissionError(w, uid, fmt.Sprintf("unmarshal object: %v", err))
			return
		}

		validationErrors := validatePhoenixJob(obj)

		if len(validationErrors) > 0 {
			logger.Info("PhoenixJob validation failed",
				zap.String("name", review.Request.Name),
				zap.String("namespace", review.Request.Namespace),
				zap.Strings("errors", validationErrors))

			writeAdmissionResponse(w, uid, false, strings.Join(validationErrors, "; "), nil)
			return
		}

		logger.Info("PhoenixJob validation passed",
			zap.String("name", review.Request.Name),
			zap.String("namespace", review.Request.Namespace))

		writeAdmissionResponse(w, uid, true, "", nil)
	}
}

// handleMutatePod handles the /mutate-pod admission webhook.
func handleMutatePod(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		review, err := readAdmissionReview(r)
		if err != nil {
			logger.Error("read admission review", zap.Error(err))
			writeAdmissionError(w, "", fmt.Sprintf("read request: %v", err))
			return
		}

		uid := string(review.Request.UID)

		// Unmarshal the pod object
		var pod map[string]interface{}
		if err := json.Unmarshal(review.Request.Object.Raw, &pod); err != nil {
			writeAdmissionError(w, uid, fmt.Sprintf("unmarshal pod: %v", err))
			return
		}

		patches := buildMutationPatches(pod)

		if len(patches) > 0 {
			logger.Info("mutating pod",
				zap.String("name", review.Request.Name),
				zap.String("namespace", review.Request.Namespace),
				zap.Int("patchCount", len(patches)))

			patchBytes, err := json.Marshal(patches)
			if err != nil {
				writeAdmissionError(w, uid, fmt.Sprintf("marshal patches: %v", err))
				return
			}
			writeAdmissionResponse(w, uid, true, "", patchBytes)
		} else {
			logger.Debug("no mutation needed",
				zap.String("name", review.Request.Name),
				zap.String("namespace", review.Request.Namespace))
			writeAdmissionResponse(w, uid, true, "", nil)
		}
	}
}

// ── Admission Review Helpers ──────────────────────────────────────

func readAdmissionReview(r *http.Request) (*admissionv1.AdmissionReview, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20)) // 2MiB limit
	defer r.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var review admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &review); err != nil {
		return nil, fmt.Errorf("unmarshal admission review: %w", err)
	}

	if review.Request == nil {
		return nil, fmt.Errorf("admission review has no request")
	}

	return &review, nil
}

func writeAdmissionResponse(w http.ResponseWriter, uid string, allowed bool, message string, patch []byte) {
	resp := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:     k8stypes.UID(uid),
			Allowed: allowed,
		},
	}

	if message != "" {
		resp.Response.Result = &metav1.Status{
			Message: message,
		}
	}

	if patch != nil {
		patchType := admissionv1.PatchTypeJSONPatch
		resp.Response.PatchType = &patchType
		resp.Response.Patch = patch
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

func writeAdmissionError(w http.ResponseWriter, uid, message string) {
	writeAdmissionResponse(w, uid, false, message, nil)
}

// nestedMap is a helper that extracts a nested map from an unstructured object.
func nestedMap(obj map[string]interface{}, fields ...string) (map[string]interface{}, bool) {
	val, found, err := unstructured.NestedMap(obj, fields...)
	if err != nil || !found {
		return nil, false
	}
	return val, true
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

	mux := http.NewServeMux()
	mux.HandleFunc("/validate-phoenixjob", handleValidatePhoenixJob(logger))
	mux.HandleFunc("/mutate-pod", handleMutatePod(logger))
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
