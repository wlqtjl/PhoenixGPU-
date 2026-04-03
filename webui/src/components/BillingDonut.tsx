// BillingDonut — Cost breakdown pie chart
// Uses recharts PieChart with DeptBilling data
// Copyright 2025 PhoenixGPU Authors
import { PieChart, Pie, Cell, Tooltip, ResponsiveContainer } from 'recharts'
import type { DeptBilling } from '../api/client'
import styles from './BillingDonut.module.css'

const PALETTE = [
  '#F59E0B', // amber
  '#3B82F6', // blue
  '#10B981', // green
  '#8B5CF6', // purple
  '#F87171', // red-muted
  '#34D399', // emerald
]

interface Props {
  data: DeptBilling[]
}

function CustomTooltip({ active, payload }: {
  active?: boolean
  payload?: { name: string; value: number; payload: DeptBilling }[]
}) {
  if (!active || !payload?.length) return null
  const d = payload[0].payload
  return (
    <div className={styles.tooltip}>
      <div className={styles.tooltipDept}>{d.department}</div>
      <div className={styles.tooltipCost}>¥{d.costCNY.toLocaleString()}</div>
      <div className={styles.tooltipHours}>{d.gpuHours.toFixed(0)} GPU·h</div>
    </div>
  )
}

export default function BillingDonut({ data }: Props) {
  if (!data.length) {
    return <div className={styles.empty}>No billing data</div>
  }

  const total = data.reduce((s, d) => s + d.costCNY, 0)

  const chartData = data.map(d => ({
    name: d.department,
    value: d.costCNY,
    ...d,
  }))

  return (
    <div className={styles.wrap}>
      <div className={styles.chartArea}>
        <ResponsiveContainer width="100%" height={180}>
          <PieChart>
            <Pie
              data={chartData}
              cx="50%"
              cy="50%"
              innerRadius={52}
              outerRadius={78}
              paddingAngle={3}
              dataKey="value"
              strokeWidth={0}
            >
              {chartData.map((_, idx) => (
                <Cell key={idx} fill={PALETTE[idx % PALETTE.length]} opacity={0.9} />
              ))}
            </Pie>
            <Tooltip content={<CustomTooltip />} />
          </PieChart>
        </ResponsiveContainer>
        {/* Centre total */}
        <div className={styles.centreLabel}>
          <div className={styles.centreValue}>¥{(total / 1000).toFixed(0)}K</div>
          <div className={styles.centreSub}>total</div>
        </div>
      </div>

      {/* Legend */}
      <div className={styles.legend}>
        {data.map((d, idx) => (
          <div key={d.department} className={styles.legendItem}>
            <span
              className={styles.legendDot}
              style={{ background: PALETTE[idx % PALETTE.length] }}
            />
            <span className={styles.legendName}>{d.department}</span>
            <span className={styles.legendPct}>{Math.round(d.costCNY / total * 100)}%</span>
          </div>
        ))}
      </div>
    </div>
  )
}
