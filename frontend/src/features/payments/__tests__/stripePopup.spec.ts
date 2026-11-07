import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { mountPaymentQuery, deferred } from './harness'
import { createPaymentsServices } from './services'
import { useStripePopup } from '../commands/useStripePopup'
const mocks = vi.hoisted(() => ({ getOrder: vi.fn(), loadStripe: vi.fn(), confirmWechatPayPayment: vi.fn(), confirmAlipayPayment: vi.fn() }))
vi.mock('../api/payment', () => ({ paymentAPI: { getOrder: mocks.getOrder } }))
vi.mock('@stripe/stripe-js/pure', () => ({ loadStripe: mocks.loadStripe }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
const originalOpener = Object.getOwnPropertyDescriptor(window, 'opener')
beforeEach(() => {
  vi.useFakeTimers(); vi.resetAllMocks()
  Object.defineProperty(window, 'opener', { configurable: true, value: window })
  vi.spyOn(window, 'postMessage').mockImplementation(() => {})
  mocks.getOrder.mockResolvedValue({ data: { id: 7, user_id: 9, status: 'PENDING', pay_amount: 8, currency: 'CNY' } })
  mocks.loadStripe.mockResolvedValue({ confirmWechatPayPayment: mocks.confirmWechatPayPayment, confirmAlipayPayment: mocks.confirmAlipayPayment })
  mocks.confirmWechatPayPayment.mockResolvedValue({ paymentIntent: { status: 'succeeded' } })
})
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers(); if (originalOpener) Object.defineProperty(window, 'opener', originalOpener); else Reflect.deleteProperty(window, 'opener') })
function initialize(source: MessageEventSource | null = window) {
  window.dispatchEvent(new MessageEvent('message', { origin: window.location.origin, source, data: { type: 'STRIPE_POPUP_INIT', clientSecret: 'secret', publishableKey: 'pk_test' } }))
}
describe('Stripe popup protocol', () => {
  it.each(['response', 'exception'])('queries the order after an uncertain SDK %s without replaying confirmation', async outcome => {
    if (outcome === 'response') mocks.confirmWechatPayPayment.mockResolvedValueOnce({ error: { type: 'api_error', message: 'uncertain' } })
    else mocks.confirmWechatPayPayment.mockRejectedValueOnce(new Error('network outcome unknown'))
    const { query } = mountPaymentQuery(() => useStripePopup(() => ({ orderId: 7, method: 'wechat_pay' })))
    initialize(); await flushPromises(); await flushPromises()
    expect(mocks.getOrder.mock.calls.length).toBeGreaterThanOrEqual(2)
    await vi.advanceTimersByTimeAsync(9000)
    expect(mocks.confirmWechatPayPayment).toHaveBeenCalledTimes(1)
    expect(query.success.value).toBe(false)
  })

  it.each(['CANCELLED', 'EXPIRED', 'FAILED', 'REFUNDED', 'FUTURE_PROCESSING'])('does not submit a popup payment for %s', async status => {
    mocks.getOrder.mockResolvedValueOnce({ data: { id: 7, user_id: 9, status, pay_amount: 8, currency: 'CNY' } })
    const { query } = mountPaymentQuery(() => useStripePopup(() => ({ orderId: 7, method: 'wechat_pay' })))
    initialize(); await flushPromises()
    expect(mocks.loadStripe).not.toHaveBeenCalled()
    expect(mocks.confirmWechatPayPayment).not.toHaveBeenCalled()
    expect(query.success.value).toBe(false)
  })

  it('accepts initialization only once and from its same-origin opener', async () => {
    const { query } = mountPaymentQuery(() => useStripePopup(() => ({ orderId: 7, method: 'wechat_pay' })))
    initialize(null); await flushPromises(); expect(mocks.getOrder).not.toHaveBeenCalled()
    initialize(); initialize(); await flushPromises(); await flushPromises()
    expect(mocks.confirmWechatPayPayment).toHaveBeenCalledTimes(1)
    expect(query.success.value).toBe(false)
    expect(window.postMessage).toHaveBeenCalledWith({ type: 'STRIPE_POPUP_READY' }, window.location.origin)
  })
  it('does not invoke the SDK when the session changes during order verification', async () => {
    const response = deferred<{ data: { id: number; user_id: number; status: string } }>(), scope = ref<string | null>('session-a')
    mocks.getOrder.mockReturnValueOnce(response.promise)
    const { wrapper } = mountPaymentQuery(() => useStripePopup(() => ({ orderId: 7, method: 'wechat_pay' })), createPaymentsServices({ scopeKey: scope }))
    initialize(); const signal = mocks.getOrder.mock.calls[0][1].signal
    scope.value = 'session-b'; response.resolve({ data: { id: 7, user_id: 9, status: 'PENDING' } }); await flushPromises()
    expect(signal.aborted).toBe(true); expect(mocks.loadStripe).not.toHaveBeenCalled()
    wrapper.unmount(); expect(vi.getTimerCount()).toBe(0)
  })
})
