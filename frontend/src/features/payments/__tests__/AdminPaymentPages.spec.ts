import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { shallowMount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { paymentsServicesKey } from '../services'
import { createPaymentsServices } from './services'
import AdminOrdersPage from '../pages/AdminOrdersPage.vue'
import AdminPaymentDashboardPage from '../pages/AdminPaymentDashboardPage.vue'
const api = vi.hoisted(() => ({ getOrders: vi.fn(), getDashboard: vi.fn() }))
vi.mock('../api/adminPayment', () => ({ adminPaymentAPI: api }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.resetAllMocks() })
describe('administrator payment pages', () => {
  it('loads the original paginated order query through the feature owner', async () => {
    api.getOrders.mockResolvedValue({ data: { items: [], total: 0 } })
    const wrapper = shallowMount(AdminOrdersPage, { global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices() } } })
    await flushPromises()
    expect(api.getOrders).toHaveBeenCalledWith({ page: 1, page_size: 20, keyword: undefined, status: undefined, payment_type: undefined, order_type: undefined }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.find('order-table-stub').exists()).toBe(true)
  })
  it('reloads the payment dashboard without changing day-range semantics', async () => {
    api.getDashboard.mockResolvedValue({ data: { today_amount: {}, total_amount: {}, avg_amount: {}, today_count: 0, total_count: 0, daily_series: [], payment_methods: [], top_users: {} } })
    const scope = ref<string | null>('session-a')
    const wrapper = shallowMount(AdminPaymentDashboardPage, { global: { provide: { [paymentsServicesKey as symbol]: createPaymentsServices({ scopeKey: scope }) } } })
    await flushPromises()
    expect(api.getDashboard).toHaveBeenCalledWith(30, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    await wrapper.findAll('button')[0].trigger('click'); await flushPromises()
    expect(api.getDashboard).toHaveBeenLastCalledWith(7, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    scope.value = null; await flushPromises()
    expect(wrapper.find('order-stats-cards-stub').exists()).toBe(false)
  })
})
