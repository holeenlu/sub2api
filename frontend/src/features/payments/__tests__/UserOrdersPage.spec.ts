import { paymentsServicesKey } from '../services'
import { createPaymentsServices } from './services'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UserOrdersView from '../pages/UserOrdersPage.vue'
import Select from '@/shared/ui/Select.vue'
import Pagination from '@/shared/ui/Pagination.vue'

const api = vi.hoisted(() => ({ getMyOrders: vi.fn(), getRefundEligibleProviders: vi.fn() }))
vi.mock('@/features/payments/api/payment', () => ({ paymentAPI: api }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.clearAllMocks()
  api.getMyOrders.mockResolvedValue({ data: { items: [], total: 100 } })
  api.getRefundEligibleProviders.mockResolvedValue({ data: { provider_instance_ids: [] } })
})

async function openOrders() {
  const wrapper = mount(UserOrdersView, {
    global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices() }, stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      OrderTable: true, BaseDialog: true, Icon: true, Pagination: true, teleport: true
    } }
  })
  await flushPromises()
  wrapper.getComponent(Pagination).vm.$emit('update:page', 4)
  await flushPromises()
  return wrapper
}

describe('order status filtering', () => {
  it('loads the first page with the selected status', async () => {
    const wrapper = await openOrders()
    const select = wrapper.getComponent(Select)
    await select.get('button').trigger('click')
    await select.findAll('[role="option"]').find(option => option.text() === 'payment.status.pending')!.trigger('click')
    await flushPromises()
    expect(api.getMyOrders).toHaveBeenLastCalledWith({ page: 1, page_size: 20, status: 'PENDING' }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.getComponent(Pagination).props('page')).toBe(1)
    expect(api.getMyOrders).toHaveBeenCalledTimes(3)
  })

  it('keeps the current page on manual refresh', async () => {
    const wrapper = await openOrders()
    await wrapper.get('[title="common.refresh"]').trigger('click')
    await flushPromises()
    expect(api.getMyOrders).toHaveBeenLastCalledWith({ page: 4, page_size: 20, status: undefined }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
  })
})
