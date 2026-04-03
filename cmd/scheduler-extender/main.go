//go:build schedulerfull
// +build schedulerfull

// phoenix-scheduler-extender — PhoenixGPU Scheduler Extender
//
// Extends the Kubernetes scheduler with GPU-aware bin-packing/spreading,
// NUMA affinity, and topology-aware placement.
//
// Architecture:
//   POST /filter     — remove nodes that cannot fit the GPU request
//   POST /prioritize — score remaining nodes by bin-pack/spread policy
//   GET  /healthz    — health probe
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type options struct {
	addr             string
	metricsAddr      string
	schedulingPolicy string
	numaAware        bool
}

func main() {
	opts := &options{}

	root := &cobra.Command{
		Use:   "scheduler-extender",
		Short: "PhoenixGPU Scheduler Extender — GPU-aware pod scheduling",
		Long: `scheduler-extender provides the Kubernetes scheduler with:
  1. GPU-aware filter and prioritize webhooks
  2. Bin-pack or spread scheduling policies
  3. NUMA topology awareness for GPU/CPU affinity
  4. vGPU allocation tracking integration`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		RunE:    func(cmd *cobra.Command, args []string) error { return run(opts) },
	}

	f := root.Flags()
	f.StringVar(&opts.addr, "addr", ":8888", "HTTP listen address for scheduler extender")
	f.StringVar(&opts.metricsAddr, "metrics-bind-address", ":8084", "Metrics endpoint address")
	f.StringVar(&opts.schedulingPolicy, "scheduling-policy", "binpack", "Scheduling policy: binpack or spread")
	f.BoolVar(&opts.numaAware, "numa-aware", true, "Enable NUMA topology-aware scheduling")

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// ── Kubernetes Scheduler Extender Types ───────────────────────────

// ExtenderArgs is the request body from kube-scheduler to the filter/prioritize endpoints.
type ExtenderArgs struct {
	Pod       *corev1.Pod    `json:"Pod"`
	Nodes     *corev1.NodeList `json:"Nodes,omitempty"`
	NodeNames *[]string      `json:"NodeNames,omitempty"`
}

// ExtenderFilterResult is the response body for the filter endpoint.
type ExtenderFilterResult struct {
	Nodes       *corev1.NodeList   `json:"Nodes,omitempty"`
	NodeNames   *[]string          `json:"NodeNames,omitempty"`
	FailedNodes map[string]string  `json:"FailedNodes,omitempty"`
	Error       string             `json:"Error,omitempty"`
}

// HostPriority represents the priority of scheduling to a particular host.
type HostPriority struct {
	Host  string `json:"Host"`
	Score int64  `json:"Score"`
}

// vGPU resource name used for filtering and scoring.
const (
	vgpuResourceName       = "nvidia.com/vgpu"
	vgpuMemoryResourceName = "nvidia.com/vgpu-memory"
	maxPriorityScore       = 100
)

// ── Filter Logic ──────────────────────────────────────────────────

// filterHandler removes nodes that cannot satisfy the pod's vGPU request.
func filterHandler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var args ExtenderArgs
		if err := decodeBody(r, &args); err != nil {
			writeFilterError(w, fmt.Sprintf("decode request: %v", err))
			return
		}

		if args.Pod == nil {
			writeFilterError(w, "Pod is nil in ExtenderArgs")
			return
		}

		// Determine how many vGPU slices the pod requests
		requestedVGPU := getPodResourceRequest(args.Pod, vgpuResourceName)
		requestedMem := getPodResourceRequest(args.Pod, vgpuMemoryResourceName)

		logger.Debug("filter request",
			zap.String("pod", args.Pod.Name),
			zap.String("namespace", args.Pod.Namespace),
			zap.Int64("requestedVGPU", requestedVGPU),
			zap.Int64("requestedMemory", requestedMem))

		// If no vGPU resources requested, pass all nodes through
		if requestedVGPU == 0 && requestedMem == 0 {
			writeJSON(w, &ExtenderFilterResult{Nodes: args.Nodes, NodeNames: args.NodeNames})
			return
		}

		// Filter nodes
		result := &ExtenderFilterResult{
			FailedNodes: make(map[string]string),
		}

		if args.Nodes != nil {
			filteredNodes := &corev1.NodeList{}
			for _, node := range args.Nodes.Items {
				if canSchedule(node, requestedVGPU, requestedMem) {
					filteredNodes.Items = append(filteredNodes.Items, node)
				} else {
					result.FailedNodes[node.Name] = "insufficient vGPU resources"
				}
			}
			result.Nodes = filteredNodes
		}

		logger.Info("filter result",
			zap.String("pod", args.Pod.Name),
			zap.Int("passed", countFilteredNodes(result)),
			zap.Int("failed", len(result.FailedNodes)))

		writeJSON(w, result)
	}
}

// canSchedule checks if a node has enough vGPU resources for the request.
func canSchedule(node corev1.Node, requestedVGPU, requestedMem int64) bool {
	// Check allocatable vGPU resources
	if requestedVGPU > 0 {
		allocatable := getNodeResource(node, vgpuResourceName)
		if allocatable < requestedVGPU {
			return false
		}
	}

	// Check allocatable vGPU memory
	if requestedMem > 0 {
		allocatable := getNodeResource(node, vgpuMemoryResourceName)
		if allocatable < requestedMem {
			return false
		}
	}

	// Check node readiness
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady && cond.Status != corev1.ConditionTrue {
			return false
		}
	}

	return true
}

// getNodeResource returns the allocatable quantity for a resource on a node.
func getNodeResource(node corev1.Node, resourceName string) int64 {
	resName := corev1.ResourceName(resourceName)
	if q, ok := node.Status.Allocatable[resName]; ok {
		return q.Value()
	}
	return 0
}

func countFilteredNodes(result *ExtenderFilterResult) int {
	if result.Nodes != nil {
		return len(result.Nodes.Items)
	}
	return 0
}

// ── Prioritize Logic ──────────────────────────────────────────────

// prioritizeHandler scores nodes based on the configured scheduling policy.
func prioritizeHandler(policy string, numaAware bool, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var args ExtenderArgs
		if err := decodeBody(r, &args); err != nil {
			logger.Error("decode prioritize request", zap.Error(err))
			writeJSON(w, []HostPriority{})
			return
		}

		if args.Pod == nil || args.Nodes == nil {
			writeJSON(w, []HostPriority{})
			return
		}

		requestedVGPU := getPodResourceRequest(args.Pod, vgpuResourceName)

		var priorities []HostPriority
		for _, node := range args.Nodes.Items {
			score := scoreNode(node, requestedVGPU, policy, numaAware)
			priorities = append(priorities, HostPriority{
				Host:  node.Name,
				Score: score,
			})
		}

		// Normalize scores to 0-maxPriorityScore
		normalizePriorities(priorities)

		logger.Debug("prioritize result",
			zap.String("pod", args.Pod.Name),
			zap.String("policy", policy),
			zap.Int("nodeCount", len(priorities)))

		writeJSON(w, priorities)
	}
}

// scoreNode computes a raw score for a node based on the scheduling policy.
func scoreNode(node corev1.Node, requestedVGPU int64, policy string, numaAware bool) int64 {
	allocatable := getNodeResource(node, vgpuResourceName)
	if allocatable == 0 {
		return 0
	}

	// Base score based on policy
	var score int64
	switch strings.ToLower(policy) {
	case "binpack":
		// Bin-pack: prefer nodes with LESS available resources (pack tightly)
		// Higher utilization = higher score
		if allocatable > 0 {
			usedRatio := float64(allocatable-requestedVGPU) / float64(allocatable)
			// Invert: more packed = higher score
			score = int64((1.0 - usedRatio) * float64(maxPriorityScore))
		}
	case "spread":
		// Spread: prefer nodes with MORE available resources (spread out)
		if allocatable > 0 {
			availableRatio := float64(allocatable) / float64(allocatable+requestedVGPU)
			score = int64(availableRatio * float64(maxPriorityScore))
		}
	default:
		// Default to binpack
		score = int64(float64(maxPriorityScore) * float64(requestedVGPU) / float64(allocatable))
	}

	// NUMA affinity bonus: prefer nodes with the same NUMA node for GPU and CPU
	if numaAware {
		if _, ok := node.Labels["phoenixgpu.io/numa-aligned"]; ok {
			score += 10
		}
	}

	// Clamp to valid range
	if score < 0 {
		score = 0
	}
	if score > maxPriorityScore {
		score = maxPriorityScore
	}

	return score
}

// normalizePriorities normalizes scores to 0-maxPriorityScore.
func normalizePriorities(priorities []HostPriority) {
	if len(priorities) == 0 {
		return
	}

	// Find min and max
	var maxScore, minScore int64
	maxScore = priorities[0].Score
	minScore = priorities[0].Score
	for _, p := range priorities[1:] {
		if p.Score > maxScore {
			maxScore = p.Score
		}
		if p.Score < minScore {
			minScore = p.Score
		}
	}

	scoreRange := maxScore - minScore
	if scoreRange == 0 {
		// All equal — assign middle score
		for i := range priorities {
			priorities[i].Score = maxPriorityScore / 2
		}
		return
	}

	for i := range priorities {
		priorities[i].Score = (priorities[i].Score - minScore) * maxPriorityScore / scoreRange
	}

	// Stable sort to make output deterministic
	sort.SliceStable(priorities, func(i, j int) bool {
		return priorities[i].Score > priorities[j].Score
	})
}

// ── Helpers ───────────────────────────────────────────────────────

// getPodResourceRequest extracts the total resource request for a given resource name.
func getPodResourceRequest(pod *corev1.Pod, resourceName string) int64 {
	resName := corev1.ResourceName(resourceName)
	var total int64
	for _, c := range pod.Spec.Containers {
		if q, ok := c.Resources.Requests[resName]; ok {
			total += q.Value()
		}
	}
	// Also check init containers
	for _, c := range pod.Spec.InitContainers {
		if q, ok := c.Resources.Requests[resName]; ok {
			v := q.Value()
			if v > total {
				total = v // init containers use max, not sum
			}
		}
	}
	return total
}

func decodeBody(r *http.Request, v interface{}) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MiB limit
	defer r.Body.Close()
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeFilterError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(&ExtenderFilterResult{Error: msg}) //nolint:errcheck
}


func run(opts *options) error {
	logger, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck

	logger.Info("starting scheduler-extender",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("policy", opts.schedulingPolicy),
		zap.Bool("numaAware", opts.numaAware))

	mux := http.NewServeMux()
	mux.HandleFunc("/filter", filterHandler(logger))
	mux.HandleFunc("/prioritize", prioritizeHandler(opts.schedulingPolicy, opts.numaAware, logger))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:         opts.addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("scheduler-extender listening", zap.String("addr", opts.addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}
