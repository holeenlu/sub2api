import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { mountPaymentQuery, deferred } from './harness'
import { createPaymentsServices } from './services'
import { useAdminOrderActions } from '../commands/useAdminOrderActions'
import { useAdminOrders } from '../queries/useAdminOrders'
import { useUserOrders } from '../queries/useUserOrders'
import { useUserOrderActions } from '../commands/useUserOrderActions'
import type { PaymentOrder } from '../api/types'
const admin = vi.hoisted(() => ({ refundOrder: vi.fn(), queryRefund: vi.fn(), cancelOrder: vi.fn(), retryRecharge: vi.fn(), getOrders: vi.fn(), getOrder: vi.fn() }))
const user = vi.hoisted(() => ({ getMyOrders: vi.fn(), getRefundEligibleProviders: vi.fn(), requestRefund: vi.fn() }))
vi.mock('../api/adminPayment', () => ({ adminPaymentAPI: admin }))
vi.mock('../api/payment', () => ({ paymentAPI: user }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.resetAllMocks() })
const order = { id: 1, user_id: 9, order_type: 'balance', provider_instance_id: 'eligible', status: 'COMPLETED', amount: 100, pay_amount: 100 } as PaymentOrder
const refund = { amount: 10.25, reason: 'requested', deduct_balance: true, force: false }
describe('administrator order commands', () => {
  it('requires the original refund permission at the command boundary', async () => {
    const { query } = mountPaymentQuery(() => useAdminOrderActions(ref(order), vi.fn().mockResolvedValue(undefined)))
    await query.handleRefund(refund); await query.handleCancelOrder(order)
    expect(admin.refundOrder).not.toHaveBeenCalled(); expect(admin.cancelOrder).not.toHaveBeenCalled()
  })
  it('preserves explicit force confirmation and exact refund amount', async () => {
    admin.refundOrder.mockResolvedValueOnce({ data: { success: false, require_force: true, warning: 'spent' } }).mockResolvedValueOnce({ data: { success: true } })
    const reload = vi.fn().mockResolvedValue(undefined)
    const services = createPaymentsServices({ permissions: ref({ canManageOrders: true, canRefundOrders: true }) })
    const { query } = mountPaymentQuery(() => useAdminOrderActions(ref(order), reload), services)
    query.openRefundDialog(order); await query.handleRefund(refund)
    expect(query.refundRequireForce.value).toBe(true); expect(query.showRefundDialog.value).toBe(true)
    expect(admin.refundOrder).toHaveBeenCalledTimes(1)
    await query.handleRefund({ ...refund, force: true })
    expect(admin.refundOrder).toHaveBeenLastCalledWith(1, { ...refund, force: true }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(reload).toHaveBeenCalledOnce(); expect(query.showRefundDialog.value).toBe(false)
  })
  it('never resends a refund after an unknown response', async () => {
    admin.refundOrder.mockRejectedValue(new Error('lost response'))
    const services = createPaymentsServices({ permissions: ref({ canManageOrders: true, canRefundOrders: true }) })
    const { query } = mountPaymentQuery(() => useAdminOrderActions(ref(order), vi.fn()), services)
    query.openRefundDialog(order)
    await query.handleRefund(refund); await query.handleRefund(refund)
    expect(admin.refundOrder).toHaveBeenCalledTimes(1); expect(query.uncertain.value.has(1)).toBe(true)
    query.closeRefundDialog(); query.openRefundDialog(order)
    expect(query.showRefundDialog.value).toBe(false)
    await query.handleRefund({ ...refund, force: true })
    expect(admin.refundOrder).toHaveBeenCalledTimes(1)
  })
  it('aborts on revocation and suppresses late notifications and reload', async () => {
    const permissions = ref({ canManageOrders: true, canRefundOrders: true }), pending = deferred<{ data: { success: boolean } }>()
    admin.refundOrder.mockReturnValue(pending.promise)
    const services = createPaymentsServices({ permissions }), reload = vi.fn()
    const { query } = mountPaymentQuery(() => useAdminOrderActions(ref(order), reload), services)
    query.openRefundDialog(order)
    const running = query.handleRefund(refund), signal = admin.refundOrder.mock.calls[0][2].signal
    permissions.value = { canManageOrders: false, canRefundOrders: false }
    pending.resolve({ data: { success: true } }); await running
    expect(signal.aborted).toBe(true); expect(services.notifySuccess).not.toHaveBeenCalled(); expect(reload).not.toHaveBeenCalled()
  })

  it.each(['COMPLETED', 'REFUND_REQUESTED', 'REFUND_FAILED'] as const)('keeps the allowed %s command and exact partial amount', async status => {
    const current = { ...order, status }
    admin.refundOrder.mockResolvedValue({ data: { success: true } })
    const reload = vi.fn().mockResolvedValue(undefined)
    const services = createPaymentsServices({ permissions: ref({ canManageOrders: false, canRefundOrders: true }) })
    const { query } = mountPaymentQuery(() => useAdminOrderActions(ref(current), reload), services)
    query.openRefundDialog(current)
    expect(query.showRefundDialog.value).toBe(true)
    await query.handleRefund(refund)
    expect(admin.refundOrder).toHaveBeenCalledWith(1, refund, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(admin.refundOrder).toHaveBeenCalledTimes(1)
    expect(reload).toHaveBeenCalledOnce()
  })

  it.each(['REFUND_PENDING', 'PARTIALLY_REFUNDED', 'REFUNDED', 'REFUNDING'] as const)('cannot bypass %s state from open or a stale confirm, even with force', async status => {
    const selected = ref<PaymentOrder | null>(order)
    const services = createPaymentsServices({ permissions: ref({ canManageOrders: true, canRefundOrders: true }) })
    const { query } = mountPaymentQuery(() => useAdminOrderActions(selected, vi.fn()), services)
    query.openRefundDialog({ ...order, status })
    expect(query.showRefundDialog.value).toBe(false)
    query.openRefundDialog(order)
    expect(query.showRefundDialog.value).toBe(true)
    selected.value = { ...order, status }
    await query.handleRefund({ ...refund, force: true })
    expect(admin.refundOrder).not.toHaveBeenCalled()
  })

  it('does not impose the user-only balance rule on an authorized administrator', async () => {
    const current: PaymentOrder = { ...order, order_type: 'subscription' }
    admin.refundOrder.mockResolvedValue({ data: { success: true } })
    const services = createPaymentsServices({ permissions: ref({ canManageOrders: false, canRefundOrders: true }) })
    const { query } = mountPaymentQuery(() => useAdminOrderActions(ref(current), vi.fn()), services)
    query.openRefundDialog(current)
    await query.handleRefund(refund)
    expect(admin.refundOrder).toHaveBeenCalledOnce()
  })

  it('does not turn an unknown pending refund into a new refund attempt', async () => {
    const current: PaymentOrder = { ...order, status: 'REFUND_PENDING' }
    admin.queryRefund.mockRejectedValue(new Error('query outcome unknown'))
    const services = createPaymentsServices({ permissions: ref({ canManageOrders: false, canRefundOrders: true }) })
    const { query } = mountPaymentQuery(() => useAdminOrderActions(ref(current), vi.fn()), services)
    await query.handleQueryRefund(current)
    query.openRefundDialog(current)
    await query.handleRefund({ ...refund, force: true })
    expect(admin.queryRefund).toHaveBeenCalledTimes(1)
    expect(admin.refundOrder).not.toHaveBeenCalled()
    expect(query.uncertain.value.has(current.id)).toBe(true)
  })

  it('rechecks refund permission and explicit dialog cancellation before dispatch', async () => {
    const permissions = ref({ canManageOrders: true, canRefundOrders: true })
    const { query } = mountPaymentQuery(() => useAdminOrderActions(ref(order), vi.fn()), createPaymentsServices({ permissions }))
    query.openRefundDialog(order); query.closeRefundDialog()
    await query.handleRefund(refund)
    query.openRefundDialog(order)
    permissions.value = { canManageOrders: true, canRefundOrders: false }
    await query.handleRefund(refund)
    expect(query.showRefundDialog.value).toBe(false)
    expect(admin.refundOrder).not.toHaveBeenCalled()
  })
})

describe('user refund command eligibility', () => {
  async function setup() {
    user.getRefundEligibleProviders.mockResolvedValue({ data: { provider_instance_ids: ['eligible'] } })
    const reload = vi.fn().mockResolvedValue(undefined)
    const scopeKey = ref<string | null>('session:9')
    const host = mountPaymentQuery(() => {
      const orders = useUserOrders()
      return { orders, actions: useUserOrderActions(reload, orders.canRequestRefund) }
    }, createPaymentsServices({ scopeKey }))
    await host.query.orders.loadRefundEligibility()
    return { ...host, reload, scopeKey }
  }

  it.each(['subscription', undefined, 'future-type'])('blocks order type %s at both open and confirm', async orderType => {
    const { query: { actions } } = await setup()
    const invalid = { ...order, order_type: orderType } as PaymentOrder
    actions.openRefundDialog(invalid)
    expect(actions.refundTarget.value).toBeNull()
    actions.refundTarget.value = invalid; actions.refundReason.value = 'reason'
    await actions.confirmRefund()
    expect(user.requestRefund).not.toHaveBeenCalled()
  })

  it.each(['REFUND_REQUESTED', 'REFUND_PENDING', 'REFUND_FAILED', 'PARTIALLY_REFUNDED', 'REFUNDED'] as const)('rechecks current %s state at confirmation', async status => {
    const { query: { actions } } = await setup()
    actions.openRefundDialog(order)
    actions.refundReason.value = 'reason'
    actions.refundTarget.value = { ...order, status }
    await actions.confirmRefund()
    expect(user.requestRefund).not.toHaveBeenCalled()
  })

  it('rechecks the provider eligibility before confirmation', async () => {
    const { query: { actions, orders } } = await setup()
    actions.openRefundDialog(order); actions.refundReason.value = 'reason'
    user.getRefundEligibleProviders.mockResolvedValue({ data: { provider_instance_ids: [] } })
    await orders.loadRefundEligibility()
    await actions.confirmRefund()
    expect(user.requestRefund).not.toHaveBeenCalled()
  })

  it('retains the unknown-submit lock even after closing the dialog', async () => {
    user.requestRefund.mockRejectedValue(new Error('lost refund-request response'))
    const { query: { actions }, reload, services } = await setup()
    actions.openRefundDialog(order); actions.refundReason.value = 'reason'
    await actions.confirmRefund()
    actions.refundTarget.value = null
    actions.openRefundDialog(order)
    expect(actions.refundTarget.value).toBeNull()
    await actions.confirmRefund()
    expect(user.requestRefund).toHaveBeenCalledTimes(1)
    expect(actions.actionLoading.value).toBe(true)
    expect(reload).not.toHaveBeenCalled()
    expect(services.notifySuccess).not.toHaveBeenCalled()
  })

  it('suppresses a late result after the session is replaced', async () => {
    const pending = deferred<unknown>()
    user.requestRefund.mockReturnValue(pending.promise)
    const { query: { actions }, services, reload, scopeKey } = await setup()
    actions.openRefundDialog(order); actions.refundReason.value = 'reason'
    const running = actions.confirmRefund()
    const signal = user.requestRefund.mock.calls[0][2].signal as AbortSignal
    scopeKey.value = null
    pending.resolve({})
    await running
    expect(signal.aborted).toBe(true)
    expect(reload).not.toHaveBeenCalled()
    expect(services.notifySuccess).not.toHaveBeenCalled()
  })
})
describe('order queries', () => {
  it('does not replace the newest user page with a late page', async () => {
    const first = deferred<{ data: { items: PaymentOrder[]; total: number } }>()
    user.getMyOrders.mockReturnValueOnce(first.promise).mockResolvedValueOnce({ data: { items: [{ ...order, id: 2 }], total: 2 } })
    const { query } = mountPaymentQuery(useUserOrders)
    const pending = query.fetchOrders(), signal = user.getMyOrders.mock.calls[0][1].signal
    query.pagination.page = 2; await query.fetchOrders(); first.resolve({ data: { items: [order], total: 1 } }); await pending
    expect(signal.aborted).toBe(true); expect(query.orders.value[0].id).toBe(2); expect(query.pagination.total).toBe(2)
  })
  it('aborts old order detail when another order is opened', async () => {
    const first = deferred<{ data: { order: PaymentOrder } }>()
    admin.getOrder.mockReturnValueOnce(first.promise).mockResolvedValueOnce({ data: { order: { ...order, id: 2 }, audit_logs: [] } })
    const { query } = mountPaymentQuery(useAdminOrders)
    const pending = query.showOrderDetail(order), signal = admin.getOrder.mock.calls[0][1].signal
    await query.showOrderDetail({ ...order, id: 2 }); first.resolve({ data: { order } }); await pending; await flushPromises()
    expect(signal.aborted).toBe(true); expect(query.selectedOrder.value?.id).toBe(2)
  })
})
