import { apiClient } from '@/shared/http'
import type { ManagedModel, ManagedModelPlatform, ModelMappings, ModelPolicy, ModelSource } from './managedModelTypes'

type RequestOptions = { signal?: AbortSignal }

/** Policy, mappings and pricing use independent server-owned CAS versions. */
export const managedModelsAPI = {
  async policy(platform: ManagedModelPlatform, options: RequestOptions = {}): Promise<ModelPolicy> {
    return (await apiClient.get<ModelPolicy>(`/admin/models/policies/${platform}`, options)).data
  },
  async save(policy: ModelPolicy, options: RequestOptions = {}): Promise<ModelPolicy> {
    return (await apiClient.put<ModelPolicy>(`/admin/models/policies/${policy.platform}`, policy, options)).data
  },
  async reset(platform: ManagedModelPlatform, version: number, options: RequestOptions = {}): Promise<ModelPolicy> {
    return (await apiClient.post<ModelPolicy>(`/admin/models/policies/${platform}/reset`, { version }, options)).data
  },
  async sync(platform: ManagedModelPlatform, version: number, accountIDs: number[], options: RequestOptions = {}): Promise<ModelPolicy> {
    return (await apiClient.post<ModelPolicy>(`/admin/models/policies/${platform}/sync`, { version, account_ids: accountIDs }, { ...options, timeout: 65000 })).data
  },
  async catalog(platform: string, options: RequestOptions = {}): Promise<ManagedModel[]> {
    return (await apiClient.get<ManagedModel[]>('/admin/models/catalog', { ...options, params: { platform } })).data
  },
  async sources(platform: ManagedModelPlatform, options: RequestOptions = {}): Promise<ModelSource[]> {
    return (await apiClient.get<ModelSource[]>('/admin/models/sources', { ...options, params: { platform } })).data
  },
  async mappings(options: RequestOptions = {}): Promise<ModelMappings> {
    return (await apiClient.get<ModelMappings>('/admin/models/mappings', options)).data
  },
  async saveMappings(value: ModelMappings, options: RequestOptions = {}): Promise<ModelMappings> {
    return (await apiClient.put<ModelMappings>('/admin/models/mappings', value, options)).data
  }
}
