import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/shared/http'
import { getSettings, getWebSearchEmulationConfig, updateSettings } from '../api/settings'
vi.mock('@/shared/http', () => ({ apiClient: { get: vi.fn(), put: vi.fn() } }))
beforeEach(() => vi.clearAllMocks())
describe('settings control API', () => {
  it('forwards cancellation for both settings reads', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: {} })
    const options = { signal: new AbortController().signal }
    await getWebSearchEmulationConfig(options)
    expect(apiClient.get).toHaveBeenCalledWith('/admin/settings/web-search-emulation', options)
    await getSettings(options)
    expect(apiClient.get).toHaveBeenCalledWith('/admin/settings', options)
  })
  it('passes sparse updates to PUT without defaults or extra requests', async () => {
    vi.mocked(apiClient.put).mockResolvedValue({ data: { revision: 'b'.repeat(64) } })
    const patch = { revision: 'a'.repeat(64), payment_enabled: false, default_balance: 0, site_name: '' }
    await updateSettings(patch)
    expect(apiClient.put).toHaveBeenCalledTimes(1)
    expect(apiClient.put).toHaveBeenCalledWith('/admin/settings', patch, undefined)
    expect(apiClient.get).not.toHaveBeenCalled()
  })
  it('forwards the caller AbortSignal to PUT without copying or replacing it', async () => {
    vi.mocked(apiClient.put).mockResolvedValue({ data: { revision: 'b'.repeat(64) } })
    const controller = new AbortController()
    const options = { signal: controller.signal }
    const patch = { revision: 'a'.repeat(64), site_name: 'Saved' }
    await updateSettings(patch, options)
    expect(apiClient.put).toHaveBeenCalledTimes(1)
    expect(apiClient.put).toHaveBeenCalledWith('/admin/settings', patch, options)
    expect(vi.mocked(apiClient.put).mock.calls[0][2]?.signal).toBe(controller.signal)
    controller.abort()
    expect(vi.mocked(apiClient.put).mock.calls[0][2]?.signal?.aborted).toBe(true)
    expect(apiClient.get).not.toHaveBeenCalled()
  })
})
