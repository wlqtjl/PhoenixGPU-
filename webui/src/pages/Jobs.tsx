// Jobs page — PhoenixJob list + checkpoint visualization
// Copyright 2025 PhoenixGPU Authors
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useJobs, useTriggerCheckpoint, type PhoenixJob, type Phase } from '../api/client'
import styles from './Jobs.module.css'

const PHASE_COLORS: Record<Phase, string> = {
  Running:       'pillGreen',
  Checkpointing: 'pillAmber',
  Restoring:     'pillBlue',
  Succeeded:     'pillGreen',
  Failed:        'pillRed',
  Pending:       'pillPurple',
}

function fmtBytes(b: number): string {
  if (b >= 1e9) return `${(b/1e9).toFixed(1)}GB`
  if (b >= 1e6) return `${(b/1e6).toFixed(0)}MB`
  return `${b}B`
}

function fmtRelTime(iso: string | null): string {
  if (!iso) return '—'
  const diff = Date.now() - new Date(iso).getTime()
  const m = Math.floor(diff / 60_000)
  if (m < 60)   return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24)   return `${h}h ago`
  return `${Math.floor(h/24)}d ago`
}

export default function Jobs() {
  const { data: jobs = [], isLoading } = useJobs()
  const trigger = useTriggerCheckpoint()
  const [selected, setSelected] = useState<PhoenixJob | null>(null)

  if (isLoading) return <div className={styles.loading}>Loading jobs...</div>

  return (
    <div className={styles.page}>
      <div className={styles.tableWrap}>
        <div className={styles.tableHeader}>
          <span>PhoenixJobs — All Namespaces</span>
          <span className="pill pillAmber">{jobs.filter(j=>j.phase==='Running').length} running</span>
        </div>

        <table className={styles.table}>
          <thead>
            <tr>
              <th>Job Name</th>
              <th>Namespace</th>
              <th>Phase</th>
              <th>GPU / Alloc</th>
              <th>Checkpoints</th>
              <th>Last Checkpoint</th>
              <th>Node</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {jobs.map(job => (
              <tr key={`${job.namespace}/${job.name}`}
                  onClick={() => setSelected(job)}
                  className={selected?.name === job.name ? styles.rowSelected : ''}>
                <td className={styles.monoCell}>{job.name}</td>
                <td className={styles.dimCell}>{job.namespace}</td>
                <td><span className={`pill ${PHASE_COLORS[job.phase]}`}>{job.phase}</span></td>
                <td>
                  <span className={styles.monoCell}>{job.gpuModel}</span><br />
                  <span className={styles.dimCell}>{Math.round(job.allocRatio*100)}% alloc</span>
                </td>
                <td><CheckpointTicks count={job.checkpointCount} /></td>
                <td className={styles.dimCell}>{fmtRelTime(job.lastCheckpointTime)}</td>
                <td className={styles.monoCell}>{job.currentNodeName || '—'}</td>
                <td>
                  <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                    <Link
                      to={`/jobs/${job.namespace}/${job.name}`}
                      className={styles.viewLink}
                      onClick={e => e.stopPropagation()}>
                      Detail
                    </Link>
                    {job.phase === 'Running' && (
                      <button
                        className={styles.ckptBtn}
                        onClick={e => {
                          e.stopPropagation()
                          trigger.mutate({ namespace: job.namespace, name: job.name })
                        }}>
                        Checkpoint
                      </button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Checkpoint history panel */}
      {selected && (
        <div className={styles.detailPanel}>
          <div className={styles.detailHeader}>
            <span className={styles.monoCell}>{selected.name}</span>
            <span className={styles.dimCell}>{selected.checkpointCount} checkpoints total</span>
            <button className={styles.closeBtn} onClick={() => setSelected(null)}>✕</button>
          </div>

          <div className={styles.snapList}>
            {selected.snapshots.length === 0
              ? <div className={styles.dimCell}>No snapshots loaded — click a job to load history</div>
              : selected.snapshots.slice().reverse().map(s => (
                  <div key={s.seq} className={styles.snapRow}>
                    <span className={styles.snapSeq}>#{s.seq.toString().padStart(5,'0')}</span>
                    <span className={styles.dimCell}>{new Date(s.createdAt).toLocaleTimeString()}</span>
                    <span className={styles.snapSize}>{fmtBytes(s.sizeBytes)}</span>
                    <span className={styles.snapDur}>{s.durationMS}ms</span>
                    <span className="pill pillGreen" style={{fontSize:'10px'}}>OK</span>
                  </div>
                ))
            }
          </div>
        </div>
      )}
    </div>
  )
}

// Checkpoint tick visualization — last N checkpoints as mini bars
function CheckpointTicks({ count }: { count: number }) {
  const MAX = 10
  const shown = Math.min(count, MAX)
  return (
    <div style={{ display:'flex', gap:2, alignItems:'center' }}>
      {Array.from({length: shown}, (_, i) => (
        <div key={i} style={{
          width: 6, height: 14, borderRadius: 1,
          background: i === shown-1 ? 'var(--amber)' : 'rgba(245,158,11,.45)',
          opacity: 0.4 + (i/shown)*0.6,
        }} />
      ))}
      <span style={{ fontFamily:'var(--mono)', fontSize:10, color:'var(--text3)', marginLeft:4 }}>
        {count}
      </span>
    </div>
  )
}
