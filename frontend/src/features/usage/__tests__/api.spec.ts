import { beforeEach, describe, expect, it, vi } from 'vitest'
import { usageAPI } from '../api/usage'
import { adminUsageAPI } from '../api/adminUsage'
const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: { get, post } }))
beforeEach(() => { get.mockReset(); post.mockReset(); get.mockResolvedValue({ data: {} }); post.mockResolvedValue({ data: {} }) })
describe('usage API keeps query and cleanup contracts', () => {
  it('keeps zero and false filters and forwards cancellation to the shared client', async () => {
    const signal = new AbortController().signal
    const params = { group_id: 0, billing_type: 0, native_compaction_v2: false, cursor: 'cursor', page: 2 }
    await usageAPI.query(params, { signal })
    expect(get).toHaveBeenCalledWith('/usage', { params, signal })
    await adminUsageAPI.list(params, { signal })
    expect(get).toHaveBeenLastCalledWith('/admin/usage', { params, signal })
  })
  it('forwards signals to auxiliary queries and does not change empty user ids into all users', async () => {
    const signal = new AbortController().signal
    await adminUsageAPI.searchApiKeys(0, '', { signal })
    expect(get).toHaveBeenCalledWith('/admin/usage/search-api-keys', { params: { user_id: 0 }, signal })
    await usageAPI.getDashboardAPIKeyBreakdown({ start_date: '2026-10-01', end_date: '2026-10-25' }, { signal })
    expect(get.mock.calls[1][1].signal).toBe(signal)
  })
  it('keeps cleanup an explicit server command and forwards its cancellation signal', async () => {
    const signal = new AbortController().signal
    const payload = { start_date: '2026-10-01', end_date: '2026-10-25', user_id: 0, stream: false }
    await adminUsageAPI.createCleanupTask(payload, { signal })
    expect(post).toHaveBeenCalledWith('/admin/usage/cleanup-tasks', payload, { signal })
    await adminUsageAPI.cancelCleanupTask(12, { signal })
    expect(post).toHaveBeenLastCalledWith('/admin/usage/cleanup-tasks/12/cancel', undefined, { signal })
  })
})
