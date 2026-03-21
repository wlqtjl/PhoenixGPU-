//go:build controllerfull
// +build controllerfull

// Package hacontroller implements the PhoenixHA Controller.
// It watches for node failures and triggers Checkpoint/Restore for affected PhoenixJobs.
//
// PhoenixGPU Core — Self-developed
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package hacontroller

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wlqtjl/PhoenixGPU/pkg/checkpoint"
)

// PhoenixJobPhase mirrors the CRD status.phase enum.
type PhoenixJobPhase string

const (
	PhaseRunning       PhoenixJobPhase = "Running"
	PhaseCheckpointing PhoenixJobPhase = "Checkpointing"
	PhaseRestoring     PhoenixJobPhase = "Restoring"
	PhaseSucceeded     PhoenixJobPhase = "Succeeded"
	PhaseFailed        PhoenixJobPhase = "Failed"
)

// FaultEvent is emitted by the FaultDetector when a node failure is detected.
type FaultEvent struct {
	NodeName   string
	DetectedAt time.Time
}

// PhoenixHAController reconciles PhoenixJob objects and handles node failures.
type PhoenixHAController struct {
	client.Client
	Scheme        *runtime.Scheme
	Checkpointer  checkpoint.Checkpointer
	FaultDetector *FaultDetector
	Logger        *zap.Logger

	// Config
	CheckpointInterval    time.Duration
	RestoreTimeoutSeconds int
	MaxRestoreAttempts    int
}

// +kubebuilder:rbac:groups=phoenixgpu.io,resources=phoenixjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=phoenixgpu.io,resources=phoenixjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is the main reconcile loop for PhoenixJob.
// It runs on every PhoenixJob change and handles the checkpoint scheduling.
func (r *PhoenixHAController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger.With(
		zap.String("namespace", req.Namespace),
		zap.String("name", req.Name),
	)

	// Fetch the PhoenixJob — using unstructured until CRD client is generated
	// TODO: replace with typed client after running controller-gen
	job := &unstructuredPhoenixJob{}
	if err := r.Get(ctx, req.NamespacedName, job.Object()); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil // Job deleted, nothing to do
		}
		return ctrl.Result{}, fmt.Errorf("get PhoenixJob: %w", err)
	}

	log.Debug("reconciling PhoenixJob", zap.String("phase", job.Phase()))

	switch PhoenixJobPhase(job.Phase()) {
	case PhaseRunning:
		return r.reconcileRunning(ctx, job, log)
	case PhaseCheckpointing:
		return r.reconcileCheckpointing(ctx, job, log)
	case PhaseRestoring:
		return r.reconcileRestoring(ctx, job, log)
	}

	return ctrl.Result{}, nil
}

// reconcileRunning handles a Running PhoenixJob.
// It schedules periodic checkpoints and watches for node failure.
func (r *PhoenixHAController) reconcileRunning(
	ctx context.Context, job *unstructuredPhoenixJob, log *zap.Logger,
) (ctrl.Result, error) {
	interval := r.CheckpointInterval
	if job.CheckpointIntervalSeconds() > 0 {
		interval = time.Duration(job.CheckpointIntervalSeconds()) * time.Second
	}

	lastCkpt := job.LastCheckpointTime()
	if time.Since(lastCkpt) < interval {
		// Not time yet — requeue at next checkpoint window
		remaining := interval - time.Since(lastCkpt)
		return ctrl.Result{RequeueAfter: remaining}, nil
	}

	log.Info("periodic checkpoint triggered",
		zap.Duration("interval", interval),
		zap.Time("lastCheckpoint", lastCkpt))

	return r.triggerCheckpoint(ctx, job, log)
}

// triggerCheckpoint initiates a Checkpoint for the job's main Pod.
func (r *PhoenixHAController) triggerCheckpoint(
	ctx context.Context, job *unstructuredPhoenixJob, log *zap.Logger,
) (ctrl.Result, error) {
	pod, err := r.getJobPod(ctx, job)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("get job pod: %w", err)
	}

	pid, err := r.getPIDFromPod(ctx, pod)
	if err != nil {
		log.Warn("could not get PID from pod, skipping checkpoint", zap.Error(err))
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	seq := job.CheckpointCount() + 1
	dir := r.snapshotDir(job, seq)

	log.Info("starting checkpoint",
		zap.String("pod", pod.Name),
		zap.Int("pid", pid),
		zap.String("dir", dir))

	if err := r.Checkpointer.Dump(ctx, pid, dir); err != nil {
		log.Error("checkpoint failed", zap.Error(err))
		// Record failure in status but don't fail the job
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	// Update status
	if err := r.updateCheckpointStatus(ctx, job, dir, seq); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("checkpoint succeeded",
		zap.Int("sequence", seq),
		zap.String("dir", dir))

	return ctrl.Result{RequeueAfter: r.CheckpointInterval}, nil
}

// HandleNodeFault is called by FaultDetector when a node goes NotReady.
// It finds all PhoenixJobs on that node and initiates restore.
func (r *PhoenixHAController) HandleNodeFault(ctx context.Context, event FaultEvent) {
	log := r.Logger.With(zap.String("faultedNode", event.NodeName))
	log.Warn("node fault detected, scanning for affected PhoenixJobs")

	// List all Pods on the faulted node
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.MatchingFields{"spec.nodeName": event.NodeName},
	); err != nil {
		log.Error("failed to list pods on faulted node", zap.Error(err))
		return
	}

	affected := 0
	for _, pod := range podList.Items {
		jobName, ok := pod.Labels["phoenixgpu.io/job-name"]
		if !ok {
			continue // not a PhoenixJob pod
		}
		affected++
		log.Info("initiating restore for affected job",
			zap.String("pod", pod.Name),
			zap.String("job", jobName),
			zap.String("namespace", pod.Namespace))

		go r.initiateRestore(context.Background(), pod.Namespace, jobName, log)
	}

	if affected == 0 {
		log.Info("no PhoenixJobs affected by node fault")
	}
}

// initiateRestore downloads the latest snapshot and restores on a healthy node.
func (r *PhoenixHAController) initiateRestore(
	ctx context.Context, namespace, jobName string, log *zap.Logger,
) {
	log = log.With(zap.String("job", jobName), zap.String("namespace", namespace))

	// Fetch job to get last checkpoint dir
	job := &unstructuredPhoenixJob{}
	key := client.ObjectKey{Namespace: namespace, Name: jobName}
	if err := r.Get(ctx, key, job.Object()); err != nil {
		log.Error("failed to fetch PhoenixJob for restore", zap.Error(err))
		return
	}

	lastDir := job.LastCheckpointDir()
	if lastDir == "" {
		log.Warn("no checkpoint available, cannot restore — job will restart from scratch")
		return
	}

	log.Info("restoring from checkpoint", zap.String("snapshotDir", lastDir))

	timeout := time.Duration(r.RestoreTimeoutSeconds) * time.Second
	restoreCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pid, err := r.Checkpointer.Restore(restoreCtx, lastDir)
	if err != nil {
		log.Error("restore failed",
			zap.String("snapshotDir", lastDir),
			zap.Error(err))
		// Increment restore attempt counter — if max reached, fail the job
		return
	}

	log.Info("restore successful", zap.Int("newPID", pid))
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func (r *PhoenixHAController) reconcileCheckpointing(
	ctx context.Context, job *unstructuredPhoenixJob, log *zap.Logger,
) (ctrl.Result, error) {
	log.Debug("job in Checkpointing phase, waiting for completion")
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *PhoenixHAController) reconcileRestoring(
	ctx context.Context, job *unstructuredPhoenixJob, log *zap.Logger,
) (ctrl.Result, error) {
	log.Debug("job in Restoring phase, monitoring progress")
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *PhoenixHAController) getJobPod(
	ctx context.Context, job *unstructuredPhoenixJob,
) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(job.Namespace()),
		client.MatchingLabels{"phoenixgpu.io/job-name": job.Name()},
	); err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pods found for job %s", job.Name())
	}
	return &podList.Items[0], nil
}

func (r *PhoenixHAController) getPIDFromPod(_ context.Context, pod *corev1.Pod) (int, error) {
	// TODO: implement via /proc/<pid> inspection or a sidecar agent
	// For Sprint 1 we use the annotation set by Device Plugin
	pidStr, ok := pod.Annotations["phoenixgpu.io/main-pid"]
	if !ok {
		return 0, fmt.Errorf("pod %s missing phoenixgpu.io/main-pid annotation", pod.Name)
	}
	var pid int
	if _, err := fmt.Sscanf(pidStr, "%d", &pid); err != nil {
		return 0, fmt.Errorf("parse pid annotation %q: %w", pidStr, err)
	}
	return pid, nil
}

func (r *PhoenixHAController) snapshotDir(job *unstructuredPhoenixJob, seq int) string {
	return fmt.Sprintf("/mnt/phoenix-snapshots/%s/%s/ckpt-%05d",
		job.Namespace(), job.Name(), seq)
}

func (r *PhoenixHAController) updateCheckpointStatus(
	ctx context.Context, job *unstructuredPhoenixJob, dir string, seq int,
) error {
	// TODO: implement proper status patch via SSA (Server-Side Apply)
	// Placeholder until typed CRD client is generated
	_ = ctx
	_ = dir
	_ = seq
	return nil
}

// SetupWithManager registers the controller with the controller-runtime manager.
func (r *PhoenixHAController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Watch PhoenixJob objects
		// TODO: For(phoenixv1alpha1.PhoenixJob{}) once types are generated
		Complete(r)
}

// ─── unstructuredPhoenixJob ──────────────────────────────────────────────────
// Thin wrapper until controller-gen produces typed structs.

type unstructuredPhoenixJob struct {
	data map[string]interface{}
}

func (j *unstructuredPhoenixJob) Object() client.Object {
	// placeholder — will be replaced by generated types
	return nil
}

func (j *unstructuredPhoenixJob) Namespace() string {
	return getString(j.data, "metadata", "namespace")
}
func (j *unstructuredPhoenixJob) Name() string  { return getString(j.data, "metadata", "name") }
func (j *unstructuredPhoenixJob) Phase() string { return getString(j.data, "status", "phase") }
func (j *unstructuredPhoenixJob) LastCheckpointDir() string {
	return getString(j.data, "status", "lastCheckpointDir")
}
func (j *unstructuredPhoenixJob) LastCheckpointTime() time.Time {
	ts := getString(j.data, "status", "lastCheckpointTime")
	t, _ := time.Parse(time.RFC3339, ts)
	return t
}
func (j *unstructuredPhoenixJob) CheckpointCount() int {
	v, _ := j.data["status"].(map[string]interface{})["checkpointCount"].(int)
	return v
}
func (j *unstructuredPhoenixJob) CheckpointIntervalSeconds() int {
	spec, _ := j.data["spec"].(map[string]interface{})
	ckpt, _ := spec["checkpoint"].(map[string]interface{})
	v, _ := ckpt["intervalSeconds"].(int)
	return v
}

func getString(m map[string]interface{}, keys ...string) string {
	var cur interface{} = m
	for _, k := range keys {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	s, _ := cur.(string)
	return s
}
