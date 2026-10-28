import { beforeEach, describe, expect, it, vi } from 'vitest'
import { announcementsAPI } from '../announcements'
import { adminAnnouncementsAPI } from '../adminAnnouncements'
const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: http }))
beforeEach(() => { vi.resetAllMocks(); Object.values(http).forEach(method => method.mockResolvedValue({ data: { message: 'ok' } })) })
describe('announcement endpoint contract', () => {
  it('preserves authenticated user list/unread and read endpoints with cancellation', async () => {
    const signal = new AbortController().signal
    await announcementsAPI.list(false, { signal })
    expect(http.get).toHaveBeenLastCalledWith('/announcements', { params: {}, signal })
    await announcementsAPI.list(true, { signal })
    expect(http.get).toHaveBeenLastCalledWith('/announcements', { params: { unread_only: 1 }, signal })
    await announcementsAPI.markRead(7, { signal })
    expect(http.post).toHaveBeenCalledWith('/announcements/7/read', undefined, { signal })
  })
  it('preserves admin pagination, CRUD, audience payload, and read-status endpoints', async () => {
    const signal = new AbortController().signal
    const filters = { status: 'active', search: 'notice', sort_by: 'created_at', sort_order: 'desc' as const }
    await adminAnnouncementsAPI.list(2, 10, filters, { signal })
    expect(http.get).toHaveBeenLastCalledWith('/admin/announcements', { params: { page: 2, page_size: 10, ...filters }, signal })
    await adminAnnouncementsAPI.getById(7, { signal })
    expect(http.get).toHaveBeenLastCalledWith('/admin/announcements/7', { signal })
    const create = { title: 'Title', content: '', targeting: { any_of: [] } }
    await adminAnnouncementsAPI.create(create, { signal })
    expect(http.post).toHaveBeenCalledWith('/admin/announcements', create, { signal })
    await adminAnnouncementsAPI.update(7, { content: '', starts_at: 0 }, { signal })
    expect(http.put).toHaveBeenCalledWith('/admin/announcements/7', { content: '', starts_at: 0 }, { signal })
    await adminAnnouncementsAPI.delete(7, { signal })
    expect(http.delete).toHaveBeenCalledWith('/admin/announcements/7', { signal })
    await adminAnnouncementsAPI.getReadStatus(7, 2, 10, { search: 'reader' }, { signal })
    expect(http.get).toHaveBeenLastCalledWith('/admin/announcements/7/read-status', { params: { page: 2, page_size: 10, search: 'reader' }, signal })
  })
})
