import { getBalanceHistory } from '../api/balanceHistory'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/shared/http', () => ({
  apiClient: {
    get,
    post,
  },
}))

import { paymentAPI } from '@/features/payments/api/payment'

describe('payment api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: {} })
  })

  it('keeps legacy public out_trade_no verification for upgrade compatibility', async () => {
    await paymentAPI.verifyOrderPublic('legacy-order-no')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/verify', {
      out_trade_no: 'legacy-order-no',
    }, { sessionPolicy: 'anonymous', signal: undefined })
  })

  it('keeps signed public resume-token resolve endpoint', async () => {
    await paymentAPI.resolveOrderPublicByResumeToken('resume-token-123')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/resolve', {
      resume_token: 'resume-token-123',
    }, { sessionPolicy: 'anonymous', signal: undefined })
  })
  it('keeps cancellation and anonymous policy on both public endpoints', async () => {
    const controller = new AbortController()
    await paymentAPI.verifyOrderPublic('legacy-order', { signal: controller.signal })
    await paymentAPI.resolveOrderPublicByResumeToken('signed-token', { signal: controller.signal })
    expect(post).toHaveBeenNthCalledWith(1, '/payment/public/orders/verify', { out_trade_no: 'legacy-order' }, { sessionPolicy: 'anonymous', signal: controller.signal })
    expect(post).toHaveBeenNthCalledWith(2, '/payment/public/orders/resolve', { resume_token: 'signed-token' }, { sessionPolicy: 'anonymous', signal: controller.signal })
  })
  it('does not mark private order queries anonymous', async () => {
    const controller = new AbortController()
    await paymentAPI.getOrder(7, { signal: controller.signal })
    expect(get).toHaveBeenCalledWith('/payment/orders/7', { signal: controller.signal })
  })

  it('retains all historical balance read filters and returns the original ledger data', async () => {
    const signal = new AbortController().signal
    const data = { items: [{ id: 'opaque-ledger-id', type: 'affiliate_balance', source: 'affiliate', value: -5, notes: 'history', occurred_at: '2026-10-25T00:00:00Z' }], total: 31, total_recharged: 100 }
    get.mockResolvedValueOnce({ data })
    const params = { category: 'other' as const, direction: 'decrease' as const, source: 'affiliate' as const, page: 2, page_size: 15 }
    expect(await getBalanceHistory(params, { signal })).toBe(data)
    expect(get).toHaveBeenCalledWith('/user/balance-history', { params, signal })
    expect(post).not.toHaveBeenCalled()
  })

})
