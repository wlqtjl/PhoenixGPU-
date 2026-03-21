//go:build migrationfull
// +build migrationfull

package internal

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var phoenixJobGVR = schema.GroupVersionResource{
	Group:    "phoenixgpu.io",
	Version:  "v1alpha1",
	Resource: "phoenixjobs",
}

type crdMigrationStatusStore struct {
	dyn dynamic.Interface
	gvr schema.GroupVersionResource
}

func NewCRDMigrationStatusStore() (MigrationStatusStore, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	return NewCRDMigrationStatusStoreWithClient(dyn, phoenixJobGVR), nil
}

func NewCRDMigrationStatusStoreWithClient(dyn dynamic.Interface, gvr schema.GroupVersionResource) MigrationStatusStore {
	return &crdMigrationStatusStore{dyn: dyn, gvr: gvr}
}

func (s *crdMigrationStatusStore) Save(ctx context.Context, status MigrationStatus) error {
	obj, err := s.dyn.Resource(s.gvr).Namespace(status.Namespace).Get(ctx, status.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get phoenixjob %s/%s: %w", status.Namespace, status.Name, err)
	}
	mig := map[string]interface{}{
		"job":        status.Job,
		"targetNode": status.TargetNode,
		"state":      status.Status,
		"startedAt":  status.StartedAt.Format(time.RFC3339Nano),
		"updatedAt":  status.UpdatedAt.Format(time.RFC3339Nano),
	}
	if status.CompletedAt != nil {
		mig["completedAt"] = status.CompletedAt.Format(time.RFC3339Nano)
	}
	if status.Error != "" {
		mig["error"] = status.Error
	}
	if err := unstructured.SetNestedMap(obj.Object, mig, "status", "migration"); err != nil {
		return fmt.Errorf("set migration status: %w", err)
	}
	if _, err := s.dyn.Resource(s.gvr).Namespace(status.Namespace).UpdateStatus(ctx, obj, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update migration status %s/%s: %w", status.Namespace, status.Name, err)
	}
	return nil
}

func (s *crdMigrationStatusStore) Get(ctx context.Context, namespace, name string) (MigrationStatus, bool, error) {
	obj, err := s.dyn.Resource(s.gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return MigrationStatus{}, false, nil
		}
		return MigrationStatus{}, false, fmt.Errorf("get phoenixjob %s/%s: %w", namespace, name, err)
	}
	mig, ok, err := unstructured.NestedMap(obj.Object, "status", "migration")
	if err != nil {
		return MigrationStatus{}, false, fmt.Errorf("read migration status map: %w", err)
	}
	if !ok {
		return MigrationStatus{}, false, nil
	}
	st := MigrationStatus{
		Namespace:  namespace,
		Name:       name,
		Job:        fmt.Sprintf("%s/%s", namespace, name),
		TargetNode: nestedString(mig, "targetNode"),
		Status:     nestedString(mig, "state"),
		Error:      nestedString(mig, "error"),
	}
	if v := nestedString(mig, "job"); v != "" {
		st.Job = v
	}
	if v := nestedString(mig, "startedAt"); v != "" {
		if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
			st.StartedAt = ts
		}
	}
	if v := nestedString(mig, "updatedAt"); v != "" {
		if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
			st.UpdatedAt = ts
		}
	}
	if v := nestedString(mig, "completedAt"); v != "" {
		if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
			st.CompletedAt = &ts
		}
	}
	return st, true, nil
}

func nestedString(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}
