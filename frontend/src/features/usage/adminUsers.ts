import { apiClient } from '@/shared/http'
import type { FetchOptions } from '@/shared/http/requestTypes'
import type { UsageStatsResponse } from './api/types'

export type AdminUserUsageStats = Pick<
  UsageStatsResponse,
  'total_requests' | 'total_cost' | 'total_tokens'
>

export async function getUserUsageStats(
  id: number,
  period: string = 'month',
  options?: FetchOptions
): Promise<AdminUserUsageStats> {
  const { data } = await apiClient.get<AdminUserUsageStats>(`/admin/users/${id}/usage`, {
    params: { period },
    ...options
  })
  return data
}
