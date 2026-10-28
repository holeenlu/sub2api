/**
 * User Announcements API endpoints
 */

import { apiClient } from '@/shared/http'
import type { UserAnnouncement } from '../model/announcement'

export async function list(unreadOnly: boolean = false, options?: { signal?: AbortSignal }): Promise<UserAnnouncement[]> {
  const { data } = await apiClient.get<UserAnnouncement[]>('/announcements', {
    params: unreadOnly ? { unread_only: 1 } : {},
    signal: options?.signal
  })
  return data
}

export async function markRead(id: number, options?: { signal?: AbortSignal }): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>(`/announcements/${id}/read`, undefined, { signal: options?.signal })
  return data
}

export const announcementsAPI = {
  list,
  markRead
}

export default announcementsAPI

