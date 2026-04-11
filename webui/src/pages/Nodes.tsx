// GPU Nodes page — per-node GPU metrics dashboard
// Copyright 2025 PhoenixGPU Authors
import { Link } from 'react-router-dom'
import { useNodes, type GPUNode } from '../api/client'
import styles from './Nodes.module.css'

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
  if (mib >= 1024) return `${(mib / 1024).toFixed(0)} GiB`
  return `${mib} MiB`
}

export default function Nodes() {
  const { data: nodes = [], isLoading } = useNodes()

  if (isLoading) return <div className={styles.loading}>Loading GPU nodes...</div>

  const totalGPUs = nodes.reduce((s, n) => s + n.gpuCount, 0)
  const readyCount = nodes.filter(n => n.ready && !n.faulted).length

  return (
    <div className={styles.page}>
      {/* Summary bar */}
      <div className={styles.summaryBar}>
        <div className={styles.summaryItem}>
          <span className={styles.summaryLabel}>Total GPUs</span>
          <span className={styles.summaryValue}>{totalGPUs}</span>
        </div>
        <div className={styles.summaryItem}>
          <span className={styles.summaryLabel}>Nodes</span>
          <span className={styles.summaryValue}>{nodes.length}</span>
        </div>
        <div className={styles.summaryItem}>
          <span className={styles.summaryLabel}>Healthy</span>
          <span className={styles.summaryValue} style={{ color: 'var(--green)' }}>{readyCount}</span>
        </div>
        {nodes.some(n => n.faulted) && (
          <div className={styles.summaryItem}>
            <span className={styles.summaryLabel}>Faulted</span>
            <span className={styles.summaryValue} style={{ color: 'var(--red)' }}>
              {nodes.filter(n => n.faulted).length}
            </span>
          </div>
        )}
      </div>

      {/* Node cards */}
      <div className={styles.grid}>
        {nodes.map(node => (
          <Link key={node.name} to={`/nodes/${node.name}`} style={{ textDecoration: 'none', color: 'inherit' }}>
            <NodeCard node={node} />
          </Link>
        ))}
      </div>

      {nodes.length === 0 && (
        <div className={styles.empty}>No GPU nodes discovered in cluster</div>
      )}
    </div>
  )
}

function NodeCard({ node: n }: { node: GPUNode }) {
  const vramPct = n.vramTotalMiB > 0
    ? Math.round((n.vramUsedMiB / n.vramTotalMiB) * 100)
    : 0

  return (
    <div className={`${styles.card} ${n.faulted ? styles.cardFaulted : ''}`}>
      {/* Card header */}
      <div className={styles.cardHeader}>
        <div className={styles.nodeName}>{n.name}</div>
        <div className={styles.nodeStatus}>
          {n.faulted
            ? <span className="pill pillRed">Faulted</span>
            : n.ready
              ? <span className="pill pillGreen">Ready</span>
              : <span className="pill pillAmber">NotReady</span>
          }
        </div>
      </div>

      {/* GPU model */}
      <div className={styles.gpuModel}>{n.gpuModel}</div>
      <div className={styles.gpuCount}>{n.gpuCount} GPU{n.gpuCount !== 1 ? 's' : ''}</div>

      {/* Metrics */}
      <div className={styles.metrics}>
        {/* VRAM */}
        <div className={styles.metricRow}>
          <span className={styles.metricLabel}>VRAM</span>
          <div className={styles.barWrap}>
            <div
              className={styles.barFill}
              style={{
                width: `${Math.min(vramPct, 100)}%`,
                background: utilColor(vramPct),
              }}
            />
          </div>
          <span className={styles.metricVal}>
            {fmtMiB(n.vramUsedMiB)} / {fmtMiB(n.vramTotalMiB)}
          </span>
        </div>

        {/* SM Utilisation */}
        <div className={styles.metricRow}>
          <span className={styles.metricLabel}>SM Util</span>
          <div className={styles.barWrap}>
            <div
              className={styles.barFill}
              style={{
                width: `${Math.min(n.smUtilPct, 100)}%`,
                background: utilColor(n.smUtilPct),
              }}
            />
          </div>
          <span className={styles.metricVal}>{n.smUtilPct.toFixed(0)}%</span>
        </div>
      </div>

      {/* Stats row */}
      <div className={styles.statsRow}>
        <div className={styles.statItem}>
          <span className={styles.statLabel}>Power</span>
          <span className={styles.statVal}>{n.powerWatt.toFixed(0)} W</span>
        </div>
        <div className={styles.statItem}>
          <span className={styles.statLabel}>Temp</span>
          <span className={styles.statVal} style={{ color: tempColor(n.tempCelsius) }}>
            {n.tempCelsius.toFixed(0)}°C
          </span>
        </div>
        <div className={styles.statItem}>
          <span className={styles.statLabel}>VRAM %</span>
          <span className={styles.statVal}>{vramPct}%</span>
        </div>
      </div>
    </div>
  )
}
