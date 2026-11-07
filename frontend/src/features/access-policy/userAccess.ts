import { apiClient } from '@/shared/http'
import type { FetchOptions } from '@/shared/http/requestTypes'
import type {
  PlatformQuotaPlatform,
  PlatformQuotaWindow,
  PlatformQuotaUpdateItem,
  PlatformQuotasResponse
} from './api/platformQuotaTypes'

export interface ReplaceUserGroupResult {
  migrated_keys: number
}

// Replaces the user's exclusive group grant; Key migration is part of that command.
export async function replaceGroup(
  userId: number,
  oldGroupId: number,
  newGroupId: number,
  options?: FetchOptions
): Promise<ReplaceUserGroupResult> {
  const url = '/admin/users/' + userId + '/replace-group'
  const payload = { old_group_id: oldGroupId, new_group_id: newGroupId }
  const { data } = options
    ? await apiClient.post<ReplaceUserGroupResult>(url, payload, options)
    : await apiClient.post<ReplaceUserGroupResult>(url, payload)
  return data
}
export async function getPlatformQuotas(
  id: number,
  options?: FetchOptions
): Promise<PlatformQuotasResponse> {
  const url = `/admin/users/${id}/platform-quotas`
  const { data } = options
    ? await apiClient.get<PlatformQuotasResponse>(url, options)
    : await apiClient.get<PlatformQuotasResponse>(url)
  return data
}

// Full replacement: [] clears configuration; null, zero and positive limits differ.
export async function updatePlatformQuotas(
  id: number,
  quotas: PlatformQuotaUpdateItem[],
  options?: FetchOptions
): Promise<PlatformQuotasResponse> {
  const url = '/admin/users/' + id + '/platform-quotas'
  const payload = { quotas }
  const { data } = options
    ? await apiClient.put<PlatformQuotasResponse>(url, payload, options)
    : await apiClient.put<PlatformQuotasResponse>(url, payload)
  return data
}
export async function resetPlatformQuotaWindow(
  id: number,
  platform: PlatformQuotaPlatform,
  window: PlatformQuotaWindow,
  options?: FetchOptions
): Promise<PlatformQuotasResponse> {
  const url = '/admin/users/' + id + '/platform-quotas/reset'
  const payload = { platform, window }
  const { data } = options
    ? await apiClient.post<PlatformQuotasResponse>(url, payload, options)
    : await apiClient.post<PlatformQuotasResponse>(url, payload)
  return data
}