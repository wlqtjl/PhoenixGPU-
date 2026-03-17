// MetricCard.test.tsx — TDD tests written before full styling
import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import MetricCard from './MetricCard'

describe('MetricCard', () => {
  it('renders label and value', () => {
    render(<MetricCard label="Total GPUs" value={32} />)
    expect(screen.getByText('Total GPUs')).toBeTruthy()
    expect(screen.getByText('32')).toBeTruthy()
  })

  it('renders optional sub-text', () => {
    render(<MetricCard label="Utilization" value="74%" sub="↑ from last week" />)
    expect(screen.getByText('↑ from last week')).toBeTruthy()
  })

  it('does not render sub when not provided', () => {
    const { queryByText } = render(<MetricCard label="Jobs" value={18} />)
    expect(queryByText('↑')).toBeNull()
  })

  it('renders string value', () => {
    render(<MetricCard label="Cost" value="¥64,400" />)
    expect(screen.getByText('¥64,400')).toBeTruthy()
  })

  it('renders with all accent types without crashing', () => {
    const accents = ['amber','green','red','blue','purple'] as const
    accents.forEach(accent => {
      const { unmount } = render(
        <MetricCard label="Test" value="0" accent={accent} />
      )
      unmount()
    })
  })
})
