// Package internal — K8sClientInterface re-exports and fake implementation.
//
// The interface decouples handlers from real K8s/Prometheus dependencies.
// FakeK8sClient is used in unit tests; RealK8sClient in production.
//
// Domain types and the K8sClientInterface live in pkg/types so that
// both this package and pkg/k8s can share them without violating
// Go's internal-package visibility rules.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package internal

import (
	"fmt"
	"strconv"
	"strings"

	apitypes "github.com/wlqtjl/PhoenixGPU/pkg/types"
)

// Re-export domain types so that the rest of cmd/api-server/internal
// can continue to reference them unqualified.
type (
	ClusterSummary     = apitypes.ClusterSummary
	GPUNode            = apitypes.GPUNode
	PhoenixJob         = apitypes.PhoenixJob
	SnapshotSummary    = apitypes.SnapshotSummary
	DeptBilling        = apitypes.DeptBilling
	BillingRecord      = apitypes.BillingRecord
	Alert              = apitypes.Alert
	TimeSeriesPoint    = apitypes.TimeSeriesPoint
	K8sClientInterface = apitypes.K8sClientInterface
)

// Re-export sentinel error.
var ErrNotFound = apitypes.ErrNotFound

func isNotFound(err error) bool { return apitypes.IsNotFound(err) }

// Re-export FakeK8sClient from pkg/types.
type FakeK8sClient = apitypes.FakeK8sClient

var NewFakeK8sClient = apitypes.NewFakeK8sClient

// ── Utilities ─────────────────────────────────────────────────────

func parsePositiveInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid positive int: %q", s)
	}
	return n, nil
}
