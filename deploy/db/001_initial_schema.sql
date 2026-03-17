-- PhoenixGPU TimescaleDB Schema
-- Migration: 001_initial_schema.sql
-- Run with: psql $BILLING_DB_DSN -f 001_initial_schema.sql
--
-- Requires TimescaleDB extension (automatically enabled on TimescaleDB)
-- Compatible with standard PostgreSQL (TimescaleDB features are additive)

-- ── Extension ────────────────────────────────────────────────────
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- ── usage_records: core billing time series ──────────────────────
-- This table is the heart of the billing system.
-- Every completed GPU task generates one row.
CREATE TABLE IF NOT EXISTS usage_records (
    id              BIGSERIAL,

    -- Identity
    period_hour     TIMESTAMPTZ NOT NULL,   -- truncated to hour for partitioning
    namespace       TEXT        NOT NULL,
    pod_name        TEXT        NOT NULL,
    job_name        TEXT        NOT NULL,
    department      TEXT        NOT NULL DEFAULT '',
    project         TEXT        NOT NULL DEFAULT '',
    cost_center     TEXT        NOT NULL DEFAULT '',

    -- Resource
    gpu_model       TEXT        NOT NULL,
    gpu_node        TEXT        NOT NULL DEFAULT '',
    alloc_ratio     FLOAT       NOT NULL CHECK (alloc_ratio > 0 AND alloc_ratio <= 1),

    -- Time
    started_at      TIMESTAMPTZ NOT NULL,
    ended_at        TIMESTAMPTZ NOT NULL,
    duration_hours  FLOAT       NOT NULL GENERATED ALWAYS AS
                    (EXTRACT(EPOCH FROM (ended_at - started_at)) / 3600.0) STORED,

    -- Computed billing metrics (set by Go billing engine)
    gpu_hours       FLOAT       NOT NULL DEFAULT 0,
    tflops_hours    FLOAT       NOT NULL DEFAULT 0,
    cost_cny        NUMERIC(12,4) NOT NULL DEFAULT 0,

    -- Audit
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Convert to TimescaleDB hypertable, partitioned by period_hour
-- chunk_time_interval = 1 month matches billing cycles
SELECT create_hypertable(
    'usage_records',
    'period_hour',
    chunk_time_interval => INTERVAL '1 month',
    if_not_exists => TRUE
);

-- Enable TimescaleDB compression (saves ~20× on old data)
ALTER TABLE usage_records SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'department, gpu_model',
    timescaledb.compress_orderby   = 'period_hour DESC'
);

-- Auto-compress chunks older than 7 days
SELECT add_compression_policy('usage_records', INTERVAL '7 days', if_not_exists => TRUE);

-- Auto-drop chunks older than 90 days (data retention policy)
SELECT add_retention_policy('usage_records', INTERVAL '90 days', if_not_exists => TRUE);

-- ── Indexes ───────────────────────────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_usage_dept_period
    ON usage_records (department, period_hour DESC);

CREATE INDEX IF NOT EXISTS idx_usage_namespace_period
    ON usage_records (namespace, period_hour DESC);

CREATE INDEX IF NOT EXISTS idx_usage_job
    ON usage_records (job_name, period_hour DESC);

-- ── quota_policies ────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS quota_policies (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_type     TEXT        NOT NULL CHECK (tenant_type IN ('cluster','dept','project','user')),
    tenant_id       TEXT        NOT NULL,
    soft_limit_gpu_hours FLOAT  NOT NULL DEFAULT 0,
    hard_limit_gpu_hours FLOAT  NOT NULL DEFAULT 0,
    tflops_budget   FLOAT,
    period          TEXT        NOT NULL CHECK (period IN ('daily','weekly','monthly')),
    price_plan      TEXT        NOT NULL DEFAULT 'standard',
    active          BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (tenant_id, period)
);

-- ── billing_alerts ────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS billing_alerts (
    id          BIGSERIAL   PRIMARY KEY,
    tenant_id   TEXT        NOT NULL,
    alert_type  TEXT        NOT NULL,  -- quota_80 | quota_100 | idle | timeout
    severity    TEXT        NOT NULL CHECK (severity IN ('info','warn','error')),
    payload     JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notified_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_billing_alerts_tenant
    ON billing_alerts (tenant_id, created_at DESC)
    WHERE resolved_at IS NULL;

-- ── Continuous aggregates (pre-computed rollups) ──────────────────
-- Daily summary per department — queried by WebUI billing page
CREATE MATERIALIZED VIEW IF NOT EXISTS daily_dept_summary
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', period_hour) AS day,
    department,
    SUM(gpu_hours)    AS total_gpu_hours,
    SUM(tflops_hours) AS total_tflops_hours,
    SUM(cost_cny)     AS total_cost_cny,
    COUNT(DISTINCT pod_name) AS job_count
FROM usage_records
GROUP BY day, department
WITH NO DATA;

-- Refresh policy: update every hour, covering last 7 days
SELECT add_continuous_aggregate_policy('daily_dept_summary',
    start_offset => INTERVAL '7 days',
    end_offset   => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour',
    if_not_exists => TRUE
);

-- ── Utility views ─────────────────────────────────────────────────
-- Current month billing summary per department
CREATE OR REPLACE VIEW current_month_billing AS
SELECT
    department,
    SUM(gpu_hours)    AS gpu_hours,
    SUM(tflops_hours) AS tflops_hours,
    SUM(cost_cny)     AS cost_cny,
    COUNT(*)          AS record_count
FROM usage_records
WHERE period_hour >= date_trunc('month', NOW())
GROUP BY department
ORDER BY cost_cny DESC;

-- Quota utilization view (joins records with policies)
CREATE OR REPLACE VIEW quota_utilization AS
SELECT
    p.tenant_id,
    p.period,
    p.soft_limit_gpu_hours,
    p.hard_limit_gpu_hours,
    COALESCE(u.gpu_hours, 0) AS used_gpu_hours,
    COALESCE(u.gpu_hours, 0) / NULLIF(p.soft_limit_gpu_hours, 0) * 100 AS used_pct
FROM quota_policies p
LEFT JOIN (
    SELECT department, SUM(gpu_hours) AS gpu_hours
    FROM usage_records
    WHERE period_hour >= date_trunc('month', NOW())
    GROUP BY department
) u ON u.department = p.tenant_id
WHERE p.active = TRUE AND p.period = 'monthly';

COMMENT ON TABLE usage_records    IS 'Core billing time series — partitioned by month via TimescaleDB';
COMMENT ON TABLE quota_policies   IS 'Tenant quota configuration — soft/hard limits per period';
COMMENT ON TABLE billing_alerts   IS 'Quota and utilization alert events';
