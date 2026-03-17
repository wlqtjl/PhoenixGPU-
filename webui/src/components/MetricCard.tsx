// MetricCard — reusable KPI card component
// Copyright 2025 PhoenixGPU Authors
import styles from './MetricCard.module.css'
import clsx from 'clsx'

type Accent = 'amber' | 'green' | 'red' | 'blue' | 'purple'

interface Props {
  label:   string
  value:   string | number
  sub?:    string
  accent?: Accent
}

export default function MetricCard({ label, value, sub, accent = 'amber' }: Props) {
  return (
    <div className={clsx(styles.card, styles[accent])}>
      <div className={styles.label}>{label}</div>
      <div className={clsx(styles.value, styles[`val-${accent}`])}>{value}</div>
      {sub && <div className={styles.sub}>{sub}</div>}
    </div>
  )
}
