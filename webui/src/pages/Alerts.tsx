// Alerts page — full alert management with severity filters + resolve
// Copyright 2025 PhoenixGPU Authors
import { useState, useMemo } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useAlerts, resolveAlert, QK } from '../api/client'
import AlertList from '../components/AlertList'
import styles from './Alerts.module.css'

type SevFilter = 'all' | 'error' | 'warn' | 'info'
type StateFilter = 'active' | 'resolved' | 'all'

export default function AlertsPage() {
  const { data: alerts = [], isLoading } = useAlerts()
  const qc = useQueryClient()
  const [sevFilter,   setSevFilter]   = useState<SevFilter>('all')
  const [stateFilter, setStateFilter] = useState<StateFilter>('active')

  const counts = useMemo(() => ({
    error:    alerts.filter(a => a.severity === 'error'   && !a.resolved).length,
    warn:     alerts.filter(a => a.severity === 'warn'    && !a.resolved).length,
    info:     alerts.filter(a => a.severity === 'info'    && !a.resolved).length,
    resolved: alerts.filter(a => a.resolved).length,
    total:    alerts.length,
  }), [alerts])

  const filtered = useMemo(() => {
    let list = alerts
    if (stateFilter === 'active')   list = list.filter(a => !a.resolved)
    if (stateFilter === 'resolved') list = list.filter(a =>  a.resolved)
    if (sevFilter !== 'all')        list = list.filter(a => a.severity === sevFilter)
    // Sort: unresolved first, then by time desc
    return [...list].sort((a, b) => {
      if (a.resolved !== b.resolved) return a.resolved ? 1 : -1
      return new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
    })
  }, [alerts, sevFilter, stateFilter])

  async function handleResolve(id: string) {
    await resolveAlert(id)
    qc.invalidateQueries({ queryKey: QK.alerts })
  }

  if (isLoading) return <div className={styles.loading}>Loading alerts...</div>

  return (
    <div className={styles.page}>
      {/* Summary strip */}
      <div className={styles.summaryStrip}>
        <SummaryChip label="Error" count={counts.error} color="var(--red)"    onClick={() => { setStateFilter('active'); setSevFilter('error') }} />
        <SummaryChip label="Warn"  count={counts.warn}  color="var(--amber)"  onClick={() => { setStateFilter('active'); setSevFilter('warn') }} />
        <SummaryChip label="Info"  count={counts.info}  color="var(--blue)"   onClick={() => { setStateFilter('active'); setSevFilter('info') }} />
        <div className={styles.summaryDiv} />
        <SummaryChip label="Resolved" count={counts.resolved} color="var(--text-muted)" onClick={() => { setStateFilter('resolved'); setSevFilter('all') }} />
      </div>

      {/* Filter bar */}
      <div className={styles.filterBar}>
        {/* State filter */}
        <div className={styles.filterGroup}>
          {(['active','all','resolved'] as StateFilter[]).map(f => (
            <button
              key={f}
              className={`${styles.filterBtn} ${stateFilter===f ? styles.filterActive : ''}`}
              onClick={() => setStateFilter(f)}
            >
              {f.charAt(0).toUpperCase()+f.slice(1)}
            </button>
          ))}
        </div>

        <div className={styles.filterSep} />

        {/* Severity filter */}
        <div className={styles.filterGroup}>
          {(['all','error','warn','info'] as SevFilter[]).map(f => (
            <button
              key={f}
              className={`${styles.filterBtn} ${sevFilter===f ? styles.filterActive : ''} ${f !== 'all' ? styles[`sev-${f}`] : ''}`}
              onClick={() => setSevFilter(f)}
            >
              {f.charAt(0).toUpperCase()+f.slice(1)}
              {f !== 'all' && counts[f] > 0 && (
                <span className={styles.filterCount}>{counts[f]}</span>
              )}
            </button>
          ))}
        </div>

        <div className={styles.resultCount}>
          {filtered.length} alert{filtered.length !== 1 ? 's' : ''}
        </div>
      </div>

      {/* Alert list panel */}
      <div className={styles.panel}>
        <AlertList
          alerts={filtered}
          onResolve={stateFilter !== 'resolved' ? handleResolve : undefined}
        />
      </div>
    </div>
  )
}

function SummaryChip({ label, count, color, onClick }: {
  label: string; count: number; color: string; onClick: () => void
}) {
  return (
    <button className={styles.summaryChip} onClick={onClick}>
      <span className={styles.chipDot} style={{ background: color }} />
      <span className={styles.chipLabel}>{label}</span>
      <span className={styles.chipCount} style={{ color }}>{count}</span>
    </button>
  )
}
