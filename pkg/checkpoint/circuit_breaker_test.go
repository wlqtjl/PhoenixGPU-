//go:build checkpointfull
// +build checkpointfull

package checkpoint

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ── fakeCheckpointer for circuit breaker tests ────────────────────

type fakeCheckpointer struct {
	dumpErr    error
	restoreErr error
	dumpCalls  int
	restoreCalls int
}

func (f *fakeCheckpointer) Dump(_ context.Context, _ int, _ string) error {
	f.dumpCalls++
	return f.dumpErr
}

func (f *fakeCheckpointer) PreDump(_ context.Context, _ int, _ string) error {
	return nil
}

func (f *fakeCheckpointer) Restore(_ context.Context, _ string) (int, error) {
	f.restoreCalls++
	return 42, f.restoreErr
}

func (f *fakeCheckpointer) Available() error {
	return nil
}

// ── Tests ─────────────────────────────────────────────────────────

func TestCircuitBreaker_ClosedState(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig(), nil)
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed, got %s", cb.State())
	}
	if err := cb.Allow(); err != nil {
		t.Fatalf("expected allow in closed state, got %v", err)
	}
}

func TestCircuitBreaker_OpensAfterMaxFailures(t *testing.T) {
	cfg := CircuitBreakerConfig{MaxFailures: 3, OpenDuration: 100 * time.Millisecond}
	cb := NewCircuitBreaker(cfg, nil)

	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.State() != CircuitOpen {
		t.Fatalf("expected open after 3 failures, got %s", cb.State())
	}
	if err := cb.Allow(); err == nil {
		t.Fatal("expected error when circuit is open")
	}
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	cfg := CircuitBreakerConfig{MaxFailures: 2, OpenDuration: 50 * time.Millisecond}
	cb := NewCircuitBreaker(cfg, nil)

	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Fatalf("expected open, got %s", cb.State())
	}

	time.Sleep(60 * time.Millisecond)

	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open after timeout, got %s", cb.State())
	}
	if err := cb.Allow(); err != nil {
		t.Fatalf("expected allow in half-open state, got %v", err)
	}
}

func TestCircuitBreaker_ClosesAfterSuccessInHalfOpen(t *testing.T) {
	cfg := CircuitBreakerConfig{MaxFailures: 1, OpenDuration: 10 * time.Millisecond}
	cb := NewCircuitBreaker(cfg, nil)

	cb.RecordFailure()
	time.Sleep(15 * time.Millisecond)

	// Should be half-open now
	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open, got %s", cb.State())
	}

	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed after success, got %s", cb.State())
	}
}

func TestCircuitBreaker_ReopensOnFailureInHalfOpen(t *testing.T) {
	cfg := CircuitBreakerConfig{MaxFailures: 1, OpenDuration: 10 * time.Millisecond}
	cb := NewCircuitBreaker(cfg, nil)

	cb.RecordFailure()
	time.Sleep(15 * time.Millisecond)

	// Half-open: record another failure
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("expected re-opened, got %s", cb.State())
	}
}

func TestProtectedCheckpointer_PassesThrough(t *testing.T) {
	inner := &fakeCheckpointer{}
	cfg := DefaultCircuitBreakerConfig()
	p := NewProtectedCheckpointer(inner, cfg, nil)

	if err := p.Dump(context.Background(), 1, "/tmp/test"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inner.dumpCalls != 1 {
		t.Fatalf("expected 1 dump call, got %d", inner.dumpCalls)
	}
	if p.BreakerState() != CircuitClosed {
		t.Fatalf("expected closed, got %s", p.BreakerState())
	}
}

func TestProtectedCheckpointer_OpensOnRepeatedFailures(t *testing.T) {
	inner := &fakeCheckpointer{dumpErr: fmt.Errorf("criu crashed")}
	cfg := CircuitBreakerConfig{MaxFailures: 2, OpenDuration: 100 * time.Millisecond}
	p := NewProtectedCheckpointer(inner, cfg, nil)

	// Two failures open the circuit
	_ = p.Dump(context.Background(), 1, "/tmp/test")
	_ = p.Dump(context.Background(), 1, "/tmp/test")
	if inner.dumpCalls != 2 {
		t.Fatalf("expected 2 dump calls, got %d", inner.dumpCalls)
	}

	// Third call should be rejected without reaching inner
	err := p.Dump(context.Background(), 1, "/tmp/test")
	if err == nil {
		t.Fatal("expected circuit breaker error")
	}
	if inner.dumpCalls != 2 {
		t.Fatalf("expected inner not called when circuit is open, got %d calls", inner.dumpCalls)
	}
}

func TestProtectedCheckpointer_RestoreWithCircuitBreaker(t *testing.T) {
	inner := &fakeCheckpointer{restoreErr: fmt.Errorf("restore failed")}
	cfg := CircuitBreakerConfig{MaxFailures: 2, OpenDuration: 50 * time.Millisecond}
	p := NewProtectedCheckpointer(inner, cfg, nil)

	_, _ = p.Restore(context.Background(), "/tmp/test")
	_, _ = p.Restore(context.Background(), "/tmp/test")

	// Circuit should be open
	_, err := p.Restore(context.Background(), "/tmp/test")
	if err == nil {
		t.Fatal("expected circuit breaker to reject")
	}

	// Wait for half-open
	time.Sleep(60 * time.Millisecond)

	// Fix the inner, verify recovery
	inner.restoreErr = nil
	pid, err := p.Restore(context.Background(), "/tmp/test")
	if err != nil {
		t.Fatalf("expected success in half-open probe, got %v", err)
	}
	if pid != 42 {
		t.Fatalf("expected pid 42, got %d", pid)
	}
	if p.BreakerState() != CircuitClosed {
		t.Fatalf("expected closed after successful probe, got %s", p.BreakerState())
	}
}

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state CircuitState
		want  string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}
