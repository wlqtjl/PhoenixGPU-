// Job Detail page — comprehensive view of a single PhoenixJob
// Copyright 2025 PhoenixGPU Authors
import { useParams, Link } from 'react-router-dom'
import { useJob, useTriggerCheckpoint, type Phase } from '../api/client'
import styles from './JobDetail.module.css'

const PHASE_COLORS: Record<Phase, string> = {
  Running:       'pillGreen',
  Checkpointing: 'pillAmber',
  Restoring:     'pillBlue',
  Succeeded:     'pillGreen',
  Failed:        'pillRed',
  Pending:       'pillPurple',
}

function fmtBytes(b: number): string {
  if (b >= 1e9) return `${(b/1e9).toFixed(2)} GB`
  if (b >= 1e6) return `${(b/1e6).toFixed(1)} MB`
  return `${b} B`
}

function fmtDuration(ms: number): string {
  if (ms >= 60_000) return `${(ms/60_000).toFixed(1)} min`
  if (ms >= 1_000) return `${(ms/1_000).toFixed(1)} s`
  return `${ms} ms`
}

function fmtRelTime(iso: string | null): string {
  if (!iso) return '—'
  const diff = Date.now() - new Date(iso).getTime()
  const m = Math.floor(diff / 60_000)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

function fmtDateTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString('zh-CN', {
      year: 'numeric', month: '2-digit', day: '2-digit',
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    })
  } catch { return iso }
}

export default function JobDetail() {
  const { namespace, name } = useParams<{ namespace: string; name: string }>()
  const { data: job, isLoading, isError } = useJob(namespace ?? '', name ?? '')
  const trigger = useTriggerCheckpoint()

  if (isLoading) return <div className={styles.loading}>Loading job details...</div>

  if (isError || !job) {
    return (
      <div className={styles.notFound}>
        <div className={styles.notFoundTitle}>Job not found</div>
        <div className={styles.notFoundSub}>
          Job &quot;{namespace}/{name}&quot; does not exist
        </div>
        <Link to="/jobs" className={styles.backLink}>← Back to Jobs</Link>
      </div>
    )
  }

  const elapsed = job.startedAt
    ? Math.round((Date.now() - new Date(job.startedAt).getTime()) / 3_600_000)
    : 0

  return (
    <div className={styles.page}>
      {/* Breadcrumb */}
      <div className={styles.breadcrumb}>
        <Link to="/jobs" className={styles.breadLink}>PhoenixJobs</Link>
        <span className={styles.breadSep}>›</span>
        <span className={styles.breadCurrent}>{job.namespace}/{job.name}</span>
      </div>

      {/* Job header */}
      <div className={styles.header}>
        <div className={styles.headerTop}>
          <div className={styles.headerLeft}>
            <div className={styles.jobName}>{job.name}</div>
            <div className={styles.jobNs}>{job.namespace} · {job.department} · {job.project}</div>
          </div>
          <div className={styles.headerActions}>
            <span className={`pill ${PHASE_COLORS[job.phase]}`}>{job.phase}</span>
            {job.phase === 'Running' && (
              <button
                className={styles.ckptBtn}
                onClick={() => trigger.mutate({ namespace: job.namespace, name: job.name })}>
                Trigger Checkpoint
              </button>
            )}
          </div>
        </div>
      </div>

      {/* Stats row */}
      <div className={styles.statsGrid}>
        <StatBlock label="Phase" value={job.phase} color={phaseColor(job.phase)} />
        <StatBlock label="GPU" value={job.gpuModel} sub={`${Math.round(job.allocRatio * 100)}% alloc`} />
        <StatBlock label="Checkpoints" value={String(job.checkpointCount)} />
        <StatBlock label="Restores" value={String(job.restoreAttempts)} color={job.restoreAttempts > 0 ? 'var(--amber)' : undefined} />
        <StatBlock label="Elapsed" value={`${elapsed}h`} />
        <StatBlock label="Node" value={job.currentNodeName || '—'} />
      </div>

      {/* Job info panel */}
      <div className={styles.panel}>
        <div className={styles.panelTitle}>Job Information</div>
        <div className={styles.infoGrid}>
          <InfoRow label="Name" value={job.name} />
          <InfoRow label="Namespace" value={job.namespace} />
          <InfoRow label="Department" value={job.department} />
          <InfoRow label="Project" value={job.project} />
          <InfoRow label="Started At" value={fmtDateTime(job.startedAt)} />
          <InfoRow label="Last Checkpoint" value={job.lastCheckpointTime ? `${fmtDateTime(job.lastCheckpointTime)} (${fmtRelTime(job.lastCheckpointTime)})` : '—'} />
          <InfoRow label="Checkpoint Dir" value={job.lastCheckpointDir || '—'} />
          <InfoRow label="Current Pod" value={job.currentPodName || '—'} />
          <InfoRow label="Current Node" value={job.currentNodeName || '—'} />
          <InfoRow label="GPU Model" value={job.gpuModel} />
          <InfoRow label="Alloc Ratio" value={`${(job.allocRatio * 100).toFixed(1)}%`} />
        </div>
      </div>

      {/* Snapshot timeline */}
      <div className={styles.panel}>
        <div className={styles.panelTitle}>
          Checkpoint Snapshots
          <span className={styles.panelBadge}>{job.snapshots.length}</span>
        </div>
        {job.snapshots.length === 0 ? (
          <div className={styles.emptySnaps}>No snapshots recorded for this job</div>
        ) : (
          <>
            {/* Timeline visualization */}
            <div className={styles.timeline}>
              {[...job.snapshots].reverse().map((s, _idx) => (
                <div key={s.seq} className={styles.timelineItem}>
                  <div className={styles.timelineDot} />
                  <div className={styles.timelineContent}>
                    <div className={styles.snapHeader}>
                      <span className={styles.snapSeq}>#{s.seq.toString().padStart(5, '0')}</span>
                      <span className="pill pillGreen" style={{ fontSize: '10px' }}>OK</span>
                    </div>
                    <div className={styles.snapMeta}>
                      <span>{fmtDateTime(s.createdAt)}</span>
                      <span className={styles.snapDivider}>·</span>
                      <span>{fmtBytes(s.sizeBytes)}</span>
                      <span className={styles.snapDivider}>·</span>
                      <span>{fmtDuration(s.durationMS)}</span>
                    </div>
                    <div className={styles.snapDetail}>
                      Node: {s.nodeName} · Pod: {s.podName} · GPU: {s.gpuModel}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  )
}

function StatBlock({ label, value, sub, color }: {
  label: string; value: string; sub?: string; color?: string
}) {
  return (
    <div className={styles.statBlock}>
      <div className={styles.statLabel}>{label}</div>
      <div className={styles.statValue} style={color ? { color } : undefined}>{value}</div>
      {sub && <div className={styles.statSub}>{sub}</div>}
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

function phaseColor(phase: Phase): string {
  const map: Record<Phase, string> = {
    Running: 'var(--green)', Checkpointing: 'var(--amber)', Restoring: 'var(--blue)',
    Succeeded: 'var(--green)', Failed: 'var(--red)', Pending: 'var(--purple)',
  }
  return map[phase]
}
