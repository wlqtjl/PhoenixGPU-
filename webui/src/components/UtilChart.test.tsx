import { render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import UtilChart from './UtilChart'
import type { TimeSeriesPoint } from '../api/client'

// recharts uses ResizeObserver internally which jsdom doesn't provide
vi.stubGlobal('ResizeObserver', class {
  observe() {}
  unobserve() {}
  disconnect() {}
})

function makePoints(count: number): TimeSeriesPoint[] {
  const now = Date.now()
  return Array.from({ length: count }, (_, i) => ({
    ts: new Date(now - (count - 1 - i) * 30 * 60_000).toISOString(),
    value: 50 + Math.sin(i * 0.5) * 20,
  }))
}

describe('UtilChart', () => {
  it('shows empty state when no data', () => {
    render(<UtilChart data={[]} />)
    expect(screen.getByText('No utilisation data')).toBeTruthy()
  })

  it('renders the chart legend', () => {
    render(<UtilChart data={makePoints(12)} />)
    expect(screen.getByText(/GPU Utilisation/)).toBeTruthy()
    expect(screen.getByText(/Alert threshold/)).toBeTruthy()
  })

  it('renders with a single data point without crash', () => {
    const point: TimeSeriesPoint = {
      ts: new Date().toISOString(),
      value: 72.5,
    }
    render(<UtilChart data={[point]} />)
    expect(screen.getByText(/GPU Utilisation/)).toBeTruthy()
  })

  it('renders with many data points', () => {
    render(<UtilChart data={makePoints(48)} />)
    expect(screen.getByText(/GPU Utilisation/)).toBeTruthy()
  })
})
