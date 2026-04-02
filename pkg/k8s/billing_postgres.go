//go:build k8sfull && billingfull
// +build k8sfull,billingfull

package k8s

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/wlqtjl/PhoenixGPU/pkg/billing"
	apitypes "github.com/wlqtjl/PhoenixGPU/pkg/types"
)

// PostgresBillingQuerier adapts billing.PostgresStore to the BillingQuerier interface.
type PostgresBillingQuerier struct {
	store  *billing.PostgresStore
	logger *zap.Logger
}

// NewPostgresBillingQuerier creates a BillingQuerier backed by TimescaleDB.
func NewPostgresBillingQuerier(dsn string, logger *zap.Logger) (*PostgresBillingQuerier, error) {
	store, err := billing.NewPostgresStore(dsn, logger)
	if err != nil {
		return nil, fmt.Errorf("connect billing store: %w", err)
	}
	return &PostgresBillingQuerier{store: store, logger: logger}, nil
}

// Close releases the underlying database connection pool.
func (q *PostgresBillingQuerier) Close() error {
	return q.store.Close()
}

func (q *PostgresBillingQuerier) QueryBillingByDepartment(ctx context.Context, period string) ([]apitypes.DeptBilling, error) {
	totals, err := q.store.SumByDepartment(ctx, period)
	if err != nil {
		return nil, fmt.Errorf("sum by department: %w", err)
	}

	result := make([]apitypes.DeptBilling, 0, len(totals))
	for dept, gpuHours := range totals {
		// Look up quota status for this department
		var quotaHours, usedPct float64
		status, err := q.store.GetQuotaStatus(ctx, dept, period)
		if err == nil && status != nil {
			quotaHours = status.Policy.SoftLimitGPUHours
			usedPct = status.UsedPct
		}

		// Estimate TFlops·h and cost from GPU hours using average GPU spec
		// (accurate per-record computation is done by billing.Engine.Compute)
		tflopsHours := gpuHours * 312 // A100 80GB default
		costCNY := gpuHours * 35      // A100 80GB default rate

		result = append(result, apitypes.DeptBilling{
			Department:  dept,
			GPUHours:    gpuHours,
			TFlopsHours: tflopsHours,
			CostCNY:     costCNY,
			QuotaHours:  quotaHours,
			UsedPct:     usedPct,
		})
	}
	return result, nil
}

func (q *PostgresBillingQuerier) QueryBillingRecords(ctx context.Context, department string) ([]apitypes.BillingRecord, error) {
	filter := billing.RecordFilter{
		Department: department,
	}

	records, err := q.store.ListRecords(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list billing records: %w", err)
	}

	result := make([]apitypes.BillingRecord, 0, len(records))
	for _, r := range records {
		result = append(result, apitypes.BillingRecord{
			Namespace:     r.Namespace,
			PodName:       r.PodName,
			JobName:       r.JobName,
			Department:    r.Department,
			GPUModel:      r.GPUModel,
			AllocRatio:    r.AllocRatio,
			StartedAt:     r.StartedAt,
			EndedAt:       r.EndedAt,
			DurationHours: r.DurationHours,
			TFlopsHours:   r.TFlopsHours,
			CostCNY:       r.CostCNY,
			GPUHours:      r.GPUHours,
		})
	}
	return result, nil
}
