import { beforeEach, describe, expect, it, vi } from 'vitest'
import subscriptionsAPI from '../api/user'
const get = vi.hoisted(() => vi.fn())
vi.mock('@/shared/http', () => ({ apiClient: { get } }))
beforeEach(() => vi.clearAllMocks())
describe('subscription read DTO preservation', () => {
 it('passes cancellation through the original active endpoint and preserves zero values', async () => {
  const signal = new AbortController().signal
  const rows = [{ id: 1, daily_usage_usd: 0, expires_at: null }]
  get.mockResolvedValue({ data: rows })
  expect(await subscriptionsAPI.getActiveSubscriptions(signal)).toBe(rows)
  expect(get).toHaveBeenCalledWith('/subscriptions/active', { signal })
 })
 it('preserves absent progress windows and server-reported counts', async () => {
  const data = [{ subscription_id: 1, daily: null, weekly: null, monthly: null, expires_at: null, days_remaining: null }]
  get.mockResolvedValue({ data })
  expect(await subscriptionsAPI.getSubscriptionsProgress()).toBe(data)
  expect(get).toHaveBeenCalledWith('/subscriptions/progress', { signal: undefined })
 })
})
