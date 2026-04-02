// UtilChart — GPU Utilisation Area Chart
// Uses recharts AreaChart with 30-min buckets
// Copyright 2025 PhoenixGPU Authors
import {
  AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip,
  ResponsiveContainer, ReferenceLine,
} from 'recharts'
import { format, parseISO } from 'date-fns'
import type { TimeSeriesPoint } from '../api/client'
import styles from './UtilChart.module.css'

interface Props {
  data: TimeSeriesPoint[]
}

function fmtLabel(iso: string): string {
  try { return format(parseISO(iso), 'HH:mm') }
  catch { return '' }
}

// Custom tooltip with dark theme
function CustomTooltip({ active, payload, label }: {
  active?: boolean
  payload?: { value: number }[]
  label?: string
}) {
  if (!active || !payload?.length) return null
  return (
    <div className={styles.tooltip}>
      <div className={styles.tooltipTime}>{label}</div>
      <div className={styles.tooltipVal}>{payload[0].value.toFixed(1)}%</div>
    </div>
  )
}

export default function UtilChart({ data }: Props) {
  if (!data.length) {
    return <div className={styles.empty}>No utilisation data</div>
  }

  const chartData = data.map(p => ({
    time:  fmtLabel(p.ts),
    value: parseFloat(p.value.toFixed(1)),
  }))

  return (
    <div className={styles.wrap}>
      <ResponsiveContainer width="100%" height={180}>
        <AreaChart data={chartData} margin={{ top: 8, right: 16, left: -10, bottom: 0 }}>
          <defs>
            <linearGradient id="utilGrad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%"  stopColor="#F59E0B" stopOpacity={0.25} />
              <stop offset="95%" stopColor="#F59E0B" stopOpacity={0.02} />
            </linearGradient>
          </defs>
          <CartesianGrid
            strokeDasharray="3 3"
            stroke="rgba(255,255,255,0.05)"
            vertical={false}
          />
          <XAxis
            dataKey="time"
            tick={{ fill: '#475569', fontSize: 10, fontFamily: 'var(--font-mono)' }}
            axisLine={false}
            tickLine={false}
            interval={Math.floor(chartData.length / 8)}
          />
          <YAxis
            domain={[0, 100]}
            tick={{ fill: '#475569', fontSize: 10, fontFamily: 'var(--font-mono)' }}
            axisLine={false}
            tickLine={false}
            tickFormatter={v => `${v}%`}
          />
          <Tooltip content={<CustomTooltip />} />
          <ReferenceLine y={80} stroke="rgba(239,68,68,0.3)" strokeDasharray="4 4" />
          <Area
            type="monotone"
            dataKey="value"
            stroke="#F59E0B"
            strokeWidth={2}
            fill="url(#utilGrad)"
            dot={false}
            activeDot={{ r: 4, fill: '#F59E0B', stroke: '#0D0F12', strokeWidth: 2 }}
          />
        </AreaChart>
      </ResponsiveContainer>
      <div className={styles.legend}>
        <span className={styles.legendDot} style={{ background: 'var(--amber-bright)' }} />
        GPU Utilisation %
        <span className={styles.legendRef} />
        Alert threshold (80%)
      </div>
    </div>
  )
}
