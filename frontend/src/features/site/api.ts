import { apiClient, type HttpRequestConfig } from '@/shared/http'
import type { PublicSettings } from '@/features/site/api/publicSettingsTypes';

/** Public IP-limited endpoint; the server has no actor-dependent projection here. */
export async function getPublicSettings(options?: { signal?: AbortSignal }): Promise<PublicSettings> {
  const config: HttpRequestConfig = { ...options, sessionPolicy: 'anonymous' }
  const { data } = await apiClient.get<PublicSettings>('/settings/public', config)
  return data
}
