// Billing Records page — detailed usage records for a department
// Copyright 2025 PhoenixGPU Authors
import { useParams, Link } from 'react-router-dom'
import { useRecords, useBilling } from '../api/client'
import styles from './BillingRecords.module.css'

function fmtDateTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString('zh-CN', {
      month: '2-digit', day: '2-digit',
      hour: '2-digit', minute: '2-digit',
    })
  } catch { return iso }
}

export default function BillingRecords() {
  const { department } = useParams<{ department: string }>()
  const dept = department ? decodeURIComponent(department) : undefined
  const { data: records = [], isLoading } = useRecords(dept)
  const { data: billing = [] } = useBilling('monthly')

  if (isLoading) return <div className={styles.loading}>Loading records...</div>

  const deptInfo = billing.find(b => b.department === dept)
  const totalCost = records.reduce((s, r) => s + r.costCNY, 0)
  const totalHours = records.reduce((s, r) => s + r.gpuHours, 0)

  return (
    <div className={styles.page}>
      {/* Breadcrumb */}
      <div className={styles.breadcrumb}>
        <Link to="/billing" className={styles.breadLink}>Billing Center</Link>
        <span className={styles.breadSep}>›</span>
        <span className={styles.breadCurrent}>{dept ?? 'All Records'}</span>
      </div>

      {/* Department summary */}
      {deptInfo && (
        <div className={styles.summary}>
          <div className={styles.summaryName}>{deptInfo.department}</div>
          <div className={styles.summaryStats}>
            <StatChip label="GPU·h" value={deptInfo.gpuHours.toFixed(0)} />
            <StatChip label="Cost" value={`¥${deptInfo.costCNY.toLocaleString()}`} />
            <StatChip label="Quota" value={`${deptInfo.quotaHours} h`} />
            <StatChip label="Used" value={`${deptInfo.usedPct.toFixed(1)}%`}
              color={deptInfo.usedPct >= 90 ? 'var(--red)' : deptInfo.usedPct >= 75 ? 'var(--amber)' : 'var(--green)'} />
          </div>
        </div>
      )}

      {/* Totals bar */}
      <div className={styles.totalsBar}>
        <span className={styles.totalLabel}>Showing {records.length} records</span>
        <span className={styles.totalValue}>Total: ¥{totalCost.toLocaleString()} · {totalHours.toFixed(1)} GPU·h</span>
      </div>

      {/* Records table */}
      <div className={styles.tableWrap}>
        <table className={styles.table}>
          <thead>
            <tr>
              <th>Job</th>
              <th>Namespace</th>
              <th>Project</th>
              <th>GPU Model</th>
              <th>Node</th>
              <th>Alloc %</th>
              <th>Start</th>
              <th>End</th>
              <th>Duration</th>
              <th>GPU·h</th>
              <th>Cost</th>
            </tr>
          </thead>
          <tbody>
            {records.map((r, idx) => (
              <tr key={idx}>
                <td className={styles.monoCell}>{r.jobName}</td>
                <td className={styles.dimCell}>{r.namespace}</td>
                <td className={styles.dimCell}>{r.project}</td>
                <td className={styles.dimCell}>{r.gpuModel}</td>
                <td className={styles.monoCell}>{r.nodeName}</td>
                <td className={styles.monoCell}>{Math.round(r.allocRatio * 100)}%</td>
                <td className={styles.dimCell}>{fmtDateTime(r.startedAt)}</td>
                <td className={styles.dimCell}>{fmtDateTime(r.endedAt)}</td>
                <td className={styles.monoCell}>{r.durationHours.toFixed(1)}h</td>
                <td className={styles.monoCell}>{r.gpuHours.toFixed(1)}</td>
                <td className={styles.costCell}>¥{r.costCNY.toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {records.length === 0 && (
          <div className={styles.empty}>No usage records found</div>
        )}
      </div>
    </div>
  )
}

function StatChip({ label, value, color }: { label: string; value: string; color?: string }) {
  return (
    <div className={styles.statChip}>
      <span className={styles.chipLabel}>{label}</span>
      <span className={styles.chipValue} style={color ? { color } : undefined}>{value}</span>
    </div>
  )
}
