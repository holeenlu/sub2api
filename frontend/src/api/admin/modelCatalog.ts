import { apiClient } from '../client'
import type { UpstreamModelMetadata } from './accounts'

export interface CatalogModel {
  id: string
  display_name: string
  platform: string
  kind: string
  lifecycle: 'active' | 'deprecated' | 'retired' | 'unknown'
  shutdown_date?: string
  access: 'listed' | 'configured' | 'candidate' | 'observed' | 'unlisted'
  source: string
  metadata: UpstreamModelMetadata
  missing: string[]
  endpoints: string[]
}

export interface CatalogPolicy { mode: "legacy" | "fixed" | "follow"; models: string[]; excluded: string[] }
export interface CatalogRelease { revision:string; created_at:string; operation:string }
export interface ModelCatalog {
  policy?: CatalogPolicy
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
}

export async function getModelCatalog(params: { account_id?: number; platform?: string }, signal?: AbortSignal): Promise<ModelCatalog> {
  const { data } = await apiClient.get<ModelCatalog>('/admin/model-catalog', { params, signal })
  return data
}

export async function refreshModelCatalog(accountID: number, signal?: AbortSignal): Promise<ModelCatalog> {
  const { data: job } = await apiClient.post<CatalogJob>('/admin/model-catalog/refresh', { account_id: accountID }, { signal })
  const deadline = Date.now() + 125000
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
  if (current.status === 'failed') throw new Error(current.error || 'model_catalog_refresh_failed')
  return getModelCatalog({ account_id: accountID }, signal)
}

export interface CatalogSyncSettings {
  priority_account_ids?: number[]
  priority_interval_seconds?: number
  price_interval_seconds?: number
  enabled: boolean
  interval_seconds: number
  timeout_seconds: number
  concurrency: number
  stale_seconds: number
}
export async function getCatalogSettings(): Promise<CatalogSyncSettings> {
  const { data } = await apiClient.get<CatalogSyncSettings>('/admin/model-catalog/settings'); return data
}
export async function saveCatalogSettings(value: CatalogSyncSettings): Promise<void> {
  await apiClient.put('/admin/model-catalog/settings', value)
}
export async function getCatalogRegistry(): Promise<CatalogModel[]> {
  const { data } = await apiClient.get<CatalogModel[]>('/admin/model-catalog/registry'); return data
}
export async function saveCatalogRegistry(models: unknown[]): Promise<void> {
  await apiClient.put('/admin/model-catalog/registry', { models })
}

export async function saveAccountCatalogPolicy(id:number,policy:CatalogPolicy):Promise<void>{await apiClient.put(`/admin/model-catalog/accounts/${id}/policy`,policy)}
export async function getCatalogHistory(id:number):Promise<CatalogRelease[]>{const {data}=await apiClient.get<{items:CatalogRelease[]}>(`/admin/model-catalog/accounts/${id}/history`);return data.items}
export async function rollbackCatalog(id:number,revision:string):Promise<void>{await apiClient.post(`/admin/model-catalog/accounts/${id}/rollback`,{revision})}
export async function getCatalogPrices():Promise<{revision:string;updated_at:string;supplemental:Record<string,unknown>}>{const {data}=await apiClient.get('/admin/model-catalog/prices');return data??{supplemental:{}}}
export async function saveCatalogPrices(models:Record<string,unknown>):Promise<void>{await apiClient.put('/admin/model-catalog/prices',{models})}
export async function explainCatalog(groupID:number):Promise<unknown>{const {data}=await apiClient.get('/admin/model-catalog/explain',{params:{group_id:groupID}});return data}

export interface CatalogTicketCandidate {model:string; fingerprint_available:boolean; access:string}
export async function getCatalogTicketModels(id:number):Promise<CatalogTicketCandidate[]>{const {data}=await apiClient.get(`/admin/model-catalog/accounts/${id}/ticket-models`);return data}
