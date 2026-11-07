import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { paymentsServicesKey } from '../services'
import { createPaymentsServices } from './services'
import UserOrdersPage from '../pages/UserOrdersPage.vue'
import AdminOrdersPage from '../pages/AdminOrdersPage.vue'
import type { OrderStatus, PaymentOrder } from '../api/types'

const api = vi.hoisted(() => ({
  getMyOrders: vi.fn(), getRefundEligibleProviders: vi.fn(), requestRefund: vi.fn(),
  getOrders: vi.fn(), getOrder: vi.fn(), refundOrder: vi.fn(), queryRefund: vi.fn()
}))
vi.mock('../api/payment', () => ({ paymentAPI: api }))
vi.mock('../api/adminPayment', () => ({ adminPaymentAPI: api }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)

const statuses: OrderStatus[] = ['COMPLETED', 'REFUND_REQUESTED', 'REFUND_PENDING', 'REFUND_FAILED', 'PARTIALLY_REFUNDED', 'REFUNDED']
const order = (id: number, status: OrderStatus, orderType: PaymentOrder['order_type'] = 'balance'): PaymentOrder => ({
  id, status, order_type: orderType, user_id: 9, amount: 100, pay_amount: 103, fee_rate: 3,
  provider_instance_id: 'allowed-provider', payment_type: 'stripe', out_trade_no: 'synthetic-' + id,
  refund_amount: 20, created_at: '2026-10-01', expires_at: '2026-10-02'
})
const stubs = {
  OrderTable: {
    props: ['orders'],
    template: '<section><div v-for="row in orders" :key="row.id" :data-order-id="row.id"><slot name="actions" :row="row" /></div></section>'
  },
  BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  Pagination: true, Select: true, Icon: true, OrderStatusBadge: true
}
beforeEach(() => {
  vi.resetAllMocks()
  api.getRefundEligibleProviders.mockResolvedValue({ data: { provider_instance_ids: ['allowed-provider'] } })
  api.requestRefund.mockResolvedValue({ data: {} })
  api.refundOrder.mockResolvedValue({ data: { success: true } })
  api.queryRefund.mockResolvedValue({ data: { success: false, warning: 'pending' } })
})

describe('refund eligibility at real order page entries', () => {
  it('only offers user refund for completed balance orders with an eligible provider', async () => {
    const orders = [
      ...statuses.map((status, index) => order(index + 1, status)),
      order(20, 'COMPLETED', 'subscription'),
      { ...order(21, 'COMPLETED'), provider_instance_id: undefined },
      { ...order(22, 'COMPLETED'), provider_instance_id: 'ineligible-provider' }
    ]
    api.getMyOrders.mockResolvedValue({ data: { items: orders, total: orders.length } })
    const wrapper = mount(UserOrdersPage, { global: { stubs, provide: { [paymentsServicesKey as symbol]: createPaymentsServices() } } })
    await flushPromises()
    for (const row of orders) {
      expect(wrapper.get('[data-order-id="' + row.id + '"]').text().includes('payment.orders.requestRefund')).toBe(row.id === 1)
    }
    await wrapper.get('[data-order-id="1"] button').trigger('click')
    await wrapper.get('textarea').setValue('  synthetic reason  ')
    const confirm = wrapper.findAll('button').find(button => button.text() === 'payment.orders.requestRefund' && button.classes().includes('btn-primary'))!
    await confirm.trigger('click')
    await flushPromises()
    expect(api.requestRefund).toHaveBeenCalledTimes(1)
    expect(api.requestRefund).toHaveBeenCalledWith(1, { reason: 'synthetic reason' }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
  })

  it('does not infer user refund eligibility when the provider query fails', async () => {
    api.getMyOrders.mockResolvedValue({ data: { items: [order(1, 'COMPLETED')], total: 1 } })
    api.getRefundEligibleProviders.mockRejectedValue(new Error('unavailable'))
    const wrapper = mount(UserOrdersPage, { global: { stubs, provide: { [paymentsServicesKey as symbol]: createPaymentsServices() } } })
    await flushPromises()
    expect(wrapper.get('[data-order-id="1"]').find('button').exists()).toBe(false)
  })

  it.each([true, false])('admin refund and query actions follow status and permission=%s', async canRefundOrders => {
    const orders = statuses.map((status, index) => order(index + 1, status))
    api.getOrders.mockResolvedValue({ data: { items: orders, total: orders.length } })
    const permissions = ref({ canManageOrders: true, canRefundOrders })
    const wrapper = mount(AdminOrdersPage, { global: { stubs, provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ permissions }) } } })
    await flushPromises()
    const labels = ['payment.admin.refund', 'payment.admin.approveRefund', 'payment.admin.queryRefundStatus', 'payment.admin.retryRefund', null, null]
    for (const [index, row] of orders.entries()) {
      const actions = wrapper.get('[data-order-id="' + row.id + '"]').findAll('button').map(button => button.text()).filter(text => text !== 'common.view')
      expect(actions).toEqual(canRefundOrders && labels[index] ? [labels[index]] : [])
    }
    if (canRefundOrders) {
      const query = wrapper.get('[data-order-id="3"]').findAll('button').find(button => button.text() === 'payment.admin.queryRefundStatus')!
      await query.trigger('click'); await flushPromises()
      expect(api.queryRefund).toHaveBeenCalledWith(3, expect.objectContaining({ signal: expect.any(AbortSignal) }))
      expect(api.refundOrder).not.toHaveBeenCalled()
      permissions.value = { canManageOrders: true, canRefundOrders: false }
      await flushPromises()
      expect(wrapper.get('[data-order-id="1"]').findAll('button')).toHaveLength(1)
    }
  })
})
