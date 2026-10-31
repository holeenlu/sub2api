import { apiClient } from '@/shared/http'
import type { PlatformQuotasResponse } from '@/features/access-policy/api/platformQuotaTypes';
export async function getMyPlatformQuotas(options?: { signal?: AbortSignal }): Promise<PlatformQuotasResponse> {
  const { data } = await apiClient.get<PlatformQuotasResponse>('/user/platform-quotas', { signal: options?.signal })
  return data
}
export const userUsageAPI = { getMyPlatformQuotas }
