// Dashboard page — Cluster overview
// Copyright 2025 PhoenixGPU Authors
import { useClusterSummary, useAlerts, useUtilHistory, useBilling } from '../api/client'
import MetricCard from '../components/MetricCard'
import UtilChart  from '../components/UtilChart'
import AlertList  from '../components/AlertList'
import BillingDonut from '../components/BillingDonut'
import styles from './Dashboard.module.css'

export default function Dashboard() {
  const { data: summary, isLoading } = useClusterSummary()
  const { data: alerts = [] }        = useAlerts()
  const { data: history = [] }       = useUtilHistory(24)
  const { data: billing = [] }       = useBilling('monthly')

  if (isLoading) return <div className={styles.loading}>Connecting to cluster API...</div>

  const activeAlerts = alerts.filter(a => !a.resolved)

  return (
    <div className={styles.page}>
      {/* KPI row */}
      <div className={styles.metricGrid}>
        <MetricCard
          label="GPU Hours (month)" accent="amber"
          value={summary?.totalGPUHours.toLocaleString() ?? '—'}
          sub="↑ 12% vs last month"
        />
        <MetricCard
          label="Avg Utilization" accent="green"
          value={`${summary?.avgUtilPct ?? '—'}%`}
          sub="+8% since HA enabled"
        />
        <MetricCard
          label="Active Jobs" accent="blue"
          value={summary?.activeJobs ?? '—'}
          sub={`${summary?.checkpointingJobs ?? 0} checkpointing · ${summary?.restoringJobs ?? 0} restoring`}
        />
        <MetricCard
          label="Active Alerts" accent="red"
          value={activeAlerts.length}
          sub={`${activeAlerts.filter(a=>a.severity==='error').length} critical`}
        />
      </div>

      {/* Charts row */}
      <div className={styles.chartsRow}>
        <div className={styles.panel}>
          <div className={styles.panelTitle}>
            Cluster GPU Utilization
            <span className={styles.panelSub}>— 24h · 30min buckets</span>
          </div>
          <UtilChart data={history} />
        </div>
        <div className={styles.panel}>
          <div className={styles.panelTitle}>Cost Breakdown</div>
          <BillingDonut data={billing} />
        </div>
      </div>

      {/* Alerts */}
      <div className={styles.panel}>
        <div className={styles.panelTitle}>Recent Alerts</div>
        <AlertList alerts={activeAlerts.slice(0, 5)} />
      </div>
    </div>
  )
}
