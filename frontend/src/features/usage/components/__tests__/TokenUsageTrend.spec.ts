import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { ChartData, TooltipCallbacks, TooltipItem } from 'chart.js'
import { Line } from 'vue-chartjs'

import TokenUsageTrend from '../TokenUsageTrend.vue'

const messages: Record<string, string> = {
  'admin.dashboard.tokenUsageTrend': 'Token Usage Trend',
  'admin.dashboard.noDataAvailable': 'No data available',
  'usage.cacheHitRate': 'Cache share',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Line: {
    props: ['data', 'options'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

describe('TokenUsageTrend', () => {
  it('calculates cache hit rate against all prompt tokens', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 500,
            output_tokens: 100,
            cache_creation_tokens: 0,
            cache_read_tokens: 1500,
            cost: 0.01,
            actual_cost: 0.005,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData: ChartData<'line'> = wrapper.getComponent(Line).props('data')
    const hitRateDataset = chartData.datasets.find(
      ds => ds.label === 'Cache share'
    )
    // Hit rate = 1500 / (500 + 1500 + 0) * 100 = 75%
    expect(hitRateDataset?.data[0]).toBe(75)
    wrapper.unmount()
  })

  it('leaves a gap instead of claiming a zero share when input was not observed', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 0,
            input_tokens: 0,
            output_tokens: 0,
            cache_creation_tokens: 0,
            cache_read_tokens: 0,
            cost: 0,
            actual_cost: 0,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData: ChartData<'line'> = wrapper.getComponent(Line).props('data')
    const hitRateDataset = chartData.datasets.find(
      ds => ds.label === 'Cache share'
    )
    expect(hitRateDataset?.data[0]).toBeNull()
    wrapper.unmount()
  })

  it('includes cache_creation_tokens in denominator for Anthropic models', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 200,
            output_tokens: 50,
            cache_creation_tokens: 300,
            cache_read_tokens: 500,
            cost: 0.02,
            actual_cost: 0.01,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData: ChartData<'line'> = wrapper.getComponent(Line).props('data')
    const hitRateDataset = chartData.datasets.find(
      ds => ds.label === 'Cache share'
    )
    // Hit rate = 500 / (200 + 500 + 300) * 100 = 50%
    expect(hitRateDataset?.data[0]).toBe(50)
    wrapper.unmount()
  })

  it.each([
    { raw: null, tokens: '—', rate: '—' },
    { raw: undefined, tokens: '—', rate: '—' },
    { raw: NaN, tokens: '—', rate: '—' },
    { raw: Infinity, tokens: '—', rate: '—' },
    { raw: '0', tokens: '—', rate: '—' },
    { raw: { y: 0 }, tokens: '—', rate: '—' },
    { raw: 0, tokens: '0', rate: '0.0%' },
    { raw: 75, tokens: '75', rate: '75.0%' },
    { raw: 1500, tokens: '1.50K', rate: '1500.0%' },
  ])('formats tooltip raw value $raw without replacing unknown with zero', ({ raw, tokens, rate }) => {
    const wrapper = mount(TokenUsageTrend, {
      props: { trendData: [{ date: '2026-10-26', requests: 1, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, cost: 0, actual_cost: 0 }] },
    })
    try {
      // The callbacks consume these fields only; chart/canvas internals are not simulated.
      type Item = Pick<TooltipItem<'line'>, 'dataset' | 'raw' | 'dataIndex'>
      const { label, footer }: Pick<TooltipCallbacks<'line', void, Item>, 'label' | 'footer'> =
        wrapper.getComponent(Line).props('options').plugins.tooltip.callbacks
      const item: Item = { dataset: { label: 'Input', data: [] }, raw, dataIndex: 0 }
      expect(label(item)).toBe(`Input: ${tokens}`)
      expect(label({ ...item, dataset: { label: 'Cache share', yAxisID: 'yPercent', data: [] } })).toBe(`Cache share: ${rate}`)
      expect(footer([item])).toBe('Actual: $0.0000 | Standard: $0.0000')
      expect(footer([])).toBe('')
      expect(footer([{ ...item, dataIndex: 1 }])).toBe('')
    } finally {
      wrapper.unmount()
    }
  })
})
