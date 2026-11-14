import { apiClient } from '@/shared/http'
import type { PaginatedResponse, FetchOptions } from '@/shared/http/requestTypes'
import type { ApiKey } from './api/types'

export async function getUserApiKeys(
  id: number,
  options?: FetchOptions
): Promise<PaginatedResponse<ApiKey>> {
  const url = `/admin/users/${id}/api-keys`
  const { data } = options
    ? await apiClient.get<PaginatedResponse<ApiKey>>(url, options)
    : await apiClient.get<PaginatedResponse<ApiKey>>(url)
  return data
}
