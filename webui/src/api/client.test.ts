// API client unit tests — verifies fetch functions and query keys.
// Uses vitest mock for axios.
//
// Copyright 2025 PhoenixGPU Authors
import { describe, it, expect, vi, beforeEach } from 'vitest'
import axios from 'axios'
import {
  fetchClusterSummary,
  fetchNodes,
  fetchJobs,
  fetchJob,
  fetchBilling,
  fetchRecords,
  fetchAlerts,
  fetchUtilHistory,
  triggerCheckpoint,
  resolveAlert,
  QK,
} from './client'

// Mock axios — intercept all requests
vi.mock('axios', () => {
  const mockAxios = {
    create: vi.fn(() => mockAxios),
    get: vi.fn(),
    post: vi.fn(),
    defaults: { headers: { common: {} } },
    interceptors: {
      request: { use: vi.fn() },
      response: { use: vi.fn() },
    },
  }
  return { default: mockAxios }
})

const api = axios as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
  create: ReturnType<typeof vi.fn>
}

beforeEach(() => {
  vi.clearAllMocks()
})

// ── Query Keys ────────────────────────────────────────────────────

describe('QK (Query Keys)', () => {
  it('cluster key is stable', () => {
    expect(QK.cluster).toEqual(['cluster'])
  })

  it('nodes key is stable', () => {
    expect(QK.nodes).toEqual(['nodes'])
  })

  it('jobs key includes namespace', () => {
    expect(QK.jobs('research')).toEqual(['jobs', 'research'])
    expect(QK.jobs(undefined)).toEqual(['jobs', undefined])
  })

  it('job key includes namespace and name', () => {
    expect(QK.job('research', 'llm-v3')).toEqual(['job', 'research', 'llm-v3'])
  })

  it('billing key includes period', () => {
    expect(QK.billing('monthly')).toEqual(['billing', 'monthly'])
    expect(QK.billing('daily')).toEqual(['billing', 'daily'])
  })

  it('records key includes department', () => {
    expect(QK.records('NLP')).toEqual(['records', 'NLP'])
    expect(QK.records(undefined)).toEqual(['records', undefined])
  })

  it('alerts key is stable', () => {
    expect(QK.alerts).toEqual(['alerts'])
  })

  it('utilHistory key includes hours', () => {
    expect(QK.utilHistory(24)).toEqual(['util-history', 24])
  })
})

// ── Fetch functions ───────────────────────────────────────────────

describe('fetchClusterSummary', () => {
  it('calls GET /cluster/summary', async () => {
    const mockData = { totalGPUs: 32, activeJobs: 18 }
    api.get.mockResolvedValueOnce({ data: mockData })

    const result = await fetchClusterSummary()
    expect(api.get).toHaveBeenCalledWith('/cluster/summary')
    expect(result).toEqual(mockData)
  })
})

describe('fetchNodes', () => {
  it('calls GET /nodes', async () => {
    const mockData = [{ name: 'gpu-node-01' }]
    api.get.mockResolvedValueOnce({ data: mockData })

    const result = await fetchNodes()
    expect(api.get).toHaveBeenCalledWith('/nodes')
    expect(result).toEqual(mockData)
  })
})

describe('fetchJobs', () => {
  it('calls GET /jobs with namespace param', async () => {
    const mockData = [{ name: 'llm-v3' }]
    api.get.mockResolvedValueOnce({ data: mockData })

    const result = await fetchJobs('research')
    expect(api.get).toHaveBeenCalledWith('/jobs', { params: { namespace: 'research' } })
    expect(result).toEqual(mockData)
  })

  it('calls GET /jobs without namespace when undefined', async () => {
    api.get.mockResolvedValueOnce({ data: [] })

    await fetchJobs(undefined)
    expect(api.get).toHaveBeenCalledWith('/jobs', { params: { namespace: undefined } })
  })
})

describe('fetchJob', () => {
  it('calls GET /jobs/:ns/:name', async () => {
    const mockData = { name: 'llm-v3', namespace: 'research' }
    api.get.mockResolvedValueOnce({ data: mockData })

    const result = await fetchJob('research', 'llm-v3')
    expect(api.get).toHaveBeenCalledWith('/jobs/research/llm-v3')
    expect(result).toEqual(mockData)
  })
})

describe('fetchBilling', () => {
  it('calls GET /billing/departments with period', async () => {
    const mockData = [{ department: 'NLP' }]
    api.get.mockResolvedValueOnce({ data: mockData })

    const result = await fetchBilling('monthly')
    expect(api.get).toHaveBeenCalledWith('/billing/departments', { params: { period: 'monthly' } })
    expect(result).toEqual(mockData)
  })
})

describe('fetchRecords', () => {
  it('calls GET /billing/records with department', async () => {
    api.get.mockResolvedValueOnce({ data: [] })

    await fetchRecords('NLP')
    expect(api.get).toHaveBeenCalledWith('/billing/records', { params: { department: 'NLP' } })
  })

  it('calls GET /billing/records without department', async () => {
    api.get.mockResolvedValueOnce({ data: [] })

    await fetchRecords(undefined)
    expect(api.get).toHaveBeenCalledWith('/billing/records', { params: { department: undefined } })
  })
})

describe('fetchAlerts', () => {
  it('calls GET /alerts', async () => {
    const mockData = [{ id: 'a1', severity: 'warn' }]
    api.get.mockResolvedValueOnce({ data: mockData })

    const result = await fetchAlerts()
    expect(api.get).toHaveBeenCalledWith('/alerts')
    expect(result).toEqual(mockData)
  })
})

describe('fetchUtilHistory', () => {
  it('calls GET /cluster/utilization-history with hours', async () => {
    api.get.mockResolvedValueOnce({ data: [] })

    await fetchUtilHistory(48)
    expect(api.get).toHaveBeenCalledWith('/cluster/utilization-history', { params: { hours: 48 } })
  })
})

describe('triggerCheckpoint', () => {
  it('calls POST /jobs/:ns/:name/checkpoint', async () => {
    api.post.mockResolvedValueOnce({ data: {} })

    await triggerCheckpoint('research', 'llm-v3')
    expect(api.post).toHaveBeenCalledWith('/jobs/research/llm-v3/checkpoint')
  })
})

describe('resolveAlert', () => {
  it('calls POST /alerts/:id/resolve', async () => {
    api.post.mockResolvedValueOnce({ data: {} })

    await resolveAlert('alert-42')
    expect(api.post).toHaveBeenCalledWith('/alerts/alert-42/resolve')
  })
})
