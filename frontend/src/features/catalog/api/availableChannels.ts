import { apiClient } from '@/shared/http'
import type { UserAvailableChannel } from './types'

/** Current user's channel, group and model-pricing projection. */
export async function getAvailableChannels(options?: { signal?: AbortSignal }): Promise<UserAvailableChannel[]> {
  const { data } = await apiClient.get<UserAvailableChannel[]>('/channels/available', {
    signal: options?.signal
  })
  return data
}

export const userChannelsAPI = { getAvailable: getAvailableChannels }
