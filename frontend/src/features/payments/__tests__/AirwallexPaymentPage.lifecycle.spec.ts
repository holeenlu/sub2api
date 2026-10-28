import { paymentsServicesKey } from '../services'
import { createPaymentsServices } from './services'
vi.mock('../api/payment', () => ({ paymentAPI: { getOrder: vi.fn().mockResolvedValue({ data: { id: 101, user_id: 9, status: 'PENDING' } }) } }))
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import AirwallexPaymentView from '../pages/AirwallexPaymentPage.vue'
import { PAYMENT_RECOVERY_STORAGE_KEY } from '@/features/payments/model/paymentFlow'

const mocks = vi.hoisted(() => ({ init: vi.fn(), redirectToCheckout: vi.fn() }))
vi.mock('@airwallex/components-sdk', () => ({ init: mocks.init }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: {} }), useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key, locale: { value: 'zh-CN', __v_isRef: true } })
}))
enableAutoUnmount(afterEach)
afterEach(() => window.localStorage.clear())

beforeEach(() => {
  vi.clearAllMocks()
  window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({
    orderId: 101, amount: 88, qrCode: '', expiresAt: '2099-01-01T00:10:00.000Z',
    paymentType: 'airwallex', payUrl: '/payment/airwallex', outTradeNo: 'sub2_awx_101',
    clientSecret: 'awx_client_secret', intentId: 'int_awx_101', currency: 'CNY',
    countryCode: 'CN', paymentEnv: 'demo', payAmount: 88, orderType: 'balance',
    paymentMode: '', resumeToken: 'resume-awx', createdAt: Date.UTC(2099, 0, 1)
  }))
})

function mountView(status = 'PENDING') {
  return shallowMount(AirwallexPaymentView, {
    global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ resolvePublicOrder: vi.fn().mockResolvedValue({ out_trade_no: 'sub2_awx_101', status, paid: false }) }) }, stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } }
  })
}

describe('Airwallex checkout lifecycle', () => {
  it.each(['CANCELLED', 'EXPIRED', 'FAILED', 'REFUNDED', 'REFUND_PENDING', 'FUTURE_PROCESSING'])('does not launch Airwallex checkout for %s', async status => {
    mountView(status); await flushPromises()
    expect(mocks.init).not.toHaveBeenCalled()
    expect(mocks.redirectToCheckout).not.toHaveBeenCalled()
  })

  it('does not initialize checkout after leaving during the SDK import', async () => {
    mocks.init.mockResolvedValue({ payments: { redirectToCheckout: mocks.redirectToCheckout } })
    const wrapper = mountView()
    wrapper.unmount()
    await flushPromises()
    expect(mocks.init).not.toHaveBeenCalled()
    expect(mocks.redirectToCheckout).not.toHaveBeenCalled()
  })

  it.each([true, false])('redirects after SDK initialization only while mounted (leave=%s)', async (leave) => {
    let finish!: (result: unknown) => void
    mocks.init.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = mountView()
    await flushPromises()
    expect(mocks.init).toHaveBeenCalledOnce()
    if (leave) wrapper.unmount()
    finish({ payments: { redirectToCheckout: mocks.redirectToCheckout } })
    await flushPromises()
    expect(mocks.redirectToCheckout).toHaveBeenCalledTimes(leave ? 0 : 1)
  })
})
