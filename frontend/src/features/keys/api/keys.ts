/**
 * API Keys management endpoints
 * Handles CRUD operations for user API keys
 */

import { apiClient } from '@/shared/http'
import type { PaginatedResponse } from '@/shared/http/requestTypes';
import type { ApiKey, CreateApiKeyRequest, UpdateApiKeyRequest } from './types'

export interface KeyRequestOptions { signal?: AbortSignal; isCurrent?: () => boolean }

function requireKeyRequest(options?: KeyRequestOptions): void {
  if (options?.signal?.aborted || options?.isCurrent?.() === false) {
    throw new DOMException('Key operation canceled', 'AbortError')
  }
}

export interface ApiKeyFilters {
  search?: string
  status?: string
  group_id?: number | string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export type ApiKeyDataRow = Pick<ApiKey,
  'name' | 'key' | 'ip_whitelist' | 'ip_blacklist' | 'quota' | 'quota_used' |
  'expires_at' | 'rate_limit_5h' | 'rate_limit_1d' | 'rate_limit_7d' |
  'usage_5h' | 'usage_1d' | 'usage_7d' | 'window_5h_start' | 'window_1d_start' | 'window_7d_start'
> & { status: ApiKey['status'] | 'disabled'; group: { name: string; platform: string } | null }

export interface ApiKeyData {
  type: 'sub2api-keys'
  version: 1
  exported_at: string
  keys: ApiKeyDataRow[]
}

export interface ApiKeyImportData { type: 'sub2api-keys'; version: 1; exported_at?: string; keys: unknown[] }

export interface ApiKeyImportResult {
  created: number
  skipped: number
  failed: number
  errors: Array<{ index: number; message: string }>
}

export async function exportData(filters: ApiKeyFilters, ids?: number[], options?: KeyRequestOptions): Promise<ApiKeyData> {
  const { data } = await apiClient.get<ApiKeyData>('/keys/export', {
    params: ids?.length ? { ids: ids.join(',') } : filters,
    signal: options?.signal
  })
  return data
}

export async function importData(data: ApiKeyImportData, options?: KeyRequestOptions): Promise<ApiKeyImportResult> {
  requireKeyRequest(options)
  const response = await apiClient.post<ApiKeyImportResult>('/keys/import', { data }, { signal: options?.signal })
  requireKeyRequest(options)
  return response.data
}

/**
 * List all API keys for current user
 * @param page - Page number (default: 1)
 * @param pageSize - Items per page (default: 10)
 * @param filters - Optional filter parameters
 * @param options - Optional request options
 * @returns Paginated list of API keys
 */
export async function list(
  page: number = 1,
  pageSize: number = 10,
  filters?: ApiKeyFilters,
  options?: {
    signal?: AbortSignal
  }
): Promise<PaginatedResponse<ApiKey>> {
  const { data } = await apiClient.get<PaginatedResponse<ApiKey>>('/keys', {
    params: { page, page_size: pageSize, ...filters },
    signal: options?.signal
  })
  return data
}

/**
 * Get API key by ID
 * @param id - API key ID
 * @returns API key details
 */
export async function getById(id: number, options?: KeyRequestOptions): Promise<ApiKey> {
  const { data } = await apiClient.get<ApiKey>(`/keys/${id}`, { signal: options?.signal })
  return data
}

/**
 * Create new API key
 * @param name - Key name
 * @param groupId - Optional group ID
 * @param customKey - Optional custom key value
 * @param ipWhitelist - Optional IP whitelist
 * @param ipBlacklist - Optional IP blacklist
 * @param quota - Optional quota limit in USD (0 = unlimited)
 * @param expiresInDays - Optional days until expiry (undefined = never expires)
 * @param rateLimitData - Optional rate limit fields
 * @returns Created API key
 */
export async function create(
  name: string,
  groupId?: number | null,
  customKey?: string,
  ipWhitelist?: string[],
  ipBlacklist?: string[],
  quota?: number,
  expiresInDays?: number,
  rateLimitData?: { rate_limit_5h?: number; rate_limit_1d?: number; rate_limit_7d?: number },
  options?: KeyRequestOptions
): Promise<ApiKey> {
  requireKeyRequest(options)
  const payload: CreateApiKeyRequest = { name }
  if (groupId !== undefined) {
    payload.group_id = groupId
  }
  if (customKey) {
    payload.custom_key = customKey
  }
  if (ipWhitelist && ipWhitelist.length > 0) {
    payload.ip_whitelist = ipWhitelist
  }
  if (ipBlacklist && ipBlacklist.length > 0) {
    payload.ip_blacklist = ipBlacklist
  }
  if (quota !== undefined && quota > 0) {
    payload.quota = quota
  }
  if (expiresInDays !== undefined && expiresInDays > 0) {
    payload.expires_in_days = expiresInDays
  }
  if (rateLimitData?.rate_limit_5h && rateLimitData.rate_limit_5h > 0) {
    payload.rate_limit_5h = rateLimitData.rate_limit_5h
  }
  if (rateLimitData?.rate_limit_1d && rateLimitData.rate_limit_1d > 0) {
    payload.rate_limit_1d = rateLimitData.rate_limit_1d
  }
  if (rateLimitData?.rate_limit_7d && rateLimitData.rate_limit_7d > 0) {
    payload.rate_limit_7d = rateLimitData.rate_limit_7d
  }

  const { data } = await apiClient.post<ApiKey>('/keys', payload, { signal: options?.signal })
  requireKeyRequest(options)
  return data
}

/**
 * Update API key
 * @param id - API key ID
 * @param updates - Fields to update
 * @returns Updated API key
 */
export async function update(id: number, updates: UpdateApiKeyRequest, options?: KeyRequestOptions): Promise<ApiKey> {
  requireKeyRequest(options)
  const { data } = await apiClient.put<ApiKey>(`/keys/${id}`, updates, { signal: options?.signal })
  requireKeyRequest(options)
  return data
}

export interface BulkUpdateApiKeysResult {
  succeededIds: number[]
  failures: Array<{ id: number; error: unknown }>
}

/** Reuse per-key validation and permissions, with at most five requests in flight. */
export async function bulkUpdate(
  ids: number[],
  updates: UpdateApiKeyRequest,
  options?: KeyRequestOptions
): Promise<BulkUpdateApiKeysResult> {
  const capturedUpdates = { ...updates }
  if (updates.ip_whitelist) capturedUpdates.ip_whitelist = [...updates.ip_whitelist]
  if (updates.ip_blacklist) capturedUpdates.ip_blacklist = [...updates.ip_blacklist]
  const uniqueIds = [...new Set(ids)]
  const result: BulkUpdateApiKeysResult = { succeededIds: [], failures: [] }
  for (let offset = 0; offset < uniqueIds.length; offset += 5) {
    requireKeyRequest(options)
    const batch = uniqueIds.slice(offset, offset + 5)
    const responses = await Promise.allSettled(batch.map((id) => update(id, capturedUpdates, options)))
    responses.forEach((response, index) => {
      if (response.status === 'fulfilled') {
        result.succeededIds.push(batch[index])
      } else {
        result.failures.push({ id: batch[index], error: response.reason })
      }
    })
  }
  requireKeyRequest(options)
  return result
}

/**
 * Delete API key
 * @param id - API key ID
 * @returns Success confirmation
 */
export async function deleteKey(id: number, options?: KeyRequestOptions): Promise<{ message: string }> {
  requireKeyRequest(options)
  const { data } = await apiClient.delete<{ message: string }>(`/keys/${id}`, { signal: options?.signal })
  requireKeyRequest(options)
  return data
}

/**
 * Toggle API key status (active/inactive)
 * @param id - API key ID
 * @param status - New status
 * @returns Updated API key
 */
export async function toggleStatus(id: number, status: 'active' | 'inactive', options?: KeyRequestOptions): Promise<ApiKey> {
  return update(id, { status }, options)
}

export const keysAPI = {
  exportData,
  importData,
  list,
  getById,
  create,
  update,
  bulkUpdate,
  delete: deleteKey,
  toggleStatus
}

