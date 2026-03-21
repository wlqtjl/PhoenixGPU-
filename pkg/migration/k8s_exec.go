// Package migration — K8s exec implementation and enhanced executor.
//
// Replaces the kubectl-based execOnNode stub with proper K8s client-go
// remotecommand SPDY executor. No kubectl binary dependency.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package migration

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ── ExecCall is the record of a single exec invocation ───────────

// ExecCall records a single kubectl-exec-equivalent call for testing.
type ExecCall struct {
	Stage     string // pre-dump | dump | transfer | restore | unfreeze
	NodeName  string
	Namespace string
	PodName   string
	Command   string
}

// PodExec is the interface for executing commands in pods.
// Implemented by K8sPodExec (production) and fakeExec (tests).
type PodExec interface {
	ExecInPod(ctx context.Context, call ExecCall) error
}

// MigrateRequest is the API-level migration request.
type MigrateRequest struct {
	JobNamespace string `json:"jobNamespace"`
	JobName      string `json:"jobName"`
	TargetNode   string `json:"targetNode"`
}

func (r MigrateRequest) Validate() error {
	if r.JobNamespace == "" {
		return fmt.Errorf("jobNamespace is required")
	}
	if r.JobName == "" {
		return fmt.Errorf("jobName is required")
	}
	if r.TargetNode == "" {
		return fmt.Errorf("targetNode is required")
	}
	return nil
}

// SetDefaults fills optional Plan fields with production defaults.
func (p *Plan) SetDefaults() {
	if p.SnapshotDir == "" {
		p.SnapshotDir = "/tmp/phoenix-migration/" + p.JobName
	}
	if p.TransferMethod == "" {
		p.TransferMethod = "rsync"
	}
	if p.FreezeTimeout == 0 {
		p.FreezeTimeout = 10 * time.Second
	}
}

// ── K8sPodExec — production implementation ───────────────────────

// K8sPodExec executes commands in pods via K8s SPDY exec API.
// No kubectl binary required.
type K8sPodExec struct {
	client *kubernetes.Clientset
	config *rest.Config
	logger *zap.Logger
}

func NewK8sPodExec(client *kubernetes.Clientset, config *rest.Config, logger *zap.Logger) *K8sPodExec {
	return &K8sPodExec{client: client, config: config, logger: logger}
}

func (e *K8sPodExec) ExecInPod(ctx context.Context, call ExecCall) error {
	// Find the pod on the target node
	podName, err := e.findPodOnNode(ctx, call.Namespace, call.NodeName, call.PodName)
	if err != nil {
		return fmt.Errorf("find pod on node %s: %w", call.NodeName, err)
	}

	req := e.client.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(call.Namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: "trainer", // convention: main training container
		Command:   []string{"/bin/sh", "-c", call.Command},
		Stdout:    true,
		Stderr:    true,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(e.config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("create SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		e.logger.Error("exec in pod failed",
			zap.String("stage", call.Stage),
			zap.String("node", call.NodeName),
			zap.String("pod", podName),
			zap.String("stderr", stderr.String()),
			zap.Error(err))
		return fmt.Errorf("exec stage=%s pod=%s: %w\nstderr: %s",
			call.Stage, podName, err, stderr.String())
	}

	e.logger.Debug("exec in pod succeeded",
		zap.String("stage", call.Stage),
		zap.String("pod", podName),
		zap.String("stdout", stdout.String()))
	return nil
}

func (e *K8sPodExec) findPodOnNode(ctx context.Context, namespace, nodeName, podHint string) (string, error) {
	pods, err := e.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName +
			",status.phase=Running",
	})
	if err != nil {
		return "", fmt.Errorf("list pods on %s: %w", nodeName, err)
	}
	for _, p := range pods.Items {
		if jobName, ok := p.Labels["phoenixgpu.io/job-name"]; ok {
			if podHint == "" || jobName == podHint || p.Name == podHint {
				return p.Name, nil
			}
		}
	}
	return "", fmt.Errorf("no PhoenixJob pod found on node %s (hint: %s)", nodeName, podHint)
}

// ── RealExecutorWithExec — injectable executor for tests ─────────

// RealExecutorWithExec is the full executor with injected PodExec.
// Production: NewRealExecutor (uses K8sPodExec)
// Tests:      NewRealExecutorWithExec (uses fakeExec)
type RealExecutorWithExec struct {
	exec   PodExec
	logger *zap.Logger
}

func NewRealExecutorWithExec(exec PodExec, logger *zap.Logger) *RealExecutorWithExec {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &RealExecutorWithExec{exec: exec, logger: logger}
}

func (e *RealExecutorWithExec) Execute(ctx context.Context, plan Plan) (*Result, error) {
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("invalid migration plan: %w", err)
	}
	plan.SetDefaults()

	result := &Result{
		State:          StatePending,
		StageDurations: make(map[State]time.Duration),
	}
	start := time.Now()

	log := e.logger.With(
		zap.String("job", plan.JobNamespace+"/"+plan.JobName),
		zap.String("src", plan.SourceNode),
		zap.String("tgt", plan.TargetNode),
	)

	stages := []struct {
		state State
		fn    func() error
	}{
		{StatePreDumping, func() error { return e.stagePreDump(ctx, plan) }},
		{StateDumping, func() error { return e.stageDump(ctx, plan) }},
		{StateTransferring, func() error { return e.stageTransfer(ctx, plan) }},
		{StateRestoring, func() error { return e.stageRestore(ctx, plan) }},
	}

	var freezeStart time.Time
	for _, s := range stages {
		if s.state == StateDumping {
			freezeStart = time.Now()
		}

		if err := e.runStage(ctx, result, s.state, s.fn, log); err != nil {
			// Critical: if restore failed, unfreeze source
			if s.state == StateRestoring {
				log.Error("restore failed — unfreezing source", zap.Error(err))
				_ = e.exec.ExecInPod(context.Background(), ExecCall{
					Stage: "unfreeze", NodeName: plan.SourceNode,
					Namespace: plan.JobNamespace, PodName: plan.JobName,
					Command: "kill -CONT $(pgrep -f python) 2>/dev/null || true",
				})
			}
			return result, err
		}

		if s.state == StateDumping {
			result.FreezeWindow = time.Since(freezeStart)
			log.Info("freeze window completed", zap.Duration("duration", result.FreezeWindow))
		}
	}

	result.State = StateDone
	result.TotalDuration = time.Since(start)
	log.Info("migration complete",
		zap.Duration("total", result.TotalDuration),
		zap.Duration("freeze", result.FreezeWindow))
	return result, nil
}

func (e *RealExecutorWithExec) runStage(
	ctx context.Context, result *Result,
	next State, fn func() error, log *zap.Logger,
) error {
	if !CanTransition(result.State, next) {
		return fmt.Errorf("invalid transition %s → %s", result.State, next)
	}
	t := time.Now()
	result.State = next
	log.Info("stage start", zap.String("stage", string(next)))
	if err := fn(); err != nil {
		result.State = StateFailed
		result.StageDurations[next] = time.Since(t)
		return fmt.Errorf("stage %s: %w", next, err)
	}
	result.StageDurations[next] = time.Since(t)
	return nil
}

// ── Stage commands ────────────────────────────────────────────────

func (e *RealExecutorWithExec) stagePreDump(ctx context.Context, p Plan) error {
	return e.exec.ExecInPod(ctx, ExecCall{
		Stage: "pre-dump", NodeName: p.SourceNode,
		Namespace: p.JobNamespace, PodName: p.JobName,
		Command: fmt.Sprintf(
			"mkdir -p %s/pre-dump && criu pre-dump --pid $(pgrep -f python | head -1) --dir %s/pre-dump",
			p.SnapshotDir, p.SnapshotDir),
	})
}

func (e *RealExecutorWithExec) stageDump(ctx context.Context, p Plan) error {
	dumpCtx, cancel := context.WithTimeout(ctx, p.FreezeTimeout*3)
	defer cancel()
	return e.exec.ExecInPod(dumpCtx, ExecCall{
		Stage: "dump", NodeName: p.SourceNode,
		Namespace: p.JobNamespace, PodName: p.JobName,
		Command: fmt.Sprintf(
			"criu dump --pid $(pgrep -f python | head -1) --dir %s --prev-images-dir %s/pre-dump --shell-job --leave-stopped",
			p.SnapshotDir, p.SnapshotDir),
	})
}

func (e *RealExecutorWithExec) stageTransfer(ctx context.Context, p Plan) error {
	return e.exec.ExecInPod(ctx, ExecCall{
		Stage: "transfer", NodeName: p.SourceNode,
		Namespace: p.JobNamespace, PodName: p.JobName,
		Command: fmt.Sprintf(
			"rsync -az --delete %s/ %s:%s/ && echo 'transfer complete'",
			p.SnapshotDir, p.TargetNode, p.SnapshotDir),
	})
}

func (e *RealExecutorWithExec) stageRestore(ctx context.Context, p Plan) error {
	return e.exec.ExecInPod(ctx, ExecCall{
		Stage: "restore", NodeName: p.TargetNode,
		Namespace: p.JobNamespace, PodName: p.JobName + "-migrated",
		Command: fmt.Sprintf(
			"criu restore --dir %s --shell-job -d && echo 'restore complete'",
			p.SnapshotDir),
	})
}

// ── URL helper (used in tests) ────────────────────────────────────

func buildExecURL(base *url.URL, namespace, pod string) *url.URL {
	u := *base
	u.Path = fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/exec", namespace, pod)
	return &u
}
