//go:build controllerfull
// +build controllerfull

// Package hacontroller — FaultDetector watches K8s Nodes for NotReady events.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package hacontroller

import (
	"context"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FaultDetector continuously monitors cluster nodes.
// When a node transitions to NotReady for longer than Threshold,
// it emits a FaultEvent to the handler.
type FaultDetector struct {
	client  client.Client
	logger  *zap.Logger
	handler func(context.Context, FaultEvent)

	// PollInterval controls how often node status is checked.
	PollInterval time.Duration
	// NotReadyThreshold is how long a node must be NotReady before it's considered faulted.
	NotReadyThreshold time.Duration

	// Track when each node first went NotReady.
	notReadySince map[string]time.Time
	// Track which nodes we've already emitted a fault event for (avoid duplicate events).
	faultEmitted map[string]bool
}

// NewFaultDetector creates a FaultDetector with sane defaults.
func NewFaultDetector(
	c client.Client,
	logger *zap.Logger,
	handler func(context.Context, FaultEvent),
) *FaultDetector {
	return &FaultDetector{
		client:            c,
		logger:            logger,
		handler:           handler,
		PollInterval:      10 * time.Second,
		NotReadyThreshold: 30 * time.Second,
		notReadySince:     make(map[string]time.Time),
		faultEmitted:      make(map[string]bool),
	}
}

// Run starts the fault detection loop. Blocks until ctx is cancelled.
func (fd *FaultDetector) Run(ctx context.Context) {
	fd.logger.Info("FaultDetector started",
		zap.Duration("pollInterval", fd.PollInterval),
		zap.Duration("notReadyThreshold", fd.NotReadyThreshold))

	ticker := time.NewTicker(fd.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fd.logger.Info("FaultDetector stopped")
			return
		case <-ticker.C:
			fd.poll(ctx)
		}
	}
}

// poll fetches all nodes and checks their Ready condition.
func (fd *FaultDetector) poll(ctx context.Context) {
	nodeList := &corev1.NodeList{}
	if err := fd.client.List(ctx, nodeList); err != nil {
		fd.logger.Warn("failed to list nodes", zap.Error(err))
		return
	}

	activeNodes := make(map[string]bool)
	for _, node := range nodeList.Items {
		activeNodes[node.Name] = true
		fd.checkNode(ctx, &node)
	}

	// Clean up tracking for deleted nodes
	for name := range fd.notReadySince {
		if !activeNodes[name] {
			delete(fd.notReadySince, name)
			delete(fd.faultEmitted, name)
		}
	}
}

// checkNode evaluates a single node's Ready condition.
func (fd *FaultDetector) checkNode(ctx context.Context, node *corev1.Node) {
	ready := isNodeReady(node)

	if ready {
		// Node recovered — reset tracking
		if _, wasFaulted := fd.notReadySince[node.Name]; wasFaulted {
			fd.logger.Info("node recovered",
				zap.String("node", node.Name))
		}
		delete(fd.notReadySince, node.Name)
		delete(fd.faultEmitted, node.Name)
		return
	}

	// Node is NotReady
	if _, tracked := fd.notReadySince[node.Name]; !tracked {
		// First time we see this node as NotReady
		fd.notReadySince[node.Name] = time.Now()
		fd.logger.Warn("node went NotReady",
			zap.String("node", node.Name),
			zap.String("threshold", fd.NotReadyThreshold.String()))
		return
	}

	elapsed := time.Since(fd.notReadySince[node.Name])
	if elapsed < fd.NotReadyThreshold {
		return // Not long enough yet
	}

	if fd.faultEmitted[node.Name] {
		return // Already emitted, don't spam
	}

	// Threshold exceeded — emit fault event
	fd.logger.Error("node fault confirmed: NotReady threshold exceeded",
		zap.String("node", node.Name),
		zap.Duration("notReadyDuration", elapsed))

	fd.faultEmitted[node.Name] = true
	event := FaultEvent{
		NodeName:   node.Name,
		DetectedAt: time.Now(),
	}
	go fd.handler(ctx, event)
}

// isNodeReady returns true if the node has Ready=True condition.
func isNodeReady(node *corev1.Node) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false // No Ready condition = not ready
}
