import { describe, expect, it, vi } from 'vitest'
import { opsAPI } from '../api/ops'
import { auditAPI } from '../api/audit'
const get = vi.hoisted(() => vi.fn())
vi.mock('@/shared/http', () => ({ apiClient: { get } }))
describe('operations API shared consumers', () => {
  it('forwards the UsageServices cancellation signal without changing filters or server facts', async () => {
    const data = { items: [{ id: 9, request_id: 'operation-from-server', duration_ms: null }], total: 1 }
    get.mockResolvedValue({ data })
    const signal = new AbortController().signal
    const params = { page: 2, page_size: 20, user_id: 4, api_key_id: 7 }
    expect(await opsAPI.listErrorLogs(params, { signal })).toBe(data)
    expect(get).toHaveBeenCalledWith('/admin/ops/errors', { params, signal })
  })
})


describe('operations DTO boundaries', () => {
  it.each([undefined, null, 0, -1, 7])('keeps existing optional scope filtering for group %s', async (groupId) => {
    const data = { enabled: false, summary: null }
    get.mockResolvedValue({ data })
    const signal = new AbortController().signal
    const params = { platform: 'openai', ...(typeof groupId === 'number' && groupId > 0 ? { group_id: groupId } : {}) }
    expect(await opsAPI.getConcurrencyStats('openai', groupId, { signal })).toBe(data)
    expect(get).toHaveBeenLastCalledWith('/admin/ops/concurrency', { params, signal })
    expect(await opsAPI.getAccountAvailabilityStats('openai', groupId, { signal })).toBe(data)
    expect(get).toHaveBeenLastCalledWith('/admin/ops/account-availability', { params, signal })
    expect(await opsAPI.getRealtimeTrafficSummary('1min', 'openai', groupId, { signal })).toBe(data)
    expect(get).toHaveBeenLastCalledWith('/admin/ops/realtime-traffic', { params: { window: '1min', ...params }, signal })
  })

  it.each([false, true])('keeps correlated-upstream filters and include_detail wire value (%s)', async (includeDetail) => {
    const data = { items: [{ id: 2, upstream_status_code: 0, upstream_latency_ms: null }], total: 1 }
    get.mockResolvedValue({ data })
    const signal = new AbortController().signal
    const params = { page: 2, page_size: 10, group_id: 0, view: 'all' as const }
    expect(await opsAPI.listRequestErrorUpstreamErrors(1, params, { include_detail: includeDetail, signal })).toBe(data)
    expect(get).toHaveBeenLastCalledWith('/admin/ops/request-errors/1/upstream-errors', {
      params: { ...params, ...(includeDetail ? { include_detail: '1' } : {}) }, signal
    })
    expect(params).toEqual({ page: 2, page_size: 10, group_id: 0, view: 'all' })
  })

  it('preserves audit extension data, null, false and zero without projecting server facts', async () => {
    const data = { items: [{ id: 2, latency_ms: 0, extra: { pre_role_upgrade: false, future: { value: null, count: 0 } } }], total: 1 }
    get.mockResolvedValue({ data })
    const signal = new AbortController().signal
    const params = { page: 2, success: 'false' }
    expect(await auditAPI.list(params, { signal })).toBe(data)
    expect(get).toHaveBeenLastCalledWith('/admin/audit-logs', { params, signal })
  })

  it('preserves the original transport failure object at audit and ops boundaries', async () => {
    const failure = { code: 'ERR_CANCELED', response: { status: 403, data: { detail: 'source rejection' } } }
    get.mockRejectedValue(failure)
    await expect(auditAPI.get(1)).rejects.toBe(failure)
    await expect(opsAPI.getRealtimeTrafficSummary('1min')).rejects.toBe(failure)
  })
})
