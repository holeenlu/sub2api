import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, enableAutoUnmount, flushPromises } from '@vue/test-utils'
import ModelDistributionChart from '../ModelDistributionChart.vue'
import EndpointDistributionChart from '../EndpointDistributionChart.vue'
import { usageServicesKey } from '../../services'
import { createUsageTestServices } from '../../__tests__/services'
const getUserBreakdown = vi.hoisted(() => vi.fn())
vi.mock('../../adminDashboard', () => ({ getUserBreakdown }))
vi.mock('vue-chartjs', () => ({ Doughnut: { template: '<div />' } }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => getUserBreakdown.mockReset())
const model = { model: 'same-model', requests: 1, total_tokens: 2, input_tokens: 1, output_tokens: 1, cache_creation_tokens: 0, cache_read_tokens: 0, cost: 0.1, actual_cost: 0.1 }
const endpoint = { endpoint: '/v1/responses', requests: 1, total_tokens: 2, cost: 0.1, actual_cost: 0.1 }
const globalOptions = () => ({ provide: { [usageServicesKey as symbol]: createUsageTestServices() }, stubs: { UserBreakdownSubTable: true } })
describe('distribution source switches', () => {
  it('invalidates an expanded model when requested/upstream/mapping source changes', async () => {
    let finish!: (value: unknown) => void; getUserBreakdown.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = mount(ModelDistributionChart, { props: { modelStats: [model], upstreamModelStats: [model] }, global: globalOptions() })
    await wrapper.get('tbody tr').trigger('click'); await flushPromises()
    const signal = getUserBreakdown.mock.calls[0][1].signal
    await wrapper.setProps({ source: 'upstream' }); finish({ users: [{ user_id: 1, email: 'old-model-user' }] }); await flushPromises()
    expect(signal.aborted).toBe(true); expect(wrapper.find('user-breakdown-sub-table-stub').exists()).toBe(false)
  })
  it('invalidates an expanded endpoint when inbound/upstream/path source changes', async () => {
    let finish!: (value: unknown) => void; getUserBreakdown.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = mount(EndpointDistributionChart, { props: { endpointStats: [endpoint], upstreamEndpointStats: [endpoint] }, global: globalOptions() })
    await wrapper.get('tbody tr').trigger('click'); await flushPromises()
    const signal = getUserBreakdown.mock.calls[0][1].signal
    await wrapper.setProps({ source: 'upstream' }); finish({ users: [{ user_id: 1, email: 'old-endpoint-user' }] }); await flushPromises()
    expect(signal.aborted).toBe(true); expect(wrapper.find('user-breakdown-sub-table-stub').exists()).toBe(false)
  })
})
