import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { useCheckoutOrder } from '../commands/useCheckoutOrder'
import { mountPaymentQuery, deferred } from './harness'
import { createPaymentsServices } from './services'
import type { CheckoutInfoResponse, CreateOrderResult } from '../api/types'
const { createOrder, push } = vi.hoisted(() => ({ createOrder: vi.fn(), push: vi.fn() }))
vi.mock('../api/payment', () => ({ paymentAPI: { createOrder } }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push, resolve: () => ({ href: '/payment/stripe' }) }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/shared/lib/device', () => ({ isMobileDevice: () => true }))
enableAutoUnmount(afterEach)
beforeEach(() => { createOrder.mockReset(); push.mockReset(); localStorage.clear() })
function setup(scope = ref<string | null>('user-9')) {
  return mountPaymentQuery(() => useCheckoutOrder({ checkout: ref({} as CheckoutInfoResponse), selectedMethod: ref('wxpay'), selectedPlan: ref(null) }), createPaymentsServices({ scopeKey: scope }))
}
const knownOrder: CreateOrderResult = { order_id: 1, amount: 10, pay_amount: 10, fee_rate: 0, expires_at: '2099-01-01T00:00:00Z', payment_type: 'wxpay', qr_code: 'weixin://existing', out_trade_no: 'trade-1' }
describe('checkout mutation ownership', () => {
  it('does not resend an uncertain create command', async () => {
    createOrder.mockRejectedValue(new Error('connection lost'))
    const { query } = setup(); await query.createOrder(10, 'balance'); await query.createOrder(10, 'balance')
    expect(createOrder).toHaveBeenCalledTimes(1); expect(query.submissionUnknown.value).toBe(true)
  })
  it('allows only the explicit merchant rejection desktop QR fallback', async () => {
    createOrder.mockRejectedValueOnce({ reason: 'WECHAT_H5_NOT_AUTHORIZED' }).mockResolvedValueOnce({ data: knownOrder })
    const { query } = setup(); await query.createOrder(10, 'balance')
    expect(createOrder).toHaveBeenCalledTimes(2)
    expect(createOrder.mock.calls[1][0].is_mobile).toBe(false); expect(query.paymentState.value.orderId).toBe(1)
  })
  it('locks the flow when the fallback result is unknown', async () => {
    createOrder.mockRejectedValueOnce({ reason: 'WECHAT_H5_NOT_AUTHORIZED' }).mockRejectedValueOnce(new Error('connection lost'))
    const { query } = setup(); await query.createOrder(10, 'balance'); await query.createOrder(10, 'balance')
    expect(query.submissionUnknown.value).toBe(true); expect(createOrder).toHaveBeenCalledTimes(2)
  })
  it('cancels and ignores an order result after switching session', async () => {
    const response = deferred<{ data: CreateOrderResult }>(), scope = ref<string | null>('user-9')
    createOrder.mockReturnValue(response.promise)
    const { query, services } = setup(scope); const pending = query.createOrder(10, 'balance')
    const signal = createOrder.mock.calls[0][1].signal
    scope.value = null; response.resolve({ data: knownOrder }); await pending; await flushPromises()
    expect(signal.aborted).toBe(true); expect(query.paymentState.value.orderId).toBe(0)
    expect(localStorage.length).toBe(0); expect(services.notifyError).not.toHaveBeenCalled(); expect(push).not.toHaveBeenCalled()
  })
})
