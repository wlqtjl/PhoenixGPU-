// AlertList component
import type { Alert } from '../api/client'
import styles from './AlertList.module.css'

const SEV_COLOR: Record<Alert['severity'], string> = {
  error: 'var(--red)',
  warn:  'var(--amber)',
  info:  'var(--blue)',
}

function fmtTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const m = Math.floor(diff / 60_000)
  if (m < 60) return `${m}m ago`
  return `${Math.floor(m/60)}h ago`
}

interface Props {
  alerts: Alert[]
  onResolve?: (id: string) => void
}

export default function AlertList({ alerts, onResolve }: Props) {
  if (alerts.length === 0) {
    return <div className={styles.empty}>No active alerts</div>
  }
  return (
    <div>
      {alerts.map(a => (
        <div key={a.id} className={styles.item} data-testid="alert-item">
          <div className={styles.dot} style={{ background: SEV_COLOR[a.severity] }} />
          <div className={styles.body}>
            <div className={styles.tenant}>{a.tenant}</div>
            <div className={styles.msg}>{a.message}</div>
          </div>
          <div className={styles.meta}>
            <div className={styles.time}>{fmtTime(a.createdAt)}</div>
            {onResolve && (
              <button className={styles.resolveBtn} onClick={() => onResolve(a.id)}>
                Resolve
              </button>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}
