import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getPlatformQuotas, replaceGroup, resetPlatformQuotaWindow, updatePlatformQuotas } from '../userAccess'
import type { PlatformQuotasResponse, PlatformQuotaUpdateItem } from '../api/platformQuotaTypes'

const http = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: http }))

beforeEach(() => vi.resetAllMocks())

function quotas(): PlatformQuotasResponse {
  return { platform_quotas: [{
    platform: 'server-registered-platform', daily_limit_usd: null, weekly_limit_usd: 0, monthly_limit_usd: 0.000000123,
    daily_usage_usd: 0.000001, weekly_usage_usd: 0, monthly_usage_usd: 0.000001,
    daily_window_start: null, weekly_window_start: '2026-10-19T00:00:00Z',
    daily_window_resets_at: null, weekly_window_resets_at: '2026-10-26T00:00:00Z'
  }] }
}

describe('user group replacement', () => {
  it('sends one atomic grant replacement, not per-Key binding requests', async () => {
    const data = { migrated_keys: 0 }
    http.post.mockResolvedValue({ data })
    expect(await replaceGroup(7, 11, 19)).toBe(data)
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(http.post).toHaveBeenCalledWith('/admin/users/7/replace-group', { old_group_id: 11, new_group_id: 19 })
    expect(http.put).not.toHaveBeenCalled()
    expect(http.get).not.toHaveBeenCalled()
  })

  it('passes the server group-eligibility failure through without a fallback command', async () => {
    const error = { code: 'GROUP_NOT_EXCLUSIVE', response: { status: 400 } }
    http.post.mockRejectedValue(error)
    await expect(replaceGroup(7, 11, 19)).rejects.toBe(error)
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(http.put).not.toHaveBeenCalled()
  })

  it('passes the same caller signal with the atomic replacement payload', async () => {
    const options = { signal: new AbortController().signal }
    const data = { migrated_keys: 3 }
    http.post.mockResolvedValueOnce({ data })
    await expect(replaceGroup(7, 11, 19, options)).resolves.toBe(data)
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(http.post).toHaveBeenCalledWith('/admin/users/7/replace-group', {
      old_group_id: 11, new_group_id: 19
    }, options)
  })
})

describe('configured user platform quotas', () => {
  it('keeps every quota, raw amount, null and optional window field', async () => {
    const data = quotas()
    http.get.mockResolvedValue({ data })
    expect(await getPlatformQuotas(7)).toBe(data)
    expect(http.get).toHaveBeenCalledTimes(1)
    expect(http.get).toHaveBeenCalledWith('/admin/users/7/platform-quotas')
    expect(data.platform_quotas[0]).not.toHaveProperty('monthly_window_start')
    expect(data.platform_quotas[0].daily_window_start).toBeNull()
  })

  it('forwards the caller signal without switching to anonymous policy', async () => {
    const controller = new AbortController()
    http.get.mockResolvedValue({ data: quotas() })
    await getPlatformQuotas(7, { signal: controller.signal })
    expect(http.get).toHaveBeenCalledTimes(1)
    expect(http.get).toHaveBeenCalledWith('/admin/users/7/platform-quotas', { signal: controller.signal })
    controller.abort()
    expect(http.get.mock.calls[0][1].signal.aborted).toBe(true)
  })

  it('preserves null=unlimited, zero=disabled and positive fractional limits in a full replacement', async () => {
    const data = quotas()
    const replacement: PlatformQuotaUpdateItem[] = [{
      platform: 'server-registered-platform', daily_limit_usd: null, weekly_limit_usd: 0, monthly_limit_usd: 0.000000123
    }]
    http.put.mockResolvedValue({ data })
    expect(await updatePlatformQuotas(7, replacement)).toBe(data)
    expect(http.put).toHaveBeenCalledTimes(1)
    expect(http.put).toHaveBeenCalledWith('/admin/users/7/platform-quotas', { quotas: replacement })
    expect(http.put.mock.calls[0][1].quotas).toBe(replacement)
  })

  it('retains the meaningful empty replacement instead of skipping the command', async () => {
    const data = { platform_quotas: [] }
    http.put.mockResolvedValue({ data })
    expect(await updatePlatformQuotas(7, [])).toBe(data)
    expect(http.put).toHaveBeenCalledTimes(1)
    expect(http.put).toHaveBeenCalledWith('/admin/users/7/platform-quotas', { quotas: [] })
  })

  it('forwards cancellation with a full quota replacement without changing limit meanings', async () => {
    const options = { signal: new AbortController().signal }
    const replacement: PlatformQuotaUpdateItem[] = [{ platform: 'openai', daily_limit_usd: null, weekly_limit_usd: 0, monthly_limit_usd: 0.000000123 }]
    const data = quotas()
    http.put.mockResolvedValueOnce({ data })
    await expect(updatePlatformQuotas(7, replacement, options)).resolves.toBe(data)
    expect(http.put).toHaveBeenCalledTimes(1)
    expect(http.put).toHaveBeenCalledWith('/admin/users/7/platform-quotas', { quotas: replacement }, options)
    expect(http.put.mock.calls[0][1].quotas).toBe(replacement)
  })

  it.each(['daily', 'weekly', 'monthly'] as const)('forwards cancellation with exactly the requested %s reset', async window => {
    const options = { signal: new AbortController().signal }
    const data = quotas()
    http.post.mockResolvedValueOnce({ data })
    await expect(resetPlatformQuotaWindow(7, 'openai', window, options)).resolves.toBe(data)
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(http.post).toHaveBeenCalledWith('/admin/users/7/platform-quotas/reset', { platform: 'openai', window }, options)
  })

  it.each(['daily', 'weekly', 'monthly'] as const)('resets exactly the requested %s window and platform', async window => {
    const data = quotas()
    http.post.mockResolvedValue({ data })
    expect(await resetPlatformQuotaWindow(7, 'server-registered-platform', window)).toBe(data)
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(http.post).toHaveBeenCalledWith('/admin/users/7/platform-quotas/reset', {
      platform: 'server-registered-platform', window
    })
  })

  it('does not turn a denied quota read into unlimited access', async () => {
    const error = { response: { status: 403 } }
    http.get.mockRejectedValue(error)
    await expect(getPlatformQuotas(7)).rejects.toBe(error)
    expect(http.get).toHaveBeenCalledTimes(1)
  })

  it.each(['replaceGroup', 'updatePlatformQuotas', 'resetPlatformQuotaWindow'] as const)('does not retry %s after an uncertain outcome', async command => {
    const error = new Error('transport outcome unknown')
    http.put.mockRejectedValue(error)
    http.post.mockRejectedValue(error)
    const mutation = command === 'replaceGroup' ? replaceGroup(7, 11, 19)
      : command === 'updatePlatformQuotas' ? updatePlatformQuotas(7, [])
      : resetPlatformQuotaWindow(7, 'openai', 'daily')
    await expect(mutation).rejects.toBe(error)
    expect(http.put.mock.calls.length + http.post.mock.calls.length).toBe(1)
    expect(http.get).not.toHaveBeenCalled()
  })
})
