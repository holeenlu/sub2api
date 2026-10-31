import type { BasePaginationResponse } from '@/shared/http/requestTypes';
/**
 * Admin Announcements API endpoints
 */

import { apiClient } from '@/shared/http'
import type {
  Announcement,
  AnnouncementUserReadStatus,
  CreateAnnouncementRequest,
  UpdateAnnouncementRequest
} from '../model/announcement'

export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: {
    status?: string
    search?: string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
  },
  options?: {
    signal?: AbortSignal
  }
): Promise<BasePaginationResponse<Announcement>> {
  const { data } = await apiClient.get<BasePaginationResponse<Announcement>>('/admin/announcements', {
    params: { page, page_size: pageSize, ...filters },
    signal: options?.signal
  })
  return data
}

export async function getById(id: number, options?: { signal?: AbortSignal }): Promise<Announcement> {
  const { data } = await apiClient.get<Announcement>(`/admin/announcements/${id}`, { signal: options?.signal })
  return data
}

export async function create(request: CreateAnnouncementRequest, options?: { signal?: AbortSignal }): Promise<Announcement> {
  const { data } = await apiClient.post<Announcement>('/admin/announcements', request, { signal: options?.signal })
  return data
}

export async function update(id: number, request: UpdateAnnouncementRequest, options?: { signal?: AbortSignal }): Promise<Announcement> {
  const { data } = await apiClient.put<Announcement>(`/admin/announcements/${id}`, request, { signal: options?.signal })
  return data
}

export async function deleteAnnouncement(id: number, options?: { signal?: AbortSignal }): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/announcements/${id}`, { signal: options?.signal })
  return data
}

export async function getReadStatus(
  id: number,
  page: number = 1,
  pageSize: number = 20,
  filters?: {
    search?: string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
  },
  options?: {
    signal?: AbortSignal
  }
): Promise<BasePaginationResponse<AnnouncementUserReadStatus>> {
  const { data } = await apiClient.get<BasePaginationResponse<AnnouncementUserReadStatus>>(
    `/admin/announcements/${id}/read-status`,
    {
      params: { page, page_size: pageSize, ...filters },
      signal: options?.signal
    }
  )
  return data
}

export const adminAnnouncementsAPI = {
  list,
  getById,
  create,
  update,
  delete: deleteAnnouncement,
  getReadStatus
}

export default adminAnnouncementsAPI
