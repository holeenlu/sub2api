import { apiClient } from '../client'

export interface ManagedModel {
  id: string
  display_name: string
  kind: 'chat' | 'image'
  enabled: boolean
  sort: number
  aliases: string[]
  family?: string
  capabilities?: Record<string, unknown>
  params: { max_tokens_default: number; max_tokens_cap: number; thinking_fallback_budget: number; on_adaptive: string; on_enabled: string; default_effort?: string }
  betas: { pass_context_1m?: boolean; required?: string[]; drop?: string[]; allow_client: boolean }
  catalog?: { context_window?: number; max_context_window?: number; max_output_tokens?: number; supported_reasoning_levels?: string[]; default_reasoning_level?: string; input_modalities?: string[]; codex_tool_capabilities?: Record<string, unknown> }
}
export interface ModelPolicy {
  version: number
  platform: string
  source: string
  updated_at?: string
  catalog_mode: string
  models: Record<string, ManagedModel>
  defaults: { enabled: boolean; max_tokens: number; thinking_fallback_budget: number; strip_context_1m: boolean; context_1m_whitelist: string[]; normalize_thinking: boolean }
  aliases?: Record<string, string>
}
export interface ModelMappings { version: number; mappings: Record<string, string> }
export interface TokenPrice { input: number | null; output: number | null; cache_read: number | null; cache_5m?: number | null; cache_1h?: number | null }
export interface ModelPrice extends TokenPrice {
  tiers?: (TokenPrice & { above: number; unpriced?: boolean })[]
  modes?: Partial<Record<'fast' | 'flex' | 'priority', TokenPrice | null>>
  image?: TokenPrice | null
}
export interface PriceEntry { id: string; source: string; effective: ModelPrice | null; override: ModelPrice | null }
export interface ModelPrices { version: number; synced_at?: string; entries: PriceEntry[] }
export interface ModelSource { id: number; name: string; platform: string; type: string }

export const modelsAPI = {
  async policy(platform: string) { return (await apiClient.get<ModelPolicy>(`/admin/models/policies/${platform}`)).data },
  async save(policy: ModelPolicy) { return (await apiClient.put<ModelPolicy>(`/admin/models/policies/${policy.platform}`, policy)).data },
  async reset(platform: string, version: number) { return (await apiClient.post<ModelPolicy>(`/admin/models/policies/${platform}/reset`, { version })).data },
  async sync(platform: string, version: number, account_ids: number[]) { return (await apiClient.post<ModelPolicy>(`/admin/models/policies/${platform}/sync`, { version, account_ids }, { timeout: 65000 })).data },
  async catalog(platform: string) { return (await apiClient.get<ManagedModel[]>('/admin/models/catalog', { params: { platform } })).data },
  async sources(platform: string) { return (await apiClient.get<ModelSource[]>('/admin/models/sources', { params: { platform } })).data },
  async mappings() { return (await apiClient.get<ModelMappings>('/admin/models/mappings')).data },
  async saveMappings(value: ModelMappings) { return (await apiClient.put<ModelMappings>('/admin/models/mappings', value)).data },
  async prices(platform: string) { return (await apiClient.get<ModelPrices>('/admin/models/pricing', { params: { platform } })).data },
  async savePrice(platform: string, model: string, version: number, price: ModelPrice | null) { return (await apiClient.put<{ version: number }>('/admin/models/pricing', { platform, model, version, price })).data },
  async syncPrices(platform: string, version: number) { return (await apiClient.post<{ version: number }>('/admin/models/pricing/sync', { platform, version })).data },
  async saveAccountModels(id: number, expected: string[], models: string[]) { return (await apiClient.put<{ models: string[] | null }>(`/admin/models/accounts/${id}/allowlist`, { expected, models })).data }
}
