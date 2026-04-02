//go:build k8sfull
// +build k8sfull

package k8s

import (
	"context"

	apitypes "github.com/wlqtjl/PhoenixGPU/pkg/types"
)

// BillingQuerier is an optional interface for querying billing data
// from a persistent store (e.g. TimescaleDB). When nil on RealK8sClient,
// the client falls back to fake data.
type BillingQuerier interface {
	QueryBillingByDepartment(ctx context.Context, period string) ([]apitypes.DeptBilling, error)
	QueryBillingRecords(ctx context.Context, department string) ([]apitypes.BillingRecord, error)
}

// SetBillingQuerier injects an optional billing data source.
// If not called (or called with nil), billing methods return mock data.
func (c *RealK8sClient) SetBillingQuerier(bq BillingQuerier) {
	c.billingQuerier = bq
}
