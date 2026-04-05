import { render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import BillingDonut from './BillingDonut'
import type { DeptBilling } from '../api/client'

// recharts uses ResizeObserver internally which jsdom doesn't provide
vi.stubGlobal('ResizeObserver', class {
  observe() {}
  unobserve() {}
  disconnect() {}
})

const makeBilling = (overrides: Partial<DeptBilling> = {}): DeptBilling => ({
  department: '算法研究院',
  gpuHours: 620,
  tflopsHours: 193440,
  costCNY: 21700,
  quotaHours: 800,
  usedPct: 77.5,
  ...overrides,
})

describe('BillingDonut', () => {
  it('shows empty state when no data', () => {
    render(<BillingDonut data={[]} />)
    expect(screen.getByText('No billing data')).toBeTruthy()
  })

  it('renders legend items for each department', () => {
    const data = [
      makeBilling({ department: '算法研究院', costCNY: 21700 }),
      makeBilling({ department: 'NLP平台组', costCNY: 16800 }),
      makeBilling({ department: 'CV工程组', costCNY: 13300 }),
    ]
    render(<BillingDonut data={data} />)
    expect(screen.getByText('算法研究院')).toBeTruthy()
    expect(screen.getByText('NLP平台组')).toBeTruthy()
    expect(screen.getByText('CV工程组')).toBeTruthy()
  })

  it('shows total cost in centre label', () => {
    const data = [
      makeBilling({ department: 'Dept A', costCNY: 30000 }),
      makeBilling({ department: 'Dept B', costCNY: 20000 }),
    ]
    // total = 50000, displayed as ¥50K
    render(<BillingDonut data={data} />)
    expect(screen.getByText('¥50K')).toBeTruthy()
    expect(screen.getByText('total')).toBeTruthy()
  })

  it('renders percentage in legend', () => {
    const data = [
      makeBilling({ department: 'A', costCNY: 75 }),
      makeBilling({ department: 'B', costCNY: 25 }),
    ]
    render(<BillingDonut data={data} />)
    expect(screen.getByText('75%')).toBeTruthy()
    expect(screen.getByText('25%')).toBeTruthy()
  })

  it('handles single department without crash', () => {
    const data = [makeBilling({ department: 'Solo Dept', costCNY: 10000 })]
    render(<BillingDonut data={data} />)
    expect(screen.getByText('Solo Dept')).toBeTruthy()
    expect(screen.getByText('100%')).toBeTruthy()
  })
})
