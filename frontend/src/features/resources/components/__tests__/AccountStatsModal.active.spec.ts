import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { ref } from 'vue'
import type { TooltipCallbacks, TooltipItem } from 'chart.js'
import { Line } from 'vue-chartjs'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import AccountStatsModal from '../AccountStatsModal.vue'
import { resourceEditorServicesKey } from '../../services'
import { editorTestServices } from '../../__tests__/editorTestServices'
import { accountFixture } from '../../__tests__/accountFixture'
import type { AccountUsageStatsResponse } from '@/features/resources/api/accountUsageTypes';

const { getStats } = vi.hoisted(() => ({ getStats: vi.fn() }))
vi.mock('@/features/resources/accounts', () => ({ default: { getStats } }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-chartjs', () => ({ Line: { props: ['data', 'options'], template: '<div data-test="trend" />' } }))
const wrappers: VueWrapper[] = []
function stats(cost: number): AccountUsageStatsResponse {
  return {
    history: [{ date: '2026-10-25', label: '10/25', requests: 4, tokens: 15, cost: 0.8, actual_cost: cost, user_cost: 0.75 }],
    summary: { days: 30, actual_days_used: 1, total_cost: cost, total_user_cost: 0.75, total_standard_cost: 0.8, total_requests: 4, total_tokens: 15, avg_daily_cost: cost, avg_daily_user_cost: 0.75, avg_daily_requests: 4, avg_daily_tokens: 15, avg_duration_ms: 1500, today: { date: '2026-10-25', cost, user_cost: 0.75, requests: 4, tokens: 15 }, highest_cost_day: null, highest_request_day: null },
    models: [], endpoints: [], upstream_endpoints: []
  }
}
function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Not initialized') }
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
function setup() {
  const actor = ref('actor-1:admin:0'), services = editorTestServices()
  services.scopeKey = () => actor.value
  const wrapper = mount(AccountStatsModal, { props: { show: true, account: accountFixture() }, global: {
    provide: { [resourceEditorServicesKey as symbol]: services },
    stubs: { BaseDialog: { props: ['show'], emits: ['close'], template: '<section v-if="show"><slot /><slot name="footer" /><button data-test="close" @click="$emit(\'close\')">close</button></section>' }, LoadingSpinner: true, Icon: true, ModelDistributionChart: true, EndpointDistributionChart: true }
  } })
  wrappers.push(wrapper)
  return { wrapper, actor, services }
}
beforeEach(() => getStats.mockReset().mockResolvedValue(stats(0.5)))
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

describe('single active account statistics panel', () => {
  it('requests the unchanged 30-day statistics and keeps account/user billing display', async () => {
    const { wrapper } = setup()
    await flushPromises()
    expect(getStats).toHaveBeenCalledWith(42, 30)
    expect(wrapper.text()).toContain('admin.accounts.stats.todayCost')
    expect(wrapper.text()).toContain('usage.userBilled')
    expect(wrapper.text()).toContain('$0.500')
    expect(wrapper.text()).toContain('$0.750')
    expect(wrapper.find('[data-test="trend"]').exists()).toBe(true)
  })

  it.each([
    { raw: 0, cost: '$0.0000', requests: '0' },
    { raw: 0.5, cost: '$0.500', requests: '0.5' },
    { raw: 1200, cost: '$1.20K', requests: '1.20K' },
    { raw: null, cost: 'unavailable', requests: 'unavailable' },
    { raw: undefined, cost: 'unavailable', requests: 'unavailable' },
    { raw: Number.NaN, cost: 'unavailable', requests: 'unavailable' },
    { raw: Number.POSITIVE_INFINITY, cost: 'unavailable', requests: 'unavailable' },
    { raw: '0', cost: 'unavailable', requests: 'unavailable' },
    { raw: { y: 0 }, cost: 'unavailable', requests: 'unavailable' }
  ])('formats numeric chart values and leaves unknown raw values to Chart.js: $raw', async ({ raw, cost, requests }) => {
    const { wrapper } = setup()
    await flushPromises()
    // Only the fields consumed by this callback; no canvas or Chart.js internals are simulated.
    type Item = Pick<TooltipItem<'line'>, 'dataset' | 'raw' | 'formattedValue'>
    const { label }: Pick<TooltipCallbacks<'line', void, Item>, 'label'> =
      wrapper.getComponent(Line).props('options').plugins.tooltip.callbacks
    const item: Item = { dataset: { label: 'Cost (USD)', data: [] }, raw, formattedValue: 'unavailable' }
    expect(label(item)).toBe(`Cost (USD): ${cost}`)
    expect(label({ ...item, dataset: { label: 'Requests', data: [] } })).toBe(`Requests: ${requests}`)
  })

  it('does not let an earlier resource overwrite the new resource statistics', async () => {
    const old = deferred<AccountUsageStatsResponse>()
    getStats.mockReturnValueOnce(old.promise).mockResolvedValueOnce(stats(12))
    const { wrapper } = setup()
    await wrapper.setProps({ account: accountFixture({ id: 99, name: 'New resource' }) })
    await flushPromises()
    old.resolve(stats(999))
    await flushPromises()
    expect(getStats).toHaveBeenLastCalledWith(99, 30)
    expect(wrapper.text()).toContain('$12.00')
    expect(wrapper.text()).not.toContain('$999.00')
  })

  it.each(['actor', 'close', 'unmount'] as const)('discards private statistics after %s', async transition => {
    const old = deferred<AccountUsageStatsResponse>()
    getStats.mockReturnValueOnce(old.promise)
    const { wrapper, actor, services } = setup()
    if (transition === 'actor') actor.value = 'actor-2:supplier:9'
    if (transition === 'close') await wrapper.get('[data-test="close"]').trigger('click')
    if (transition === 'unmount') wrapper.unmount()
    old.resolve(stats(999))
    await flushPromises()
    expect(wrapper.text()).not.toContain('$999.00')
    expect(services.notices.showError).not.toHaveBeenCalled()
    expect(getStats).toHaveBeenCalledTimes(1)
    if (transition === 'close') expect(wrapper.emitted('close')).toEqual([[]])
  })

  it('reports the current request failure without replacing it with fabricated statistics', async () => {
    getStats.mockRejectedValueOnce(new Error('test failure'))
    const { wrapper, services } = setup()
    await flushPromises()
    expect(services.notices.showError).toHaveBeenCalledWith('admin.accounts.failedToLoad')
    expect(wrapper.find('[data-test="trend"]').exists()).toBe(false)
  })
})

