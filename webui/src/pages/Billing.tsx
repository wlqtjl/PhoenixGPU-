// Billing Center page
// Copyright 2025 PhoenixGPU Authors
import { useState } from 'react'
import { useBilling, type DeptBilling } from '../api/client'
import MetricCard from '../components/MetricCard'
import styles from './Billing.module.css'

type Period = 'daily' | 'weekly' | 'monthly'

function quotaColor(pct: number): string {
  if (pct >= 90) return 'var(--red)'
  if (pct >= 75) return 'var(--amber)'
  return 'var(--green)'
}

export default function Billing() {
  const [period, setPeriod] = useState<Period>('monthly')
  const { data: billing = [], isLoading } = useBilling(period)

  const totalCost    = billing.reduce((s, d) => s + d.costCNY, 0)
  const totalHours   = billing.reduce((s, d) => s + d.gpuHours, 0)
  const totalTFlops  = billing.reduce((s, d) => s + d.tflopsHours, 0)

  if (isLoading) return <div>Loading billing data...</div>

  return (
    <div className={styles.page}>
      {/* Period selector */}
      <div className={styles.periodRow}>
        {(['daily','weekly','monthly'] as Period[]).map(p => (
          <button key={p}
            className={`${styles.periodBtn} ${period===p ? styles.periodActive : ''}`}
            onClick={() => setPeriod(p)}>
            {p.charAt(0).toUpperCase()+p.slice(1)}
          </button>
        ))}
      </div>

      {/* Summary KPIs */}
      <div className={styles.metricGrid}>
        <MetricCard label="Total Cost" accent="amber"
          value={`¥${totalCost.toLocaleString()}`}
          sub={`${billing.length} departments`} />
        <MetricCard label="Total TFlops·h" accent="green"
          value={(totalTFlops/1000).toFixed(1)+'K'}
          sub="Standardised across GPU types" />
        <MetricCard label="Total GPU Hours" accent="blue"
          value={Math.round(totalHours).toLocaleString()}
          sub="Physical card · hours" />
      </div>

      {/* Department bars */}
      <div className={styles.panel}>
        <div className={styles.panelTitle}>
          Department Usage &amp; Quota
          <span className={styles.panelSub}>— {period}</span>
        </div>
        <div className={styles.headerRow}>
          <span>Department</span>
          <span>Usage / Quota</span>
          <span>Cost</span>
          <span>%</span>
        </div>
        {billing.map(d => (
          <DeptRow key={d.department} dept={d} />
        ))}
      </div>

      {/* TFlops explanation */}
      <div className={styles.infoBox}>
        <div className={styles.infoTitle}>Why TFlops·h?</div>
        <div className={styles.infoText}>
          GPU % 在不同型号间不可比（A100 1% ≠ RTX 4090 1%）。
          PhoenixGPU 以 <span className={styles.mono}>TFlops·h = AllocRatio × FP16TFlops × DurationHours</span> 为统一货币，
          跨型号完全可比，确保计费公平。
        </div>
      </div>
    </div>
  )
}

function DeptRow({ dept: d }: { dept: DeptBilling }) {
  const pct = Math.round(d.usedPct)
  const clr = quotaColor(pct)
  const pillCls = pct >= 90 ? 'pillRed' : pct >= 75 ? 'pillAmber' : 'pillGreen'
  const status  = pct >= 90 ? '超限风险' : pct >= 75 ? '注意' : '正常'

  return (
    <div className={styles.deptRow}>
      <div className={styles.deptName}>{d.department}</div>
      <div className={styles.deptBar}>
        <div className={styles.barLabel}>
          {d.gpuHours.toFixed(0)} / {d.quotaHours} GPU·h
        </div>
        <div className={styles.barBg}>
          <div className={styles.barFill} style={{ width:`${Math.min(pct,100)}%`, background:clr }} />
        </div>
      </div>
      <div className={styles.deptCost}>¥{d.costCNY.toLocaleString()}</div>
      <div className={styles.deptPct}>
        <span style={{ fontFamily:'var(--mono)', fontSize:12, color:clr }}>{pct}%</span>
        <span className={`pill ${pillCls}`} style={{marginLeft:6,fontSize:10}}>{status}</span>
      </div>
    </div>
  )
}
