//go:build migrationfull
// +build migrationfull

package internal

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestCRDMigrationStatusStore_SaveAndGet(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "phoenixgpu.io/v1alpha1",
		"kind":       "PhoenixJob",
		"metadata": map[string]interface{}{
			"name":      "job-a",
			"namespace": "research",
		},
	}}

	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClient(scheme, obj)
	gvr := schema.GroupVersionResource{Group: "phoenixgpu.io", Version: "v1alpha1", Resource: "phoenixjobs"}
	store := NewCRDMigrationStatusStoreWithClient(dyn, gvr)

	completed := now.Add(2 * time.Minute)
	err := store.Save(context.Background(), MigrationStatus{
		Job:         "research/job-a",
		Namespace:   "research",
		Name:        "job-a",
		TargetNode:  "gpu-node-02",
		Status:      "done",
		StartedAt:   now,
		UpdatedAt:   completed,
		CompletedAt: &completed,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	got, ok, err := store.Get(context.Background(), "research", "job-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok {
		t.Fatalf("expected migration status to exist")
	}
	if got.Status != "done" {
		t.Fatalf("status mismatch: got=%s", got.Status)
	}
	if got.TargetNode != "gpu-node-02" {
		t.Fatalf("target node mismatch: got=%s", got.TargetNode)
	}
	if got.CompletedAt == nil {
		t.Fatalf("completedAt should be set")
	}

	updatedObj, err := dyn.Resource(gvr).Namespace("research").Get(context.Background(), "job-a", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get updated object: %v", err)
	}
	mig, found, _ := unstructured.NestedMap(updatedObj.Object, "status", "migration")
	if !found {
		t.Fatalf("expected status.migration to be persisted")
	}
	if mig["state"] != "done" {
		t.Fatalf("persisted state mismatch: got=%v", mig["state"])
	}
}

func TestCRDMigrationStatusStore_GetMissingStatus(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "phoenixgpu.io/v1alpha1",
		"kind":       "PhoenixJob",
		"metadata": map[string]interface{}{
			"name":      "job-b",
			"namespace": "research",
		},
	}}

	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), obj)
	gvr := schema.GroupVersionResource{Group: "phoenixgpu.io", Version: "v1alpha1", Resource: "phoenixjobs"}
	store := NewCRDMigrationStatusStoreWithClient(dyn, gvr)

	_, ok, err := store.Get(context.Background(), "research", "job-b")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ok {
		t.Fatalf("expected no migration status")
	}
}
