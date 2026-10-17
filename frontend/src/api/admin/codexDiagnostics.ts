import { apiClient } from '../client'
import type { ApiKey } from '@/types'

export interface DiagnosticItem {
  model: string
  gateway_error_code?: string
  status: 'normal' | 'degraded' | 'uncertain' | 'failed'
  reason?: string
  predicted_model?: string
  probability?: number
  parsed_number_count?: number
  http_status?: number
}

const base = (id: number) => `/admin/accounts/${id}`

export async function ownKeys(): Promise<ApiKey[]> {
  const keys: ApiKey[] = []
  for (let page = 1; ; page++) {
    const { data } = await apiClient.get<{ items: ApiKey[]; total: number }>('/keys', { params: { page, page_size: 100, status: 'active' } })
    keys.push(...data.items)
    if (keys.length >= data.total || !data.items.length) return keys
  }
}

export interface DiagnosticPlan {
  interval_minutes: number
  account_id: number
  owner_id: number
  api_key_id: number
  models: string[]
  enabled: boolean
  revision: number
  next_run_at: string | null
  updated_at: string
}
export interface DiagnosticSummary {
  stale?: boolean
  interval_minutes: number
  run_id: number
  status: string
  checked_at: string | null
  enabled: boolean
  next_run_at: string | null
}
export interface DiagnosticRun {
  id: number
  account_id: number
  owner_id: number
  api_key_id: number
  api_key_name: string
  plan_revision: number
  models: string[]
  source: 'manual' | 'scheduled'
  status: string
  reason?: string
  items: Array<DiagnosticItem & { fingerprint_commit?: string; expected_count?: number; duration_ms: number }>
  created_at: string
  started_at: string | null
  finished_at: string | null
  cancel_requested: boolean
}
export interface DiagnosticRules {
 default_interval_minutes: number
 min_interval_minutes: number
 max_interval_minutes: number
 max_models: number
 history_limit: number
 confidence_threshold: number
}
export async function diagnosticPlan(id: number) {
  const { data } = await apiClient.get<{ plan: DiagnosticPlan | null; summary: DiagnosticSummary; rules: DiagnosticRules }>(base(id) + '/codex-diagnostic')
  return data
}
export async function saveDiagnosticPlan(id: number, plan: Pick<DiagnosticPlan, 'api_key_id' | 'models' | 'enabled' | 'interval_minutes'>) {
  const { data } = await apiClient.put<DiagnosticPlan>(base(id) + '/codex-diagnostic', plan)
  return data
}
export async function diagnosticModels(id: number, keyID: number) {
  const { data } = await apiClient.get<{ choices: { models: string[]; items: Array<{ id: string; eligible: boolean; reason?: string }>; group_name: string; whitelist_enabled: boolean; commit: string } }>(base(id) + '/codex-diagnostic', { params: { api_key_id: keyID } })
  return data.choices
}

export async function startDiagnosticRun(id: number) {
  const { data } = await apiClient.post<DiagnosticRun>(base(id) + '/codex-diagnostic')
  return data
}
export async function diagnosticRuns(id: number) {
  const { data } = await apiClient.get<{ items: DiagnosticRun[] }>(base(id) + '/codex-diagnostic/history')
  return data
}

export async function cancelDiagnosticRun(id: number, runID: number) {
  await apiClient.post(base(id) + '/codex-diagnostic/' + runID + '/cancel')
}

export async function refreshFingerprint(): Promise<{ commit: string; models: string[] }> {
 const { data } = await apiClient.post('/admin/accounts/codex-diagnostic-fingerprint/refresh')
 return data
}
