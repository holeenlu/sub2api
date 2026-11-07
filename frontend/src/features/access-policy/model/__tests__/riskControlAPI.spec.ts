import { beforeEach, describe, expect, it, vi } from 'vitest'
import { riskControlAPI } from '../../api/riskControl'
const { get, put, post, remove } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn(), remove: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: { get, put, post, delete: remove } }))
beforeEach(() => { vi.clearAllMocks(); get.mockResolvedValue({ data: {} }); put.mockResolvedValue({ data: {} }); remove.mockResolvedValue({ data: {} }) })
describe('the existing moderation REST contract', () => {
 it('keeps the config update as one PUT with false, zero, empty values and direct-proxy semantics', async () => {
  const payload = { enabled: false, sample_rate: 0, proxy_id: 0, group_ids: [], block_message: '', clear_api_key: true }
  await riskControlAPI.updateConfig(payload)
  expect(put).toHaveBeenCalledTimes(1); expect(put).toHaveBeenCalledWith('/admin/risk-control/config', payload)
  expect(post).not.toHaveBeenCalled(); expect(payload).not.toHaveProperty('revision')
 })
 it('cancels reads through shared HTTP while retaining endpoints', async () => {
  const signal = new AbortController().signal
  await riskControlAPI.getConfig(signal); await riskControlAPI.getStatus(signal); await riskControlAPI.listLogs({ page: 2 }, signal)
  expect(get).toHaveBeenCalledWith('/admin/risk-control/config', { signal }); expect(get).toHaveBeenCalledWith('/admin/risk-control/status', { signal }); expect(get).toHaveBeenCalledWith('/admin/risk-control/logs', { params: { page: 2 }, signal })
 })
})
