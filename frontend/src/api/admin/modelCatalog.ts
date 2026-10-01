import { apiClient } from '../client'
import type { UpstreamModelMetadata } from './accounts'
import type { AccountModelCatalogPolicy } from '@/types'

export interface CatalogModel {
  id: string
  display_name: string
  platform: string
  kind: string
  disabled?: boolean
  lifecycle: 'active' | 'deprecated' | 'retired' | 'unknown'
  shutdown_date?: string
  access: 'listed' | 'configured' | 'candidate' | 'observed' | 'unlisted'
  source: string
  metadata: UpstreamModelMetadata
  missing: string[]
  endpoints: string[]
}

export type CatalogPolicy = AccountModelCatalogPolicy
export interface ModelCatalog {
  warnings?: string[]
  revision: string
  account_id?: number
  platform: string
  status: 'ready' | 'stale' | 'unavailable'
  updated_at: string
  checked_at: string
  last_error?: string
  models: CatalogModel[]
}

export interface CatalogJob {
  id: string
  account_id: number
  status: 'running' | 'complete' | 'failed'
  revision?: string
  error?: string
  succeeded?: number
  failed?: number
}

export async function getModelCatalog(params: { account_id?: number; platform?: string; view?: 'selection' }, signal?: AbortSignal): Promise<ModelCatalog> {
  const { data } = await apiClient.get<ModelCatalog>('/admin/model-catalog', { params, signal })
  return data
}

export async function refreshModelCatalog(accountID: number, signal?: AbortSignal, view?: 'selection'): Promise<ModelCatalog> {
  const { data: job } = await apiClient.post<CatalogJob>('/admin/model-catalog/refresh', { account_id: accountID }, { signal })
  await waitForCatalogJob(job, 125000, signal)
  return getModelCatalog({ account_id: accountID, ...(view ? { view } : {}) }, signal)
}

export async function syncModelCatalog(signal?: AbortSignal): Promise<CatalogJob> {
  const { data: job } = await apiClient.post<CatalogJob>('/admin/model-catalog/sync', {}, { signal })
  // A global sync may take longer than one account. The server owns the job.
  return waitForCatalogJob(job, 31 * 60 * 1000, signal, true)
}

async function waitForCatalogJob(job: CatalogJob, timeout: number, signal?: AbortSignal, allowPartial = false): Promise<CatalogJob> {
  const deadline = Date.now() + timeout
  let current = job
  while (current.status === 'running') {
    if (Date.now() >= deadline) throw new Error('model_catalog_refresh_timeout')
    await new Promise<void>((resolve, reject) => {
      if (signal?.aborted) { reject(new DOMException('Aborted', 'AbortError')); return }
      const done = () => { signal?.removeEventListener('abort', abort); resolve() }
      const timer = setTimeout(done, 700)
      const abort = () => { clearTimeout(timer); signal?.removeEventListener("abort", abort); reject(new DOMException('Aborted', 'AbortError')) }
      signal?.addEventListener('abort', abort, { once: true })
    })
    const { data } = await apiClient.get<CatalogJob>(`/admin/model-catalog/jobs/${job.id}`, { signal })
    current = data
  }
  if (current.status === 'failed' && !allowPartial) throw new Error(current.error || 'model_catalog_refresh_failed')
  return current
}

export interface CatalogSyncSettings {
  enabled: boolean
  interval_seconds: number
}
export async function getCatalogSettings(): Promise<CatalogSyncSettings> {
  const { data } = await apiClient.get<CatalogSyncSettings>('/admin/model-catalog/settings'); return data
}
export async function saveCatalogSettings(value: CatalogSyncSettings): Promise<void> {
  await apiClient.put('/admin/model-catalog/settings', value)
}
export interface CatalogModelInput {
  id: string
  platform: string
  display_name: string
  kind: string
  disabled: boolean
}
export async function saveCatalogModel(model: CatalogModelInput): Promise<void> {
  await apiClient.put('/admin/model-catalog/models', model)
}

export interface CatalogTicketCandidate {model:string; fingerprint_available:boolean; access:string}
export async function getCatalogTicketModels(id:number):Promise<CatalogTicketCandidate[]>{const {data}=await apiClient.get(`/admin/model-catalog/accounts/${id}/ticket-models`);return data}
