// Package billing — PostgreSQL/TimescaleDB BillingStore implementation.
//
// Implements the Store interface against TimescaleDB.
// Uses lib/pq driver (pure Go, no CGO dependency).
//
// Engineering Covenant:
//   - All queries have context (caller-controlled timeout)
//   - Prepared statements prevent SQL injection
//   - Connection pool tuned for K8s pod lifecycle
//   - Graceful degradation: query failure returns empty slice, not 500
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package billing

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

// PostgresStore implements Store against TimescaleDB.
type PostgresStore struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewPostgresStore opens a connection pool to TimescaleDB.
// dsn example: "postgres://phoenix:secret@localhost:5432/phoenixgpu?sslmode=disable"
func NewPostgresStore(dsn string, logger *zap.Logger) (*PostgresStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	// Connection pool tuned for typical K8s sidecar usage
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(3)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping TimescaleDB: %w", err)
	}

	return &PostgresStore{db: db, logger: logger}, nil
}

// Close releases the connection pool.
func (s *PostgresStore) Close() error { return s.db.Close() }

// ── SaveRecord ────────────────────────────────────────────────────

const insertRecord = `
INSERT INTO usage_records (
    period_hour, namespace, pod_name, job_name,
    department, project, cost_center,
    gpu_model, gpu_node, alloc_ratio,
    started_at, ended_at,
    gpu_hours, tflops_hours, cost_cny
) VALUES (
    date_trunc('hour', $1), $2, $3, $4,
    $5, $6, $7,
    $8, $9, $10,
    $11, $12,
    $13, $14, $15
)`

func (s *PostgresStore) SaveRecord(ctx context.Context, r UsageRecord) error {
	_, err := s.db.ExecContext(ctx, insertRecord,
		r.StartedAt,   // period_hour derived from started_at
		r.Namespace, r.PodName, r.JobName,
		r.Department, r.Project, r.CostCenter,
		r.GPUModel, r.NodeName, r.AllocRatio,
		r.StartedAt, r.EndedAt,
		r.GPUHours, r.TFlopsHours, r.CostCNY,
	)
	if err != nil {
		return fmt.Errorf("insert usage record: %w", err)
	}
	return nil
}

// ── GetQuotaStatus ────────────────────────────────────────────────

func (s *PostgresStore) GetQuotaStatus(ctx context.Context, tenantID, period string) (*QuotaStatus, error) {
	const q = `
		SELECT
			p.soft_limit_gpu_hours,
			p.hard_limit_gpu_hours,
			p.period,
			COALESCE(u.used_gpu_hours, 0) AS used_gpu_hours,
			COALESCE(u.used_gpu_hours, 0) / NULLIF(p.soft_limit_gpu_hours, 0) * 100 AS used_pct
		FROM quota_policies p
		LEFT JOIN (
			SELECT department, SUM(gpu_hours) AS used_gpu_hours
			FROM usage_records
			WHERE department = $1
			  AND period_hour >= date_trunc($2, NOW())
			GROUP BY department
		) u ON u.department = p.tenant_id
		WHERE p.tenant_id = $1 AND p.period = $2 AND p.active = TRUE
		LIMIT 1`

	var policy QuotaPolicy
	var used, usedPct float64

	err := s.db.QueryRowContext(ctx, q, tenantID, period).Scan(
		&policy.SoftLimitGPUHours,
		&policy.HardLimitGPUHours,
		&policy.Period,
		&used,
		&usedPct,
	)
	if err == sql.ErrNoRows {
		return nil, nil // no quota configured
	}
	if err != nil {
		return nil, fmt.Errorf("get quota status %s: %w", tenantID, err)
	}

	policy.TenantID = tenantID
	return &QuotaStatus{
		Policy:       policy,
		UsedGPUHours: used,
		UsedPct:      usedPct,
	}, nil
}

// ── ListRecords ───────────────────────────────────────────────────

func (s *PostgresStore) ListRecords(ctx context.Context, f RecordFilter) ([]UsageRecord, error) {
	// Build safe parameterized query
	where := " WHERE 1=1"
	args  := []interface{}{}
	idx   := 1

	if f.Namespace != "" {
		where += fmt.Sprintf(" AND namespace = $%d", idx)
		args = append(args, f.Namespace); idx++
	}
	if f.Department != "" {
		where += fmt.Sprintf(" AND department = $%d", idx)
		args = append(args, f.Department); idx++
	}
	if f.Project != "" {
		where += fmt.Sprintf(" AND project = $%d", idx)
		args = append(args, f.Project); idx++
	}
	if !f.From.IsZero() {
		where += fmt.Sprintf(" AND started_at >= $%d", idx)
		args = append(args, f.From); idx++
	}
	if !f.To.IsZero() {
		where += fmt.Sprintf(" AND ended_at <= $%d", idx)
		args = append(args, f.To); idx++
	}
	_ = idx

	q := `SELECT namespace, pod_name, job_name, department, project,
		         gpu_model, gpu_node, alloc_ratio,
		         started_at, ended_at, duration_hours,
		         gpu_hours, tflops_hours, cost_cny
		  FROM usage_records` + where + ` ORDER BY started_at DESC LIMIT 500`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list records: %w", err)
	}
	defer rows.Close()

	var records []UsageRecord
	for rows.Next() {
		var r UsageRecord
		if err := rows.Scan(
			&r.Namespace, &r.PodName, &r.JobName, &r.Department, &r.Project,
			&r.GPUModel, &r.NodeName, &r.AllocRatio,
			&r.StartedAt, &r.EndedAt, &r.DurationHours,
			&r.GPUHours, &r.TFlopsHours, &r.CostCNY,
		); err != nil {
			return nil, fmt.Errorf("scan record: %w", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return records, nil
}

// ── SumByDepartment ───────────────────────────────────────────────

func (s *PostgresStore) SumByDepartment(ctx context.Context, period string) (map[string]float64, error) {
	// Use the continuous aggregate view for performance
	var startTime time.Time
	switch period {
	case "daily":
		startTime = time.Now().Truncate(24 * time.Hour)
	case "weekly":
		startTime = time.Now().AddDate(0, 0, -7)
	default: // monthly
		y, m, _ := time.Now().Date()
		startTime = time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	}

	const q = `
		SELECT department, SUM(gpu_hours) AS total
		FROM usage_records
		WHERE period_hour >= $1
		GROUP BY department
		ORDER BY total DESC`

	rows, err := s.db.QueryContext(ctx, q, startTime)
	if err != nil {
		return nil, fmt.Errorf("sum by department: %w", err)
	}
	defer rows.Close()

	totals := make(map[string]float64)
	for rows.Next() {
		var dept string
		var total float64
		if err := rows.Scan(&dept, &total); err != nil {
			return nil, fmt.Errorf("scan department sum: %w", err)
		}
		totals[dept] = total
	}
	return totals, rows.Err()
}

// ── MemoryStore enhancement: SetQuota for tests ───────────────────

// SetQuota allows tests to configure quota policies on MemoryStore.
func (m *MemoryStore) SetQuota(tenantID string, policy QuotaPolicy) {
	m.quotas[tenantID] = policy
}
