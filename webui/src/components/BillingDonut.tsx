// BillingDonut — Department cost breakdown placeholder
// Full recharts PieChart deferred to Sprint 8
import type { DeptBilling } from '../api/client'

interface Props {
  data: DeptBilling[]
}

const COLORS = ['#6366f1', '#f59e0b', '#22c55e', '#ef4444', '#8b5cf6']

export default function BillingDonut({ data }: Props) {
  if (data.length === 0) return <div style={{ color: '#9ca3af' }}>No data</div>

  const total = data.reduce((s, d) => s + d.costCNY, 0)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      {data.map((d, i) => {
        const pct = total > 0 ? (d.costCNY / total) * 100 : 0
        return (
          <div key={d.department} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <div style={{
              width: 10, height: 10, borderRadius: '50%',
              background: COLORS[i % COLORS.length],
            }} />
            <span style={{ flex: 1, fontSize: 13 }}>{d.department}</span>
            <span style={{ fontFamily: 'monospace', fontSize: 12 }}>{pct.toFixed(0)}%</span>
            <span style={{ fontFamily: 'monospace', fontSize: 12, color: '#6b7280' }}>
              ¥{d.costCNY.toLocaleString()}
            </span>
          </div>
        )
      })}
    </div>
  )
}
