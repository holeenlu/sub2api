import { defineComponent, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { usageServicesKey } from '../services'
import { createUsageTestServices } from './services'
import { useUserDashboardData } from '../queries/useUserDashboardData'
import { useDistributionBreakdown } from '../queries/useDistributionBreakdown'
const api = vi.hoisted(() => ({ getDashboardStats: vi.fn(), getDashboardTrend: vi.fn(), getDashboardModels: vi.fn(), getByDateRange: vi.fn(), getMyPlatformQuotas: vi.fn(), getUserBreakdown: vi.fn() }))
vi.mock('../api/usage', () => ({ usageAPI: api }))
vi.mock('../api/userRead', () => ({ getMyPlatformQuotas: api.getMyPlatformQuotas }))
vi.mock('../adminDashboard', () => ({ getUserBreakdown: api.getUserBreakdown }))
enableAutoUnmount(afterEach)
beforeEach(() => vi.resetAllMocks())
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }
function open<T>(setup: () => T, scope = ref<string | null>('session-a')) {
  let query!: T
  const wrapper = mount(defineComponent({ setup() { query = setup(); return () => null } }), { global: { provide: { [usageServicesKey as symbol]: createUsageTestServices({ scopeKey: scope }) } } })
  return { query, wrapper, scope }
}
describe('actual user dashboard query owner', () => {
  it('shares the real successful loading lifecycle and supports refresh', async () => {
    const pending = deferred<unknown>(); api.getDashboardStats.mockReturnValueOnce(pending.promise).mockResolvedValueOnce({ today_requests: 5 })
    const { query } = open(useUserDashboardData), first = query.loadStats()
    expect(query.loading.value).toBe(true)
    pending.resolve({ today_requests: 3 }); await first
    expect(query.loading.value).toBe(false); expect(query.stats.value?.today_requests).toBe(3)
    await query.loadStats(); expect(query.stats.value?.today_requests).toBe(5); expect(api.getDashboardStats).toHaveBeenCalledTimes(2)
  })
  it('does not turn failure or an empty response into zero statistics', async () => {
    api.getDashboardStats.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(null)
    const { query } = open(useUserDashboardData)
    await query.loadStats(); expect(query.stats.value).toBeNull(); expect(query.loading.value).toBe(false)
    await query.loadStats(); expect(query.stats.value).toBeNull()
  })
  it('keeps the new session loading state when an old response arrives', async () => {
    const first = deferred<unknown>(), second = deferred<unknown>()
    api.getDashboardStats.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const { query, scope } = open(useUserDashboardData), old = query.loadStats(), signal = api.getDashboardStats.mock.calls[0][0].signal
    scope.value = 'session-b'; const current = query.loadStats()
    first.resolve({ today_requests: 100 }); await old
    expect(signal.aborted).toBe(true); expect(query.stats.value).toBeNull(); expect(query.loading.value).toBe(true)
    second.resolve({ today_requests: 1 }); await current; expect(query.stats.value?.today_requests).toBe(1)
  })
  it('retains the original 100-row recent-read adapter and exposes only five rows', async () => {
    api.getByDateRange.mockResolvedValue({ items: Array.from({ length: 8 }, (_, id) => ({ id })) })
    const { query } = open(useUserDashboardData); await query.loadRecent()
    expect(api.getByDateRange).toHaveBeenCalledWith(query.startDate.value, query.endDate.value, undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(query.recentUsage.value).toHaveLength(5)
  })
  it('keeps failed quota reads unknown and aborts active work on disposal', async () => {
    api.getMyPlatformQuotas.mockRejectedValue(new Error('offline'))
    const pending = deferred<unknown>(); api.getDashboardStats.mockReturnValue(pending.promise)
    const { query, wrapper } = open(useUserDashboardData); await query.loadPlatformQuotas(); expect(query.platformQuotas.value).toBeNull()
    const loading = query.loadStats(), signal = api.getDashboardStats.mock.calls[0][0].signal
    wrapper.unmount(); pending.resolve({ today_requests: 10 }); await loading
    expect(signal.aborted).toBe(true); expect(query.stats.value).toBeNull()
  })
})
describe('distribution drill-down query owner', () => {
  it('drops a late expanded row after changing the selected row or filters', async () => {
    const old = deferred<{ users: Array<{ user_id: number }> }>()
    api.getUserBreakdown.mockReturnValueOnce(old.promise).mockResolvedValueOnce({ users: [{ user_id: 2 }] })
    const start = ref('2026-10-24'), { query } = open(() => useDistributionBreakdown(() => ({ start_date: start.value }), () => true))
    const first = query.toggle('model-a', { model: 'a' }), signal = api.getUserBreakdown.mock.calls[0][1].signal
    await query.toggle('model-b', { model: 'b' }); old.resolve({ users: [{ user_id: 1 }] }); await first
    expect(signal.aborted).toBe(true); expect(query.breakdownItems.value[0].user_id).toBe(2)
    start.value = '2026-10-25'; await flushPromises(); expect(query.expandedKey.value).toBeNull(); expect(query.breakdownItems.value).toEqual([])
  })
  it('does not query disabled breakdowns and cancels on collapsing the selected row', async () => {
    const enabled = ref(false), pending = deferred<{ users: Array<{ user_id: number }> }>()
    api.getUserBreakdown.mockReturnValue(pending.promise)
    const { query } = open(() => useDistributionBreakdown(() => ({}), () => enabled.value))
    await query.toggle('row', {}); expect(api.getUserBreakdown).not.toHaveBeenCalled()
    enabled.value = true; const running = query.toggle('row', {}), signal = api.getUserBreakdown.mock.calls[0][1].signal
    await query.toggle('row', {}); pending.resolve({ users: [{ user_id: 1 }] }); await running
    expect(signal.aborted).toBe(true); expect(query.breakdownItems.value).toEqual([]); expect(query.breakdownLoading.value).toBe(false)
  })
})
