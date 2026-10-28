/**
 * Admin API Keys API endpoints
 * Handles API key management for administrators
 */

import { apiClient } from '@/shared/http'
import type { FetchOptions } from '@/shared/http/requestTypes'
import type { ApiKey } from './api/types'

export interface UpdateApiKeyGroupResult {
  api_key: ApiKey
  auto_granted_group_access: boolean
  granted_group_id?: number
  granted_group_name?: string
}

/**
 * Update an API key's group binding
 * @param id - API Key ID
 * @param groupId - Group ID (0 to unbind, positive to bind, null/undefined to skip)
 * @returns Updated API key with auto-grant info
 */
export async function updateApiKeyGroup(id: number, groupId: number | null, options?: FetchOptions): Promise<UpdateApiKeyGroupResult> {
  const url = '/admin/api-keys/' + id
  const payload = { group_id: groupId === null ? 0 : groupId }
  const { data } = options
    ? await apiClient.put<UpdateApiKeyGroupResult>(url, payload, options)
    : await apiClient.put<UpdateApiKeyGroupResult>(url, payload)
  return data
}
export const adminKeysAPI = {
  updateApiKeyGroup
}

export default adminKeysAPI
