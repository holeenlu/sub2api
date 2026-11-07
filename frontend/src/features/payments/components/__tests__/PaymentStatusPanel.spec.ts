import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const pollOrderStatus = vi.hoisted(() => vi.fn())
const cancelOrder = vi.hoisted(() => vi.fn())
const verifyOrder = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const toCanvas = vi.hoisted(() => vi.fn())

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

vi.mock('@/features/payments/api/payment', () => ({ paymentAPI: {
  getOrder: (id: number, options: unknown) => pollOrderStatus(id, options).then(data => ({ data })), cancelOrder, verifyOrder,
} }))

import { paymentsServicesKey } from '../../services'
import { createPaymentsServices } from '../../__tests__/services'
vi.mock('qrcode', () => ({
  default: {
    toCanvas,
  },
}))

import PaymentStatusPanel from '../PaymentStatusPanel.vue'

const orderFactory = (status: string) => ({
  id: 42,
  user_id: 9,
  amount: 88,
  pay_amount: 88,
  fee_rate: 0,
  payment_type: 'alipay',
  out_trade_no: 'sub2_20260420abcd1234',
  status,
  order_type: 'balance',
  created_at: '2026-04-20T12:00:00Z',
  expires_at: '2099-01-01T12:30:00Z',
  refund_amount: 0,
})

describe('PaymentStatusPanel', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    pollOrderStatus.mockReset()
    cancelOrder.mockReset()
    verifyOrder.mockReset()
    showError.mockReset()
    toCanvas.mockReset().mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('treats RECHARGING as a successful terminal state', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('RECHARGING'))

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
      },
      global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ notifyError: showError }) },
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('shows reopen button in QR mode when payUrl is also available', async () => {
    const openSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as Window)

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        payUrl: 'https://pay.example.com/session/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
      },
      global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ notifyError: showError }) },
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    expect(wrapper.text()).toContain('payment.qr.openPayWindow')

    await wrapper.get('button.btn.btn-secondary.text-sm').trigger('click')
    expect(openSpy).toHaveBeenCalledWith(
      'https://pay.example.com/session/42',
      'paymentPopup',
      expect.any(String),
    )

    openSpy.mockRestore()
  })

  it('uses generic QR copy for custom methods that contain built-in names', async () => {
    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'card_alipay',
        orderType: 'balance',
      },
      global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ notifyError: showError }) },
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('payment.qr.scanToPay')
    expect(wrapper.text()).not.toContain('payment.qr.scanAlipay')
  })

  it('actively verifies a stuck pending order and settles it when upstream confirms payment', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({
      data: orderFactory('COMPLETED'),
    })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'wxpay',
        orderType: 'balance',
      },
      global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ notifyError: showError }) },
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('actively verifies a pending mobile Alipay precreate order', async () => {
    const originalLocation = window.location
    const originalHidden = Object.getOwnPropertyDescriptor(document, 'hidden')
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { assign: vi.fn() },
    })
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => false,
    })
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({ data: orderFactory('COMPLETED') })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/dynamic-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
        mobileAlipayDeepLink: true,
      },
      global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ notifyError: showError }) }, stubs: { Icon: true } },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.emitted('success')).toHaveLength(1)

    wrapper.unmount()
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    if (originalHidden) Object.defineProperty(document, 'hidden', originalHidden)
  })

  it('actively verifies a pending desktop Alipay order', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({ data: orderFactory('COMPLETED') })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/desktop-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
      },
      global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ notifyError: showError }) }, stubs: { Icon: true } },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.emitted('success')).toHaveLength(1)

    wrapper.unmount()
  })

  it('keeps the QR fallback hidden until the Alipay app launch times out', async () => {
    const originalLocation = window.location
    const originalHidden = Object.getOwnPropertyDescriptor(document, 'hidden')
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { assign },
    })
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => false,
    })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/dynamic-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
        mobileAlipayDeepLink: true,
      },
      global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ notifyError: showError }) }, stubs: { Icon: true } },
    })

    await flushPromises()
    expect(assign).toHaveBeenCalledWith(expect.stringContaining('alipays://platformapi/startapp?saId=10000007&qrcode='))
    expect(wrapper.find('[data-test="alipay-qr-fallback"]').exists()).toBe(false)

    await vi.advanceTimersByTimeAsync(2200)
    await flushPromises()

    expect(wrapper.find('[data-test="alipay-qr-fallback"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('payment.qr.saveQRCode')
    expect(wrapper.text()).toContain('sub2_20260420abcd1234')
    expect(toCanvas).toHaveBeenCalledWith(expect.any(HTMLCanvasElement), 'https://qr.alipay.com/dynamic-order-42', expect.any(Object))

    wrapper.unmount()
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    if (originalHidden) Object.defineProperty(document, 'hidden', originalHidden)
  })

  it('does not show the QR fallback after the page enters the background', async () => {
    const originalLocation = window.location
    const originalHidden = Object.getOwnPropertyDescriptor(document, 'hidden')
    let hidden = false
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { assign: vi.fn() },
    })
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => hidden,
    })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/dynamic-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
        mobileAlipayDeepLink: true,
      },
      global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ notifyError: showError }) }, stubs: { Icon: true } },
    })

    await flushPromises()
    hidden = true
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(2200)
    await flushPromises()

    expect(wrapper.find('[data-test="alipay-qr-fallback"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('payment.qr.alipayContinueInApp')

    wrapper.unmount()
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    if (originalHidden) Object.defineProperty(document, 'hidden', originalHidden)
  })
  it.each([new Error('connection lost'), { status: 503 }])('does not replay cancellation with an unknown result: %s', async (error) => {
    cancelOrder.mockRejectedValue(error)
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({ data: orderFactory('PENDING') })
    const wrapper = mount(PaymentStatusPanel, {
      props: { orderId: 42, qrCode: 'private-qr', expiresAt: '2099-01-01T12:30:00Z', paymentType: 'alipay' },
      global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ notifyError: showError }) }, stubs: { Icon: true } },
    })
    const cancel = wrapper.get('button.btn-secondary.w-full')
    await cancel.trigger('click'); await flushPromises()
    expect(cancel.attributes('disabled')).toBeDefined()
    expect(wrapper.find('[role="status"]').text()).toContain('payment.result.processing')
    await cancel.trigger('click')
    await vi.advanceTimersByTimeAsync(3000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    expect(cancelOrder).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('settled')).toBeUndefined()
    wrapper.unmount()
  })

  it('allows an explicit retry after a definite cancellation rejection', async () => {
    cancelOrder.mockRejectedValueOnce({ status: 400, reason: 'CANCEL_RATE_LIMITED' }).mockResolvedValueOnce({})
    const wrapper = mount(PaymentStatusPanel, {
      props: { orderId: 42, qrCode: 'private-qr', expiresAt: '2099-01-01T12:30:00Z', paymentType: 'alipay' },
      global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ notifyError: showError }) }, stubs: { Icon: true } },
    })
    const cancel = wrapper.get('button.btn-secondary.w-full')
    await cancel.trigger('click'); await flushPromises()
    expect(cancel.attributes('disabled')).toBeUndefined()
    await cancel.trigger('click'); await flushPromises()
    expect(cancelOrder).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('settled')).toEqual([['cancelled']])
    wrapper.unmount()
  })

  it.each(['resolve', 'reject'] as const)('keeps confirmed payment when an older cancellation %s arrives', async (completion) => {
    let resolve!: () => void
    let reject!: (error: unknown) => void
    cancelOrder.mockReturnValueOnce(new Promise<void>((done, fail) => { resolve = done; reject = fail }))
    pollOrderStatus.mockResolvedValue(orderFactory('COMPLETED'))
    const wrapper = mount(PaymentStatusPanel, {
      props: { orderId: 42, qrCode: 'private-qr', expiresAt: '2099-01-01T12:30:00Z', paymentType: 'alipay' },
      global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ notifyError: showError }) }, stubs: { Icon: true } },
    })
    await wrapper.get('button.btn-secondary.w-full').trigger('click')
    const signal = cancelOrder.mock.calls[0][1].signal as AbortSignal
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.emitted('success')).toHaveLength(1)
    expect(signal.aborted).toBe(true)
    if (completion === 'resolve') resolve()
    else reject(new Error('late cancellation response'))
    await flushPromises()
    expect(wrapper.emitted('settled')).toEqual([['success']])
    expect(wrapper.text()).toContain('payment.result.success')
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('hides a previous session payment and drops a late poll', async () => {
    const scopeKey = ref<string | null>('session-a')
    let resolve!: (order: ReturnType<typeof orderFactory>) => void
    pollOrderStatus.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const wrapper = mount(PaymentStatusPanel, {
      props: { orderId: 42, qrCode: 'private-qr', expiresAt: '2099-01-01T12:30:00Z', paymentType: 'alipay' },
      global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ scopeKey }) }, stubs: { Icon: true } },
    })
    await vi.advanceTimersByTimeAsync(3000)
    const signal = pollOrderStatus.mock.calls[0][1].signal
    scopeKey.value = 'session-b'; resolve(orderFactory('COMPLETED')); await flushPromises()
    expect(signal.aborted).toBe(true); expect(wrapper.emitted('success')).toBeUndefined()
    expect(wrapper.text()).toBe(''); expect(vi.getTimerCount()).toBe(0)
    wrapper.unmount()
  })

})
