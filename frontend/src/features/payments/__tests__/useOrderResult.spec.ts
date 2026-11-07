import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { mountPaymentQuery, deferred } from './harness'
import { createPaymentsServices } from './services'
import { useOrderResult } from '../queries/useOrderResult'
import type { PublicOrderVerifyResult } from '../api/payment'
import type { PaymentOrder } from '../api/types'
const getOrder = vi.hoisted(() => vi.fn())
vi.mock('../api/payment', () => ({ paymentAPI: { getOrder, verifyOrder: vi.fn().mockRejectedValue(new Error('No private fixture')) } }))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.useFakeTimers(); getOrder.mockReset(); localStorage.clear() })
afterEach(() => vi.useRealTimers())
const publicOrder = (status = 'PENDING'): PublicOrderVerifyResult => ({ out_trade_no: 'order-1', status, paid: status === 'COMPLETED', created_at: '', expires_at: '' })
const target = { orderId: 1, resumeToken: 'opaque-1', outTradeNo: 'order-1', legacyReturn: false }
describe('order result scope', () => {
  it('keeps a public credential response independent of login changes', async () => {
    const response = deferred<PublicOrderVerifyResult>(), scope = ref<string | null>('user-a')
    const resolvePublicOrder = vi.fn().mockReturnValue(response.promise)
    const { query, services } = mountPaymentQuery(() => useOrderResult(() => target), createPaymentsServices({ scopeKey: scope, resolvePublicOrder }))
    const returned = { ...publicOrder('COMPLETED'), id: 1, user_id: 9 }
    scope.value = 'user-b'; response.resolve(returned); await flushPromises()
    scope.value = 'user-c'; await flushPromises()
    expect(query.order.value).toEqual(returned)
    expect(resolvePublicOrder).toHaveBeenCalledTimes(1); expect(getOrder).not.toHaveBeenCalled()
    expect(services.refreshUser).not.toHaveBeenCalled()
  })
  it('cancels private reads and discards the previous session response', async () => {
    const old = deferred<{ data: PaymentOrder }>(), scope = ref<string | null>('user-a')
    getOrder.mockReturnValueOnce(old.promise).mockResolvedValue({ data: { id: 1, user_id: 9, status: 'PENDING' } })
    const { query } = mountPaymentQuery(() => useOrderResult(() => ({ ...target, resumeToken: '', outTradeNo: '' })), createPaymentsServices({ scopeKey: scope }))
    const signal = getOrder.mock.calls[0][1].signal
    scope.value = null; old.resolve({ data: { id: 1, user_id: 9, status: 'COMPLETED' } as PaymentOrder }); await flushPromises()
    expect(signal.aborted).toBe(true); expect(query.order.value).toBeNull()
  })
  it('does not confirm a URL return when the server cannot resolve it', async () => {
    const resolvePublicOrder = vi.fn().mockRejectedValue(new Error('offline'))
    const { query, wrapper } = mountPaymentQuery(() => useOrderResult(() => ({ ...target, orderId: 0, resumeToken: '', legacyReturn: true })), createPaymentsServices({ resolvePublicOrder }))
    await flushPromises(); expect(query.order.value).toBeNull()
    expect(resolvePublicOrder).toHaveBeenCalledWith({ outTradeNo: 'order-1' }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    wrapper.unmount(); expect(vi.getTimerCount()).toBe(0)
  })
  it('aborts a pending public read on disposal and never schedules a late timer', async () => {
    const response = deferred<PublicOrderVerifyResult>(), resolvePublicOrder = vi.fn().mockReturnValue(response.promise)
    const { query, wrapper } = mountPaymentQuery(() => useOrderResult(() => target), createPaymentsServices({ resolvePublicOrder }))
    const signal = resolvePublicOrder.mock.calls[0][1].signal
    wrapper.unmount(); response.resolve(publicOrder('COMPLETED')); await flushPromises()
    expect(signal.aborted).toBe(true); expect(query.order.value).toBeNull(); expect(vi.getTimerCount()).toBe(0)
  })
  it('bounds pending status polling without repeating any payment command', async () => {
    const resolvePublicOrder = vi.fn().mockResolvedValue(publicOrder())
    const { wrapper } = mountPaymentQuery(() => useOrderResult(() => target), createPaymentsServices({ resolvePublicOrder }))
    await flushPromises(); await vi.advanceTimersByTimeAsync(60000)
    expect(resolvePublicOrder).toHaveBeenCalledTimes(16); expect(vi.getTimerCount()).toBe(0)
    wrapper.unmount()
  })
})
