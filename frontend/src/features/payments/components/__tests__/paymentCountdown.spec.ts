import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import PaymentStatusPanel from '../PaymentStatusPanel.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/features/payments/api/payment', () => ({ paymentAPI: { getOrder: vi.fn().mockResolvedValue({ data: null }) } }))
import { paymentsServicesKey } from '../../services'
import { createPaymentsServices } from '../../__tests__/services'
vi.mock('qrcode', () => ({ default: { toCanvas: vi.fn() } }))
vi.mock('vue-router', () => ({
  useRoute: () => ({ query: { order_id: '42', expires_at: new Date(Date.now() + 120_000).toISOString() } }),
  useRouter: () => ({ push: vi.fn() }),
}))

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-09-30T12:00:00Z'))
})
afterEach(() => vi.useRealTimers())

async function open() {
  const props = { orderId: 42, qrCode: '', expiresAt: new Date(Date.now() + 120_000).toISOString(), paymentType: 'custom' }
  return mount(PaymentStatusPanel, { props, global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices() }, stubs: { Icon: true } } })
}

describe('payment status countdown', () => {
  it('catches up after timers were suspended', async () => {
    const wrapper = await open()
    expect(wrapper.text()).toContain('02:00')
    vi.setSystemTime(Date.now() + 65_000)
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.text()).toContain('00:54')
  })

  it('expires and stops polling on the first overdue tick', async () => {
    const wrapper = await open()
    vi.setSystemTime(Date.now() + 121_000)
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.text()).toContain('payment.qr.expired')
    expect(vi.getTimerCount()).toBe(0)
  })

  it('still counts down normally', async () => {
    const wrapper = await open()
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.text()).toContain('01:59')
  })
})
