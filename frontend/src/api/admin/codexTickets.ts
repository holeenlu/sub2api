import { apiClient } from '../client'
import type { Account, ApiKey } from '@/types'

export type TicketStatus = NonNullable<Account['codex_turn_tickets']>[number] & {
  fingerprint_pending?: boolean
  captured_at?: string
  turn_state_present: boolean
  cookie_present: boolean
  fingerprint_commit?: string
  harvest_enabled: boolean
  harvest_paused: boolean
}

export interface TicketEvent {
  id: number
  model: string
  occurred_at: string
  kind: 'success' | 'miss' | 'error' | 'invalidation'
  trigger?: string
  ticket_length?: number | null
  duration_ms?: number
  reason_code?: string
  http_status?: number | null
  response_http_status?: number | null
  proxy_name?: string
  fingerprint_commit?: string
  fingerprint_predicted_model?: string
  fingerprint_probability?: number | null
  fingerprint_matched?: boolean | null
  challenge_expected_count?: number | null
  parsed_number_count?: number | null
  turn_state_present?: boolean | null
  cookie_present?: boolean | null
  verification_method?: string
  request_kind?: string
  request_route?: string
  ticket_generation_id?: string
  original_ticket_present?: boolean
  original_cookie_present?: boolean
  returned_cookie_present?: boolean
  returned_set_cookie_count?: number
  invalidated_current?: boolean
}

export interface TicketInvalidation extends TicketEvent {
  original_ticket?: string | null
  original_cookie?: string | null
  returned_ticket: string
  returned_cookie?: string | null
  returned_set_cookies?: string[]
}

export interface TicketDiagnostic {
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

export async function fingerprint(): Promise<{ commit: string; models: string[] }> {
  const { data } = await apiClient.get('/admin/accounts/codex-ticket-fingerprint')
  return data
}

export async function refreshFingerprint(): Promise<{ commit: string; models: string[] }> {
  const { data } = await apiClient.post('/admin/accounts/codex-ticket-fingerprint/refresh')
  return data
}

export async function events(id: number, params: { model?: string; filter?: string; page: number; page_size: number; start_time?: string; end_time?: string }): Promise<{ items: TicketEvent[]; total: number }> {
  const { data } = await apiClient.get(`${base(id)}/codex-ticket-events`, { params })
  return data
}

export async function invalidation(id: number, eventID: number): Promise<TicketInvalidation> {
  const { data } = await apiClient.get(`${base(id)}/codex-ticket-invalidations/${eventID}`)
  return data
}

export async function harvest(id: number, model: string): Promise<{ outcome: string; reason_code?: string }> {
  const { data } = await apiClient.post(`${base(id)}/codex-ticket-harvest`, { model }, { timeout: 120000 })
  return data
}


export async function ownKeys(): Promise<ApiKey[]> {
  const keys: ApiKey[] = []
  for (let page = 1; ; page++) {
    const { data } = await apiClient.get<{ items: ApiKey[]; total: number }>('/keys', { params: { page, page_size: 100, status: 'active' } })
    keys.push(...data.items)
    if (keys.length >= data.total || !data.items.length) return keys
  }
}

export async function cadence(): Promise<{ retry_min_seconds: number; retry_max_seconds: number; refresh_seconds: number }> {
  const { data } = await apiClient.get('/admin/accounts/codex-ticket-cadence')
  return data
}

export async function saveCadence(value: { retry_min_seconds: number; retry_max_seconds: number; refresh_seconds: number }): Promise<void> {
  await apiClient.put('/admin/accounts/codex-ticket-cadence', value)
}

export interface TicketParticipation {
  enabled: boolean
  models: Record<string, boolean>
}

export async function saveParticipation(id: number, value: TicketParticipation): Promise<TicketParticipation> {
  const { data } = await apiClient.put(`${base(id)}/codex-ticket-participation`, value)
  return data
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
  items: Array<TicketDiagnostic & { fingerprint_commit?: string; expected_count?: number; duration_ms: number }>
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
  const { data } = await apiClient.get<{ plan: DiagnosticPlan | null; summary: DiagnosticSummary; rules: DiagnosticRules }>(base(id) + '/codex-ticket-diagnostic')
  return data
}
export async function saveDiagnosticPlan(id: number, plan: Pick<DiagnosticPlan, 'api_key_id' | 'models' | 'enabled' | 'interval_minutes'>) {
  const { data } = await apiClient.put<DiagnosticPlan>(base(id) + '/codex-ticket-diagnostic', plan)
  return data
}
export async function diagnosticModels(id: number, keyID: number) {
  const { data } = await apiClient.get<{ choices: { models: string[]; items: Array<{ id: string; eligible: boolean; reason?: string }>; group_name: string; whitelist_enabled: boolean; commit: string } }>(base(id) + '/codex-ticket-diagnostic', { params: { api_key_id: keyID } })
  return data.choices
}

export async function startDiagnosticRun(id: number) {
  const { data } = await apiClient.post<DiagnosticRun>(base(id) + '/codex-ticket-diagnostic')
  return data
}
export async function diagnosticRuns(id: number) {
  const { data } = await apiClient.get<{ items: DiagnosticRun[] }>(base(id) + '/codex-ticket-diagnostic/history')
  return data
}

export async function cancelDiagnosticRun(id: number, runID: number) {
  await apiClient.post(base(id) + '/codex-ticket-diagnostic/' + runID + '/cancel')
}
