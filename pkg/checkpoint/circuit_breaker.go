//go:build checkpointfull
// +build checkpointfull

// Circuit breaker for CRIU operations.
//
// Wraps a Checkpointer with a circuit breaker that opens (rejects calls
// immediately) after consecutive failures, preventing cascading damage
// when CRIU or the underlying filesystem is unhealthy.
//
// States:
//   - Closed:    requests pass through normally.
//   - Open:      requests are rejected immediately (fast-fail).
//   - HalfOpen:  one probe request is allowed to check health.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package checkpoint

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // normal operation
	CircuitOpen                         // fast-fail
	CircuitHalfOpen                     // probing
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig configures the circuit breaker behaviour.
type CircuitBreakerConfig struct {
	// MaxFailures is the number of consecutive failures before opening.
	MaxFailures int
	// OpenDuration is how long the circuit stays open before entering half-open.
	OpenDuration time.Duration
}

// DefaultCircuitBreakerConfig returns sensible defaults for CRIU operations.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxFailures:  3,
		OpenDuration: 30 * time.Second,
	}
}

// CircuitBreaker guards an operation against cascading failures.
type CircuitBreaker struct {
	mu           sync.Mutex
	state        CircuitState
	failures     int
	lastFailTime time.Time
	config       CircuitBreakerConfig
	logger       *zap.Logger
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(cfg CircuitBreakerConfig, logger *zap.Logger) *CircuitBreaker {
	if logger == nil {
		logger = zap.NewNop()
	}
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 3
	}
	if cfg.OpenDuration <= 0 {
		cfg.OpenDuration = 30 * time.Second
	}
	return &CircuitBreaker{
		state:  CircuitClosed,
		config: cfg,
		logger: logger,
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.currentState()
}

// currentState returns the effective state, checking if open->half-open transition is due.
// Caller must hold cb.mu.
func (cb *CircuitBreaker) currentState() CircuitState {
	if cb.state == CircuitOpen && time.Since(cb.lastFailTime) >= cb.config.OpenDuration {
		cb.state = CircuitHalfOpen
		cb.logger.Info("circuit breaker entering half-open state")
	}
	return cb.state
}

// Allow checks if a request is allowed through the circuit breaker.
func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	state := cb.currentState()
	switch state {
	case CircuitClosed:
		return nil
	case CircuitHalfOpen:
		return nil // allow one probe
	case CircuitOpen:
		return fmt.Errorf("circuit breaker is open: CRIU operations suspended after %d consecutive failures (retry in %s)",
			cb.config.MaxFailures,
			(cb.config.OpenDuration - time.Since(cb.lastFailTime)).Truncate(time.Second))
	}
	return nil
}

// RecordSuccess records a successful operation, resetting the breaker.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == CircuitHalfOpen {
		cb.logger.Info("circuit breaker closing after successful probe")
	}
	cb.failures = 0
	cb.state = CircuitClosed
}

// RecordFailure records a failed operation.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.lastFailTime = time.Now()
	if cb.failures >= cb.config.MaxFailures {
		cb.state = CircuitOpen
		cb.logger.Error("circuit breaker opened",
			zap.Int("consecutiveFailures", cb.failures),
			zap.Duration("openDuration", cb.config.OpenDuration))
	}
}

// ProtectedCheckpointer wraps a Checkpointer with a circuit breaker.
type ProtectedCheckpointer struct {
	inner   Checkpointer
	breaker *CircuitBreaker
}

// NewProtectedCheckpointer wraps a Checkpointer with circuit breaker protection.
func NewProtectedCheckpointer(inner Checkpointer, cfg CircuitBreakerConfig, logger *zap.Logger) *ProtectedCheckpointer {
	return &ProtectedCheckpointer{
		inner:   inner,
		breaker: NewCircuitBreaker(cfg, logger),
	}
}

func (p *ProtectedCheckpointer) Dump(ctx context.Context, pid int, dir string) error {
	if err := p.breaker.Allow(); err != nil {
		return err
	}
	err := p.inner.Dump(ctx, pid, dir)
	if err != nil {
		p.breaker.RecordFailure()
	} else {
		p.breaker.RecordSuccess()
	}
	return err
}

func (p *ProtectedCheckpointer) PreDump(ctx context.Context, pid int, dir string) error {
	if err := p.breaker.Allow(); err != nil {
		return err
	}
	err := p.inner.PreDump(ctx, pid, dir)
	if err != nil {
		p.breaker.RecordFailure()
	} else {
		p.breaker.RecordSuccess()
	}
	return err
}

func (p *ProtectedCheckpointer) Restore(ctx context.Context, dir string) (int, error) {
	if err := p.breaker.Allow(); err != nil {
		return 0, err
	}
	pid, err := p.inner.Restore(ctx, dir)
	if err != nil {
		p.breaker.RecordFailure()
	} else {
		p.breaker.RecordSuccess()
	}
	return pid, err
}

func (p *ProtectedCheckpointer) Available() error {
	return p.inner.Available()
}

// BreakerState exposes the circuit state for monitoring / tests.
func (p *ProtectedCheckpointer) BreakerState() CircuitState {
	return p.breaker.State()
}
