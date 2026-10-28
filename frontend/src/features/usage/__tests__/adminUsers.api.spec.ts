import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getUserUsageStats, type AdminUserUsageStats } from '../adminUsers'

const http = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: http }))

beforeEach(() => vi.resetAllMocks())

describe('administrator user usage summary', () => {
  it('preserves the monthly default and recorded precision', async () => {
    const data: AdminUserUsageStats = { total_requests: 17, total_cost: 0.000000123, total_tokens: 531 }
    http.get.mockResolvedValue({ data })
    expect(await getUserUsageStats(7)).toBe(data)
    expect(http.get).toHaveBeenCalledTimes(1)
    expect(http.get).toHaveBeenCalledWith('/admin/users/7/usage', { params: { period: 'month' } })
  })

  it.each(['day', 'week', 'all', ''])('keeps explicit period %j unchanged', async period => {
    const data = { total_requests: 0, total_tokens: 0, total_cost: null }
    http.get.mockResolvedValue({ data })
    expect(await getUserUsageStats(7, period)).toBe(data)
    expect(http.get).toHaveBeenCalledTimes(1)
    expect(http.get).toHaveBeenCalledWith('/admin/users/7/usage', { params: { period } })
    expect(data.total_cost).toBeNull()
  })

  it('forwards cancellation and does not fill absent usage with zero', async () => {
    const controller = new AbortController()
    const data = { total_requests: 2 }
    http.get.mockResolvedValue({ data })
    const result = await getUserUsageStats(7, 'month', { signal: controller.signal })
    expect(result).toBe(data)
    expect(result).not.toHaveProperty('total_cost')
    expect(result).not.toHaveProperty('total_tokens')
    expect(http.get).toHaveBeenCalledTimes(1)
    expect(http.get).toHaveBeenCalledWith('/admin/users/7/usage', {
      params: { period: 'month' }, signal: controller.signal
    })
  })

  it('propagates errors without fabricating an unused account summary', async () => {
    const error = new Error('usage unavailable')
    http.get.mockRejectedValue(error)
    await expect(getUserUsageStats(7)).rejects.toBe(error)
    expect(http.get).toHaveBeenCalledTimes(1)
  })
})
