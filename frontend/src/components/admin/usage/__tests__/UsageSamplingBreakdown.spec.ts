import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UsageSamplingBreakdown from '../UsageSamplingBreakdown.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, args?: { count: number }) => args ? `${key} ${args.count}` : key }) }))
describe('Cloud sampling detail', () => {
  it('keeps historical missing details absent rather than inventing a zero segment', () => {
    expect(mount(UsageSamplingBreakdown).find('table').exists()).toBe(false)
  })
  it('renders disjoint tokens and actual calculated costs without duplicating reasoning', () => {
    const wrapper = mount(UsageSamplingBreakdown, { props: { details: { version: 1, scope: 'cloud_turn', pricing_mode: 'token', hosted_cost: 0.01, hosted_actual_cost: 0.02, samples: [
      { input_tokens: 20, cache_read_tokens: 80, cache_creation_tokens: 0, output_tokens: 5, reasoning_tokens: 4, total_cost: 0.12, actual_cost: 0.24 },
      { input_tokens: 30, cache_read_tokens: 100, cache_creation_tokens: 0, output_tokens: 8, reasoning_tokens: 6, total_cost: 0.2, actual_cost: 0.4 },
    ] } } })
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.findAll('tbody tr')[0].findAll('td').map(x => x.text())).toEqual(['1', '20', '80', '0', '5', '$0.12000000', '$0.24000000'])
    expect(wrapper.text()).toContain('usage.samplingHosted')
  })
  it('does not invent per-step charges on root-priced plans', () => {
    const wrapper = mount(UsageSamplingBreakdown, { props: { details: { version: 1, scope: 'cloud_turn', pricing_mode: 'per_request', hosted_cost: 0, hosted_actual_cost: 0, samples: [{ input_tokens: 1, output_tokens: 2, cache_read_tokens: 0, cache_creation_tokens: 0, reasoning_tokens: 0, total_cost: null, actual_cost: null }] } } })
    expect(wrapper.text()).toContain('usage.samplingRootPrice')
    expect(wrapper.findAll('tbody td').slice(-2).map(x => x.text())).toEqual(['—', '—'])
  })
})
