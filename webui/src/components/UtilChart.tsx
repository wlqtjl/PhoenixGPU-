// UtilChart — GPU utilization history chart placeholder
// Full recharts implementation deferred to Sprint 8
import type { TimeSeriesPoint } from '../api/client'

interface Props {
  data: TimeSeriesPoint[]
}

export default function UtilChart({ data }: Props) {
  if (data.length === 0) return <div style={{ color: '#9ca3af' }}>No data</div>

  const values = data.map(d => d.value)
  const max = Math.max(...values)
  const barWidth = Math.max(2, Math.floor(400 / values.length))

  return (
    <div style={{ display: 'flex', alignItems: 'flex-end', gap: 1, height: 120 }}>
      {values.map((v, i) => (
        <div
          key={i}
          title={`${v.toFixed(1)}%`}
          style={{
            width: barWidth,
            height: `${(v / (max || 1)) * 100}%`,
            background: v > 80 ? '#ef4444' : v > 60 ? '#f59e0b' : '#22c55e',
            borderRadius: 1,
            minHeight: 2,
          }}
        />
      ))}
    </div>
  )
}
