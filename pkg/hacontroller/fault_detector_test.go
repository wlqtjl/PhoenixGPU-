package hacontroller

import (
	"context"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"go.uber.org/zap/zaptest"
)

func makeNode(name string, ready bool) *corev1.Node {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: status,
				},
			},
		},
	}
}

func TestIsNodeReady_True(t *testing.T) {
	node := makeNode("gpu-node-1", true)
	if !isNodeReady(node) {
		t.Error("expected node to be ready")
	}
}

func TestIsNodeReady_False(t *testing.T) {
	node := makeNode("gpu-node-1", false)
	if isNodeReady(node) {
		t.Error("expected node to be not ready")
	}
}

func TestIsNodeReady_NoCondition(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "bare-node"},
		Status:     corev1.NodeStatus{},
	}
	if isNodeReady(node) {
		t.Error("node with no conditions should not be ready")
	}
}

func TestFaultDetector_EmitsFaultAfterThreshold(t *testing.T) {
	logger := zaptest.NewLogger(t)
	var (
		mu     sync.Mutex
		events []FaultEvent
	)

	handler := func(_ context.Context, e FaultEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}

	fd := &FaultDetector{
		logger:            logger,
		handler:           handler,
		PollInterval:      5 * time.Millisecond,
		NotReadyThreshold: 20 * time.Millisecond,
		notReadySince:     make(map[string]time.Time),
		faultEmitted:      make(map[string]bool),
	}

	node := makeNode("gpu-node-1", false)

	// First check — should start tracking but not emit
	fd.checkNode(context.Background(), node)
	mu.Lock()
	count := len(events)
	mu.Unlock()
	if count != 0 {
		t.Fatalf("should not emit fault event immediately, got %d events", count)
	}

	// Wait beyond threshold
	time.Sleep(30 * time.Millisecond)

	// Second check — should emit
	fd.checkNode(context.Background(), node)
	time.Sleep(5 * time.Millisecond) // let goroutine run

	mu.Lock()
	count = len(events)
	mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 fault event after threshold, got %d", count)
	}
	if events[0].NodeName != "gpu-node-1" {
		t.Errorf("fault event has wrong node name: %s", events[0].NodeName)
	}
}

func TestFaultDetector_NoDoubleFaultEmission(t *testing.T) {
	logger := zaptest.NewLogger(t)
	var mu sync.Mutex
	var count int

	handler := func(_ context.Context, _ FaultEvent) {
		mu.Lock()
		defer mu.Unlock()
		count++
	}

	fd := &FaultDetector{
		logger:            logger,
		handler:           handler,
		NotReadyThreshold: 10 * time.Millisecond,
		notReadySince:     make(map[string]time.Time),
		faultEmitted:      make(map[string]bool),
	}

	node := makeNode("gpu-node-1", false)
	fd.checkNode(context.Background(), node)
	time.Sleep(20 * time.Millisecond)

	// Call multiple times — should only emit once
	for i := 0; i < 5; i++ {
		fd.checkNode(context.Background(), node)
	}
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	final := count
	mu.Unlock()
	if final != 1 {
		t.Errorf("expected exactly 1 fault emission, got %d", final)
	}
}

func TestFaultDetector_ResetsOnRecovery(t *testing.T) {
	logger := zaptest.NewLogger(t)
	var mu sync.Mutex
	var count int

	handler := func(_ context.Context, _ FaultEvent) {
		mu.Lock()
		defer mu.Unlock()
		count++
	}

	fd := &FaultDetector{
		logger:            logger,
		handler:           handler,
		NotReadyThreshold: 10 * time.Millisecond,
		notReadySince:     make(map[string]time.Time),
		faultEmitted:      make(map[string]bool),
	}

	// Node goes NotReady, fault emitted
	bad := makeNode("gpu-node-1", false)
	fd.checkNode(context.Background(), bad)
	time.Sleep(20 * time.Millisecond)
	fd.checkNode(context.Background(), bad)
	time.Sleep(10 * time.Millisecond)

	// Node recovers
	good := makeNode("gpu-node-1", true)
	fd.checkNode(context.Background(), good)

	// Node goes NotReady again — should emit second event
	time.Sleep(5 * time.Millisecond)
	fd.checkNode(context.Background(), bad)
	time.Sleep(20 * time.Millisecond)
	fd.checkNode(context.Background(), bad)
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	final := count
	mu.Unlock()
	if final != 2 {
		t.Errorf("expected 2 fault emissions (before and after recovery), got %d", final)
	}
}
