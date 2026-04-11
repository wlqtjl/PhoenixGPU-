// Settings page — cluster configuration and system information
// Copyright 2025 PhoenixGPU Authors
import { useClusterSummary, useNodes } from '../api/client'
import styles from './Settings.module.css'

export default function Settings() {
  const { data: summary } = useClusterSummary()
  const { data: nodes = [] } = useNodes()

  const totalVRAM = nodes.reduce((s, n) => s + n.vramTotalMiB, 0)
  const totalPower = nodes.reduce((s, n) => s + n.powerWatt, 0)
  const gpuModels = [...new Set(nodes.map(n => n.gpuModel))]

  return (
    <div className={styles.page}>
      {/* System overview */}
      <div className={styles.panel}>
        <div className={styles.panelTitle}>System Information</div>
        <div className={styles.infoGrid}>
          <InfoRow label="Software" value="PhoenixGPU" />
          <InfoRow label="Version" value="v0.1.0-dev" />
          <InfoRow label="Build" value="2026.04.10 · go1.22 · linux/amd64" />
          <InfoRow label="API Server" value="http://localhost:8080/api/v1" />
          <InfoRow label="Data Mode" value={import.meta.env.VITE_MOCK === 'true' ? 'Mock (Demo)' : 'Live Cluster'} />
        </div>
      </div>

      {/* Cluster summary */}
      <div className={styles.panel}>
        <div className={styles.panelTitle}>Cluster Overview</div>
        <div className={styles.infoGrid}>
          <InfoRow label="Total Nodes" value={String(nodes.length)} />
          <InfoRow label="Total GPUs" value={String(summary?.totalGPUs ?? '—')} />
          <InfoRow label="Total VRAM" value={`${(totalVRAM / 1024).toFixed(0)} GiB`} />
          <InfoRow label="Total Power" value={`${totalPower} W`} />
          <InfoRow label="GPU Models" value={gpuModels.join(', ') || '—'} />
          <InfoRow label="Active Jobs" value={String(summary?.activeJobs ?? '—')} />
          <InfoRow label="Avg Utilization" value={`${summary?.avgUtilPct ?? '—'}%`} />
        </div>
      </div>

      {/* Feature flags */}
      <div className={styles.panel}>
        <div className={styles.panelTitle}>Features &amp; Components</div>
        <div className={styles.featureGrid}>
          <FeatureCard name="CRIU Checkpoint/Restore" status="enabled" desc="Transparent process checkpoint and live migration" />
          <FeatureCard name="vGPU Multiplexing" status="enabled" desc="Time-sliced virtual GPU sharing via libvgpu" />
          <FeatureCard name="HA Fault Recovery" status="enabled" desc="Automatic fault detection and job rescheduling" />
          <FeatureCard name="Billing Engine" status="enabled" desc="TFlops·h based fair billing across GPU models" />
          <FeatureCard name="Scheduler Extender" status="enabled" desc="K8s scheduler plugin for GPU-aware placement" />
          <FeatureCard name="Admission Webhook" status="enabled" desc="Mutating webhook for pod GPU resource injection" />
          <FeatureCard name="Prometheus Metrics" status="enabled" desc="Full observability via /metrics endpoints" />
          <FeatureCard name="Alert System" status="enabled" desc="Quota alerts, fault alerts, temperature warnings" />
        </div>
      </div>

      {/* API endpoints */}
      <div className={styles.panel}>
        <div className={styles.panelTitle}>API Endpoints</div>
        <div className={styles.endpointList}>
          <Endpoint method="GET" path="/api/v1/cluster/summary" desc="Cluster summary metrics" />
          <Endpoint method="GET" path="/api/v1/nodes" desc="List GPU nodes with metrics" />
          <Endpoint method="GET" path="/api/v1/jobs" desc="List PhoenixJobs (filter by ?namespace=)" />
          <Endpoint method="GET" path="/api/v1/jobs/:ns/:name" desc="Get single job details" />
          <Endpoint method="POST" path="/api/v1/jobs/:ns/:name/checkpoint" desc="Trigger manual checkpoint" />
          <Endpoint method="GET" path="/api/v1/billing/departments" desc="Department billing summary" />
          <Endpoint method="GET" path="/api/v1/billing/records" desc="Usage records (filter by ?department=)" />
          <Endpoint method="GET" path="/api/v1/alerts" desc="List all alerts" />
          <Endpoint method="POST" path="/api/v1/alerts/:id/resolve" desc="Resolve an alert" />
          <Endpoint method="GET" path="/api/v1/cluster/utilization-history" desc="GPU util time series" />
          <Endpoint method="GET" path="/healthz" desc="Health check" />
          <Endpoint method="GET" path="/readyz" desc="Readiness check" />
          <Endpoint method="GET" path="/metrics" desc="Prometheus metrics" />
        </div>
      </div>

      {/* Deployment info */}
      <div className={styles.panel}>
        <div className={styles.panelTitle}>Deployment Configuration</div>
        <div className={styles.infoGrid}>
          <InfoRow label="Helm Chart" value="deploy/helm/phoenixgpu" />
          <InfoRow label="Database" value="PostgreSQL (billing)" />
          <InfoRow label="Migrations" value="deploy/db/migrate.sh" />
          <InfoRow label="Auth" value="Bearer token (--auth-tokens)" />
          <InfoRow label="TLS" value="Optional (--tls-cert, --tls-key)" />
          <InfoRow label="Rate Limit" value="Configurable (--rate-limit-rps)" />
        </div>
      </div>
    </div>
  )
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className={styles.infoRow}>
      <span className={styles.infoLabel}>{label}</span>
      <span className={styles.infoValue}>{value}</span>
    </div>
  )
}

function FeatureCard({ name, status, desc }: { name: string; status: string; desc: string }) {
  return (
    <div className={styles.featureCard}>
      <div className={styles.featureHeader}>
        <span className={styles.featureName}>{name}</span>
        <span className={`pill ${status === 'enabled' ? 'pillGreen' : 'pillRed'}`}>{status}</span>
      </div>
      <div className={styles.featureDesc}>{desc}</div>
    </div>
  )
}

function Endpoint({ method, path, desc }: { method: string; path: string; desc: string }) {
  const methodCls = method === 'GET' ? styles.methodGet : styles.methodPost
  return (
    <div className={styles.endpointRow}>
      <span className={`${styles.method} ${methodCls}`}>{method}</span>
      <span className={styles.endpointPath}>{path}</span>
      <span className={styles.endpointDesc}>{desc}</span>
    </div>
  )
}
