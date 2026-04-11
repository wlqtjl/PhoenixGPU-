// Node Detail page — detailed per-GPU metrics for a single node
// Copyright 2025 PhoenixGPU Authors
import { useParams, Link } from 'react-router-dom'
import { useNodes, useJobs, type GPUNode } from '../api/client'
import styles from './NodeDetail.module.css'

function tempColor(t: number): string {
  if (t >= 80) return 'var(--red)'
  if (t >= 65) return 'var(--amber)'
  return 'var(--green)'
}

function utilColor(pct: number): string {
  if (pct >= 90) return 'var(--red)'
  if (pct >= 75) return 'var(--amber)'
  return 'var(--green)'
}

function fmtMiB(mib: number): string {
  if (mib >= 1024) return `${(mib / 1024).toFixed(1)} GiB`
  return `${mib} MiB`
}

export default function NodeDetail() {
  const { name } = useParams<{ name: string }>()
  const { data: nodes = [], isLoading } = useNodes()
  const { data: jobs = [] } = useJobs()

  if (isLoading) return <div className={styles.loading}>Loading node details...</div>

  const node = nodes.find(n => n.name === name)
  if (!node) {
    return (
      <div className={styles.notFound}>
        <div className={styles.notFoundTitle}>Node not found</div>
        <div className={styles.notFoundSub}>Node &quot;{name}&quot; does not exist in the cluster</div>
        <Link to="/nodes" className={styles.backLink}>← Back to Nodes</Link>
      </div>
    )
  }

  // Jobs running on this node
  const nodeJobs = jobs.filter(j => j.currentNodeName === name)
  const vramPct = node.vramTotalMiB > 0
    ? Math.round((node.vramUsedMiB / node.vramTotalMiB) * 100)
    : 0

  // Simulate per-GPU data (in real scenario, comes from API)
  const perGPUData = generatePerGPU(node)

  return (
    <div className={styles.page}>
      {/* Breadcrumb */}
      <div className={styles.breadcrumb}>
        <Link to="/nodes" className={styles.breadLink}>GPU Nodes</Link>
        <span className={styles.breadSep}>›</span>
        <span className={styles.breadCurrent}>{node.name}</span>
      </div>

      {/* Node header */}
      <div className={styles.header}>
        <div className={styles.headerLeft}>
          <div className={styles.nodeName}>{node.name}</div>
          <div className={styles.gpuModel}>{node.gpuModel} × {node.gpuCount}</div>
        </div>
        <div className={styles.headerRight}>
          {node.faulted
            ? <span className="pill pillRed">Faulted</span>
            : node.ready
              ? <span className="pill pillGreen">Ready</span>
              : <span className="pill pillAmber">NotReady</span>
          }
        </div>
      </div>

      {/* Overview metrics */}
      <div className={styles.metricsGrid}>
        <MetricBlock label="SM Utilization" value={`${node.smUtilPct}%`} color={utilColor(node.smUtilPct)} />
        <MetricBlock label="VRAM Used" value={fmtMiB(node.vramUsedMiB)} sub={`/ ${fmtMiB(node.vramTotalMiB)} (${vramPct}%)`} color={utilColor(vramPct)} />
        <MetricBlock label="Power Draw" value={`${node.powerWatt} W`} sub="TDP headroom" color="var(--blue)" />
        <MetricBlock label="Temperature" value={`${node.tempCelsius}°C`} color={tempColor(node.tempCelsius)} />
      </div>

      {/* Per-GPU breakdown */}
      <div className={styles.panel}>
        <div className={styles.panelTitle}>Per-GPU Breakdown</div>
        <div className={styles.gpuGrid}>
          {perGPUData.map((gpu, idx) => (
            <div key={idx} className={styles.gpuCard}>
              <div className={styles.gpuIdx}>GPU {idx}</div>
              <div className={styles.gpuBarRow}>
                <span className={styles.gpuBarLabel}>SM</span>
                <div className={styles.gpuBarBg}>
                  <div className={styles.gpuBarFill} style={{ width: `${gpu.smUtil}%`, background: utilColor(gpu.smUtil) }} />
                </div>
                <span className={styles.gpuBarVal}>{gpu.smUtil}%</span>
              </div>
              <div className={styles.gpuBarRow}>
                <span className={styles.gpuBarLabel}>VRAM</span>
                <div className={styles.gpuBarBg}>
                  <div className={styles.gpuBarFill} style={{ width: `${gpu.vramPct}%`, background: utilColor(gpu.vramPct) }} />
                </div>
                <span className={styles.gpuBarVal}>{gpu.vramPct}%</span>
              </div>
              <div className={styles.gpuStats}>
                <span>{gpu.power}W</span>
                <span style={{ color: tempColor(gpu.temp) }}>{gpu.temp}°C</span>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Jobs on this node */}
      <div className={styles.panel}>
        <div className={styles.panelTitle}>
          Jobs on this Node
          <span className={styles.panelBadge}>{nodeJobs.length}</span>
        </div>
        {nodeJobs.length === 0 ? (
          <div className={styles.emptyJobs}>No active jobs on this node</div>
        ) : (
          <div className={styles.jobList}>
            {nodeJobs.map(job => (
              <Link key={`${job.namespace}/${job.name}`}
                to={`/jobs/${job.namespace}/${job.name}`}
                className={styles.jobRow}>
                <span className={styles.jobName}>{job.name}</span>
                <span className={styles.jobNs}>{job.namespace}</span>
                <span className={`pill ${phasePill(job.phase)}`}>{job.phase}</span>
                <span className={styles.jobAlloc}>{Math.round(job.allocRatio * 100)}% alloc</span>
                <span className={styles.jobCkpt}>{job.checkpointCount} ckpts</span>
              </Link>
            ))}
          </div>
        )}
      </div>

      {/* Node info */}
      <div className={styles.panel}>
        <div className={styles.panelTitle}>Node Information</div>
        <div className={styles.infoGrid}>
          <InfoRow label="Node Name" value={node.name} />
          <InfoRow label="GPU Model" value={node.gpuModel} />
          <InfoRow label="GPU Count" value={String(node.gpuCount)} />
          <InfoRow label="Total VRAM" value={fmtMiB(node.vramTotalMiB)} />
          <InfoRow label="Status" value={node.faulted ? 'Faulted' : node.ready ? 'Ready' : 'NotReady'} />
          <InfoRow label="Active Jobs" value={String(nodeJobs.length)} />
        </div>
      </div>
    </div>
  )
}

function MetricBlock({ label, value, sub, color }: {
  label: string; value: string; sub?: string; color: string
}) {
  return (
    <div className={styles.metricBlock}>
      <div className={styles.metricLabel}>{label}</div>
      <div className={styles.metricValue} style={{ color }}>{value}</div>
      {sub && <div className={styles.metricSub}>{sub}</div>}
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

function phasePill(phase: string): string {
  const map: Record<string, string> = {
    Running: 'pillGreen', Checkpointing: 'pillAmber', Restoring: 'pillBlue',
    Succeeded: 'pillGreen', Failed: 'pillRed', Pending: 'pillPurple',
  }
  return map[phase] ?? 'pillAmber'
}

// Generate per-GPU simulated data based on node averages
function generatePerGPU(node: GPUNode) {
  const count = node.gpuCount
  return Array.from({ length: count }, (_, i) => {
    // Add slight variance per GPU
    const variance = (i % 3 === 0 ? 1.1 : i % 3 === 1 ? 0.9 : 1.0)
    return {
      smUtil: Math.min(100, Math.round(node.smUtilPct * variance + (i * 2 - count))),
      vramPct: Math.min(100, Math.round((node.vramUsedMiB / node.vramTotalMiB) * 100 * variance)),
      power: Math.round(node.powerWatt / count * variance),
      temp: Math.round(node.tempCelsius + (i - count / 2) * 1.5),
    }
  })
}
