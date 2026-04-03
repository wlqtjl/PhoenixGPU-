// PhoenixGPU API Client
// All types mirror the Go structs in pkg/
// Copyright 2025 PhoenixGPU Authors

import axios from 'axios'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'

// ── Base client ────────────────────────────────────────────────

const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL ?? '/api/v1',
  timeout: 10_000,
  headers: { 'Content-Type': 'application/json' },
})

// ── API Types ──────────────────────────────────────────────────

export type Phase =
  | 'Pending' | 'Running' | 'Checkpointing'
  | 'Restoring' | 'Succeeded' | 'Failed'

export interface SnapshotMeta {
  namespace:    string
  jobName:      string
  seq:          number
  nodeName:     string
  podName:      string
  gpuModel:     string
  createdAt:    string   // ISO 8601
  durationMS:   number
  sizeBytes:    number
}

export interface PhoenixJob {
  name:              string
  namespace:         string
  phase:             Phase
  checkpointCount:   number
  restoreAttempts:   number
  lastCheckpointTime: string | null
  lastCheckpointDir: string
  currentPodName:    string
  currentNodeName:   string
  gpuModel:          string
  allocRatio:        number
  department:        string
  project:           string
  startedAt:         string
  snapshots:         SnapshotMeta[]
}

export interface GPUNode {
  name:          string
  gpuModel:      string
  gpuCount:      number
  vramTotalMiB:  number
  vramUsedMiB:   number
  smUtilPct:     number
  powerWatt:     number
  tempCelsius:   number
  ready:         boolean
  faulted:       boolean
}

export interface ClusterSummary {
  totalGPUs:      number
  activeJobs:     number
  checkpointingJobs: number
  restoringJobs:  number
  avgUtilPct:     number
  alertCount:     number
  totalGPUHours:  number
  totalCostCNY:   number
}

export interface DeptBilling {
  department:    string
  gpuHours:      number
  tflopsHours:   number
  costCNY:       number
  quotaHours:    number
  usedPct:       number
}

export interface UsageRecord {
  namespace:    string
  podName:      string
  jobName:      string
  department:   string
  project:      string
  gpuModel:     string
  nodeName:     string
  allocRatio:   number
  startedAt:    string
  endedAt:      string
  durationHours: number
  tflopsHours:  number
  costCNY:      number
  gpuHours:     number
}

export interface TimeSeriesPoint {
  ts:      string
  value:   number
}

export interface Alert {
  id:        string
  severity:  'info' | 'warn' | 'error'
  tenant:    string
  message:   string
  createdAt: string
  resolved:  boolean
}

// ── Query keys ────────────────────────────────────────────────

export const QK = {
  cluster:  ['cluster'] as const,
  nodes:    ['nodes'] as const,
  jobs:     (ns?: string) => ['jobs', ns] as const,
  job:      (ns: string, name: string) => ['job', ns, name] as const,
  billing:  (period: string) => ['billing', period] as const,
  records:  (dept?: string) => ['records', dept] as const,
  alerts:   ['alerts'] as const,
  utilHistory: (hours: number) => ['util-history', hours] as const,
} as const

// ── API functions ──────────────────────────────────────────────

export const fetchClusterSummary = (): Promise<ClusterSummary> =>
  api.get('/cluster/summary').then(r => r.data)

export const fetchNodes = (): Promise<GPUNode[]> =>
  api.get('/nodes').then(r => r.data)

export const fetchJobs = (namespace?: string): Promise<PhoenixJob[]> =>
  api.get('/jobs', { params: { namespace } }).then(r => r.data)

export const fetchJob = (namespace: string, name: string): Promise<PhoenixJob> =>
  api.get(`/jobs/${namespace}/${name}`).then(r => r.data)

export const fetchBilling = (period: string): Promise<DeptBilling[]> =>
  api.get('/billing/departments', { params: { period } }).then(r => r.data)

export const fetchRecords = (department?: string): Promise<UsageRecord[]> =>
  api.get('/billing/records', { params: { department } }).then(r => r.data)

export const fetchAlerts = (): Promise<Alert[]> =>
  api.get('/alerts').then(r => r.data)

export const fetchUtilHistory = (hours: number): Promise<TimeSeriesPoint[]> =>
  api.get('/cluster/utilization-history', { params: { hours } }).then(r => r.data)

export const triggerCheckpoint = (namespace: string, name: string): Promise<void> =>
  api.post(`/jobs/${namespace}/${name}/checkpoint`).then(r => r.data)

export const resolveAlert = (id: string): Promise<void> =>
  api.post(`/alerts/${id}/resolve`).then(r => r.data)

// ── React Query hooks ──────────────────────────────────────────

export function useClusterSummary() {
  return useQuery({
    queryKey: QK.cluster,
    queryFn: fetchClusterSummary,
    refetchInterval: 15_000,
    staleTime: 10_000,
  })
}

export function useNodes() {
  return useQuery({
    queryKey: QK.nodes,
    queryFn: fetchNodes,
    refetchInterval: 10_000,
    staleTime: 8_000,
  })
}

export function useJobs(namespace?: string) {
  return useQuery({
    queryKey: QK.jobs(namespace),
    queryFn: () => fetchJobs(namespace),
    refetchInterval: 10_000,
    staleTime: 8_000,
  })
}

export function useBilling(period: string) {
  return useQuery({
    queryKey: QK.billing(period),
    queryFn: () => fetchBilling(period),
    staleTime: 60_000,
  })
}

export function useAlerts() {
  return useQuery({
    queryKey: QK.alerts,
    queryFn: fetchAlerts,
    refetchInterval: 20_000,
  })
}

export function useUtilHistory(hours: number) {
  return useQuery({
    queryKey: QK.utilHistory(hours),
    queryFn: () => fetchUtilHistory(hours),
    refetchInterval: 30_000,
  })
}

export function useTriggerCheckpoint() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) =>
      triggerCheckpoint(namespace, name),
    onSuccess: () => qc.invalidateQueries({ queryKey: QK.jobs() }),
  })
}

export function useResolveAlert() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => resolveAlert(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: QK.alerts }),
  })
}

// ── Mock data for development (used when VITE_MOCK=true) ───────

export const MOCK: {
  summary: ClusterSummary
  nodes: GPUNode[]
  jobs: PhoenixJob[]
  billing: DeptBilling[]
  alerts: Alert[]
  utilHistory: TimeSeriesPoint[]
} = {
  summary: {
    totalGPUs: 32, activeJobs: 18, checkpointingJobs: 2,
    restoringJobs: 1, avgUtilPct: 74, alertCount: 3,
    totalGPUHours: 1840, totalCostCNY: 64400,
  },
  nodes: [
    { name:'gpu-node-01', gpuModel:'NVIDIA A100 80GB', gpuCount:8,
      vramTotalMiB:81920, vramUsedMiB:61440, smUtilPct:82, powerWatt:380, tempCelsius:72, ready:true, faulted:false },
    { name:'gpu-node-02', gpuModel:'NVIDIA A100 80GB', gpuCount:8,
      vramTotalMiB:81920, vramUsedMiB:49152, smUtilPct:65, powerWatt:310, tempCelsius:68, ready:true, faulted:false },
    { name:'gpu-node-03', gpuModel:'NVIDIA A100 40GB', gpuCount:8,
      vramTotalMiB:40960, vramUsedMiB:32768, smUtilPct:71, powerWatt:270, tempCelsius:64, ready:true, faulted:false },
    { name:'gpu-node-04', gpuModel:'NVIDIA H800', gpuCount:8,
      vramTotalMiB:81920, vramUsedMiB:73728, smUtilPct:91, powerWatt:420, tempCelsius:79, ready:true, faulted:false },
  ],
  jobs: [
    { name:'llm-pretrain-v3', namespace:'research', phase:'Running',
      checkpointCount:24, restoreAttempts:0, lastCheckpointTime:'2026-03-17T02:30:00Z',
      lastCheckpointDir:'/mnt/snapshots/research/llm-pretrain-v3/ckpt-00024',
      currentPodName:'llm-pretrain-v3-pod', currentNodeName:'gpu-node-04',
      gpuModel:'NVIDIA H800', allocRatio:0.5, department:'算法研究院', project:'LLM预训练',
      startedAt:'2026-03-14T08:00:00Z',
      snapshots: Array.from({length:5}, (_,i) => ({
        namespace:'research', jobName:'llm-pretrain-v3', seq:20+i,
        nodeName:'gpu-node-04', podName:'llm-pretrain-v3-pod',
        gpuModel:'NVIDIA H800', createdAt:new Date(Date.now()-(4-i)*300_000).toISOString(),
        durationMS:8200-(i*100), sizeBytes:2_800_000_000+(i*50_000_000),
      })) },
    { name:'rlhf-finetune', namespace:'nlp', phase:'Checkpointing',
      checkpointCount:8, restoreAttempts:0, lastCheckpointTime:'2026-03-17T02:10:00Z',
      lastCheckpointDir:'/mnt/snapshots/nlp/rlhf-finetune/ckpt-00008',
      currentPodName:'rlhf-pod', currentNodeName:'gpu-node-01',
      gpuModel:'NVIDIA A100 80GB', allocRatio:0.25, department:'NLP平台组', project:'RLHF微调',
      startedAt:'2026-03-16T10:00:00Z', snapshots:[] },
    { name:'cv-detection-v2', namespace:'cv', phase:'Restoring',
      checkpointCount:12, restoreAttempts:1, lastCheckpointTime:'2026-03-16T22:00:00Z',
      lastCheckpointDir:'/mnt/snapshots/cv/cv-detection-v2/ckpt-00012',
      currentPodName:'cv-pod-new', currentNodeName:'gpu-node-02',
      gpuModel:'NVIDIA A100 40GB', allocRatio:0.5, department:'CV工程组', project:'目标检测2.0',
      startedAt:'2026-03-15T14:00:00Z', snapshots:[] },
    { name:'embedding-eval', namespace:'nlp', phase:'Succeeded',
      checkpointCount:5, restoreAttempts:0, lastCheckpointTime:'2026-03-16T18:00:00Z',
      lastCheckpointDir:'/mnt/snapshots/nlp/embedding-eval/ckpt-00005',
      currentPodName:'', currentNodeName:'',
      gpuModel:'NVIDIA A100 40GB', allocRatio:0.125, department:'NLP平台组', project:'向量检索',
      startedAt:'2026-03-16T08:00:00Z', snapshots:[] },
  ],
  billing: [
    { department:'算法研究院', gpuHours:620, tflopsHours:193440, costCNY:21700, quotaHours:800, usedPct:77.5 },
    { department:'NLP平台组',  gpuHours:480, tflopsHours:149760, costCNY:16800, quotaHours:600, usedPct:80.0 },
    { department:'CV工程组',   gpuHours:380, tflopsHours:118560, costCNY:13300, quotaHours:500, usedPct:76.0 },
    { department:'推理基础设施',gpuHours:280, tflopsHours:87360,  costCNY:9800,  quotaHours:400, usedPct:70.0 },
    { department:'数据工程部', gpuHours:150, tflopsHours:24750,  costCNY:1800,  quotaHours:300, usedPct:50.0 },
  ],
  alerts: [
    { id:'a1', severity:'error', tenant:'算法研究院',
      message:'月度配额已用 93%，预计 48h 内超限', createdAt:'2026-03-17T01:00:00Z', resolved:false },
    { id:'a2', severity:'warn', tenant:'cv-detection-v2',
      message:'任务 Restore 失败 1 次，正在第 2 次重试', createdAt:'2026-03-17T00:30:00Z', resolved:false },
    { id:'a3', severity:'warn', tenant:'gpu-node-03',
      message:'GPU 温度 64°C，接近告警阈值 70°C', createdAt:'2026-03-16T23:00:00Z', resolved:false },
  ],
  utilHistory: Array.from({length:48}, (_, i) => ({
    ts: new Date(Date.now() - (47-i)*30*60_000).toISOString(),
    value: 55 + Math.sin(i*0.3)*18 + Math.random()*8,
  })),
}
