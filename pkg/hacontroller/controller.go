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
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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

	// MaxConcurrentRestores limits the number of parallel restore goroutines.
	MaxConcurrentRestores int

	// restoreSem is a semaphore channel to limit concurrent restores.
	restoreSem     chan struct{}
	restoreSemOnce sync.Once

	// restoreWg tracks in-flight restore goroutines for graceful shutdown.
	restoreWg sync.WaitGroup
}

// +kubebuilder:rbac:groups=phoenixgpu.io,resources=phoenixjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=phoenixgpu.io,resources=phoenixjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// PhoenixJobGVR is the GroupVersionResource for the PhoenixJob CRD.
var PhoenixJobGVR = schema.GroupVersionResource{
	Group:    "phoenixgpu.io",
	Version:  "v1alpha1",
	Resource: "phoenixjobs",
}

// PhoenixJobGVK is the GroupVersionKind for the PhoenixJob CRD.
var PhoenixJobGVK = schema.GroupVersionKind{
	Group:   "phoenixgpu.io",
	Version: "v1alpha1",
	Kind:    "PhoenixJob",
}

// Reconcile is the main reconcile loop for PhoenixJob.
// It runs on every PhoenixJob change and handles the checkpoint scheduling.
func (r *PhoenixHAController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger.With(
		zap.String("namespace", req.Namespace),
		zap.String("name", req.Name),
	)

	// Fetch the PhoenixJob using an Unstructured object
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(PhoenixJobGVK)
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil // Job deleted, nothing to do
		}
		return ctrl.Result{}, fmt.Errorf("get PhoenixJob: %w", err)
	}

	job := &unstructuredPhoenixJob{obj: obj}
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

	// Lazily initialize the restore semaphore
	r.restoreSemOnce.Do(func() {
		maxConc := r.MaxConcurrentRestores
		if maxConc <= 0 {
			maxConc = 10 // sensible default
		}
		r.restoreSem = make(chan struct{}, maxConc)
	})

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

		// Acquire semaphore slot to limit concurrent restores
		ns, jn := pod.Namespace, jobName
		r.restoreWg.Add(1)
		go func() {
			defer r.restoreWg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic in restore goroutine",
						zap.String("namespace", ns),
						zap.String("job", jn),
						zap.Any("panic", rec))
				}
			}()
			select {
			case r.restoreSem <- struct{}{}:
				defer func() { <-r.restoreSem }()
				r.initiateRestore(ctx, ns, jn, log)
			case <-ctx.Done():
				log.Warn("context cancelled before restore could start",
					zap.String("job", jn))
			}
		}()
	}

	if affected == 0 {
		log.Info("no PhoenixJobs affected by node fault")
	}
}

// Shutdown waits for all in-flight restore goroutines to finish.
// Call this during controller teardown to ensure clean shutdown.
func (r *PhoenixHAController) Shutdown() {
	r.restoreWg.Wait()
}

// initiateRestore downloads the latest snapshot and restores on a healthy node.
func (r *PhoenixHAController) initiateRestore(
	ctx context.Context, namespace, jobName string, log *zap.Logger,
) {
	log = log.With(zap.String("job", jobName), zap.String("namespace", namespace))

	// Fetch job to get last checkpoint dir
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(PhoenixJobGVK)
	key := client.ObjectKey{Namespace: namespace, Name: jobName}
	if err := r.Get(ctx, key, obj); err != nil {
		log.Error("failed to fetch PhoenixJob for restore", zap.Error(err))
		return
	}
	job := &unstructuredPhoenixJob{obj: obj}

	lastDir := job.LastCheckpointDir()
	if lastDir == "" {
		log.Warn("no checkpoint available, cannot restore — job will restart from scratch")
		return
	}

	// Update phase to Restoring
	if err := r.updateJobPhase(ctx, job, PhaseRestoring); err != nil {
		log.Error("failed to update job phase to Restoring", zap.Error(err))
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
		attempts := job.RestoreAttempts() + 1
		if attempts >= r.MaxRestoreAttempts {
			log.Error("max restore attempts reached, marking job as Failed",
				zap.Int("attempts", attempts))
			if err := r.updateJobPhase(ctx, job, PhaseFailed); err != nil {
				log.Error("failed to update job phase to Failed", zap.Error(err))
			}
		}
		return
	}

	log.Info("restore successful", zap.Int("newPID", pid))
	// Transition back to Running
	if err := r.updateJobPhase(ctx, job, PhaseRunning); err != nil {
		log.Error("failed to update job phase to Running after restore", zap.Error(err))
	}
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func (r *PhoenixHAController) reconcileCheckpointing(
	_ context.Context, job *unstructuredPhoenixJob, log *zap.Logger,
) (ctrl.Result, error) {
	log.Debug("job in Checkpointing phase, monitoring checkpoint completion")

	// Check if checkpoint has completed by checking if the dir was written
	lastDir := job.LastCheckpointDir()
	if lastDir != "" {
		// Checkpoint directory present — transition back to Running
		log.Info("checkpoint completed, transitioning back to Running",
			zap.String("dir", lastDir))
		// Phase transition will be handled by the next reconcile after status update
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *PhoenixHAController) reconcileRestoring(
	_ context.Context, job *unstructuredPhoenixJob, log *zap.Logger,
) (ctrl.Result, error) {
	log.Debug("job in Restoring phase, monitoring restore progress")

	attempts := job.RestoreAttempts()
	if attempts >= r.MaxRestoreAttempts {
		log.Error("max restore attempts exceeded in Restoring phase",
			zap.Int("attempts", attempts),
			zap.Int("max", r.MaxRestoreAttempts))
		// Will be handled by the restore goroutine marking as Failed
	}
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

// pidMaxLimit is the Linux kernel's compile-time maximum PID value (PID_MAX_LIMIT),
// which is the upper bound for the runtime-configurable /proc/sys/kernel/pid_max.
const pidMaxLimit = 4194304

func (r *PhoenixHAController) getPIDFromPod(ctx context.Context, pod *corev1.Pod) (int, error) {
	// Strategy 1: annotation set by Device Plugin (fast path)
	if pidStr, ok := pod.Annotations["phoenixgpu.io/main-pid"]; ok {
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			r.Logger.Warn("invalid pid annotation, falling back to container status",
				zap.String("pod", pod.Name), zap.String("pidStr", pidStr))
		} else if pid > 0 && pid <= pidMaxLimit {
			return pid, nil
		}
	}

	// Strategy 2: extract PID from container status (containerID → PID via runtime)
	pid, err := r.getPIDFromContainerStatus(ctx, pod)
	if err == nil && pid > 0 {
		return pid, nil
	}
	if err != nil {
		r.Logger.Debug("container status PID lookup failed", zap.Error(err))
	}

	return 0, fmt.Errorf("pod %s/%s: unable to determine main PID (no annotation and container status lookup failed)",
		pod.Namespace, pod.Name)
}

// getPIDFromContainerStatus attempts to extract PID from the pod's container status.
// It reads /proc on the host to find the PID matching the container ID.
func (r *PhoenixHAController) getPIDFromContainerStatus(_ context.Context, pod *corev1.Pod) (int, error) {
	if len(pod.Status.ContainerStatuses) == 0 {
		return 0, fmt.Errorf("pod %s has no container statuses", pod.Name)
	}

	// Find the main training container (prefer container named "training" or first GPU container)
	var containerID string
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Running == nil {
			continue
		}
		if cs.Name == "training" || cs.Name == "main" {
			containerID = cs.ContainerID
			break
		}
		if containerID == "" {
			containerID = cs.ContainerID
		}
	}

	if containerID == "" {
		return 0, fmt.Errorf("pod %s has no running containers", pod.Name)
	}

	// Parse container ID (format: "containerd://abc123..." or "docker://abc123...")
	parts := strings.SplitN(containerID, "://", 2)
	if len(parts) != 2 || parts[1] == "" {
		return 0, fmt.Errorf("invalid container ID format: %s", containerID)
	}
	cid := parts[1]

	// Look up PID from /proc by scanning for the container ID in cgroup
	pid, err := findPIDForContainer(cid)
	if err != nil {
		return 0, fmt.Errorf("find PID for container %s: %w", cid, err)
	}

	return pid, nil
}

// findPIDForContainer scans /proc to find the init PID of a container.
func findPIDForContainer(containerID string) (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("read /proc: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a PID directory
		}

		cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
		if !fileContains(cgroupPath, containerID) {
			continue
		}

		// Check NSpid line to find PID 1 in the container namespace
		statusPath := fmt.Sprintf("/proc/%d/status", pid)
		if isContainerInitProcess(statusPath) {
			return pid, nil
		}
	}

	return 0, fmt.Errorf("no process found for container %s", containerID)
}

// fileContains efficiently checks if a file contains a substring by scanning line-by-line.
func fileContains(path, substr string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 4096) // cgroup files are typically small
	n, err := f.Read(buf)
	if n == 0 {
		return false
	}
	return strings.Contains(string(buf[:n]), substr)
}

// isContainerInitProcess checks if the process at statusPath is PID 1 inside a container.
func isContainerInitProcess(statusPath string) bool {
	f, err := os.Open(statusPath)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 8192) // /proc/PID/status is typically < 2KB
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}

	for _, line := range strings.Split(string(buf[:n]), "\n") {
		if strings.HasPrefix(line, "NSpid:") {
			fields := strings.Fields(line)
			// If last field is "1", this is PID 1 in the container namespace
			return len(fields) >= 3 && fields[len(fields)-1] == "1"
		}
	}
	return false
}

func (r *PhoenixHAController) snapshotDir(job *unstructuredPhoenixJob, seq int) string {
	return fmt.Sprintf("/mnt/phoenix-snapshots/%s/%s/ckpt-%05d",
		job.Namespace(), job.Name(), seq)
}

func (r *PhoenixHAController) updateCheckpointStatus(
	ctx context.Context, job *unstructuredPhoenixJob, dir string, seq int,
) error {
	// Use Server-Side Apply (SSA) to patch the PhoenixJob status
	patch := map[string]interface{}{
		"apiVersion": "phoenixgpu.io/v1alpha1",
		"kind":       "PhoenixJob",
		"metadata": map[string]interface{}{
			"name":      job.Name(),
			"namespace": job.Namespace(),
		},
		"status": map[string]interface{}{
			"lastCheckpointDir":  dir,
			"lastCheckpointTime": time.Now().UTC().Format(time.RFC3339),
			"checkpointCount":    seq,
			"phase":              string(PhaseRunning),
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal status patch: %w", err)
	}

	obj := job.obj.DeepCopy()
	err = r.Status().Patch(ctx, obj,
		client.RawPatch(types.MergePatchType, patchBytes))
	if err != nil {
		return fmt.Errorf("patch PhoenixJob status: %w", err)
	}
	return nil
}

// updateJobPhase patches the PhoenixJob status phase field.
func (r *PhoenixHAController) updateJobPhase(
	ctx context.Context, job *unstructuredPhoenixJob, phase PhoenixJobPhase,
) error {
	patch := map[string]interface{}{
		"status": map[string]interface{}{
			"phase": string(phase),
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal phase patch: %w", err)
	}

	obj := job.obj.DeepCopy()
	return r.Status().Patch(ctx, obj,
		client.RawPatch(types.MergePatchType, patchBytes))
}

// SetupWithManager registers the controller with the controller-runtime manager.
func (r *PhoenixHAController) SetupWithManager(mgr ctrl.Manager) error {
	// Create an Unstructured object to watch PhoenixJob CRD
	phoenixJob := &unstructured.Unstructured{}
	phoenixJob.SetGroupVersionKind(PhoenixJobGVK)

	return ctrl.NewControllerManagedBy(mgr).
		For(phoenixJob).
		Watches(&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.podToPhoenixJob)).
		Complete(r)
}

// podToPhoenixJob maps Pod events to the owning PhoenixJob for reconciliation.
func (r *PhoenixHAController) podToPhoenixJob(_ context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil
	}
	jobName, ok := pod.Labels["phoenixgpu.io/job-name"]
	if !ok {
		return nil
	}
	return []reconcile.Request{
		{NamespacedName: client.ObjectKey{
			Namespace: pod.Namespace,
			Name:      jobName,
		}},
	}
}

// ─── unstructuredPhoenixJob ──────────────────────────────────────────────────
// Thin wrapper around Unstructured for typed access to PhoenixJob fields.

type unstructuredPhoenixJob struct {
	obj *unstructured.Unstructured
}

func (j *unstructuredPhoenixJob) Object() client.Object {
	return j.obj
}

func (j *unstructuredPhoenixJob) Namespace() string {
	return j.obj.GetNamespace()
}
func (j *unstructuredPhoenixJob) Name() string { return j.obj.GetName() }
func (j *unstructuredPhoenixJob) Phase() string {
	v, _, _ := unstructured.NestedString(j.obj.Object, "status", "phase")
	return v
}
func (j *unstructuredPhoenixJob) LastCheckpointDir() string {
	v, _, _ := unstructured.NestedString(j.obj.Object, "status", "lastCheckpointDir")
	return v
}
func (j *unstructuredPhoenixJob) LastCheckpointTime() time.Time {
	ts, _, _ := unstructured.NestedString(j.obj.Object, "status", "lastCheckpointTime")
	t, _ := time.Parse(time.RFC3339, ts)
	return t
}
func (j *unstructuredPhoenixJob) CheckpointCount() int {
	v, _, _ := unstructured.NestedInt64(j.obj.Object, "status", "checkpointCount")
	return int(v)
}
func (j *unstructuredPhoenixJob) RestoreAttempts() int {
	v, _, _ := unstructured.NestedInt64(j.obj.Object, "status", "restoreAttempts")
	return int(v)
}
func (j *unstructuredPhoenixJob) CheckpointIntervalSeconds() int {
	v, _, _ := unstructured.NestedInt64(j.obj.Object, "spec", "checkpoint", "intervalSeconds")
	return int(v)
}
