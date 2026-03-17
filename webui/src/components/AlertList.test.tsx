import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import AlertList from './AlertList'
import type { Alert } from '../api/client'

const makeAlert = (overrides: Partial<Alert> = {}): Alert => ({
  id: 'a1', severity: 'warn', tenant: 'NLP Lab',
  message: 'Quota at 85%', createdAt: new Date(Date.now()-600_000).toISOString(),
  resolved: false, ...overrides,
})

describe('AlertList', () => {
  it('shows empty state when no alerts', () => {
    render(<AlertList alerts={[]} />)
    expect(screen.getByText('No active alerts')).toBeTruthy()
  })

  it('renders each alert', () => {
    const alerts = [
      makeAlert({ id:'a1', message:'Quota at 85%' }),
      makeAlert({ id:'a2', severity:'error', message:'Node fault detected' }),
    ]
    render(<AlertList alerts={alerts} />)
    expect(screen.getAllByTestId('alert-item')).toHaveLength(2)
    expect(screen.getByText('Quota at 85%')).toBeTruthy()
    expect(screen.getByText('Node fault detected')).toBeTruthy()
  })

  it('shows tenant name', () => {
    render(<AlertList alerts={[makeAlert({ tenant: 'CV Lab' })]} />)
    expect(screen.getByText('CV Lab')).toBeTruthy()
  })

  it('calls onResolve with correct id', () => {
    const onResolve = vi.fn()
    render(<AlertList alerts={[makeAlert({ id:'alert-42' })]} onResolve={onResolve} />)
    fireEvent.click(screen.getByText('Resolve'))
    expect(onResolve).toHaveBeenCalledWith('alert-42')
  })

  it('does not show resolve button when onResolve not provided', () => {
    render(<AlertList alerts={[makeAlert()]} />)
    expect(screen.queryByText('Resolve')).toBeNull()
  })
})
