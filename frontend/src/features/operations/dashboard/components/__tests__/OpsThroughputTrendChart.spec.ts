import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import OpsThroughputTrendChart from '../OpsThroughputTrendChart.vue'
import OpsSwitchRateTrendChart from '../OpsSwitchRateTrendChart.vue'
import { Line } from 'vue-chartjs'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const { chart } = vi.hoisted(() => {
  const chart: { toBase64Image: ReturnType<typeof vi.fn>; resetZoom?: unknown } = { toBase64Image: vi.fn() }
  return { chart }
})

vi.mock('vue-chartjs', async () => {
  const { defineComponent, h } = await import('vue')
  return {
    Line: defineComponent({
      props: ['data', 'options'],
      setup(_props, { expose }) {
        expose({ chart })
        return () => h('div', { class: 'line-chart' })
      }
    })
  }
})

beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  delete chart.resetZoom
  chart.toBase64Image.mockReturnValue('data:image/png;base64,chart')
})
afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('OpsThroughputTrendChart', () => {
  it('allows the header controls to wrap on narrow screens', () => {
    const wrapper = mount(OpsThroughputTrendChart, {
      props: {
        points: [],
        loading: false,
        timeRange: '1h',
      },
      global: {
        stubs: {
          EmptyState: true,
          HelpTooltip: true,
        },
      },
    })

    const header = wrapper.get('[data-testid="throughput-chart-header"]')
    expect(header.classes()).toEqual(expect.arrayContaining(['flex-col', 'sm:flex-row']))

    const toolbar = wrapper.get('[data-testid="throughput-chart-toolbar"]')
    expect(toolbar.classes()).toEqual(expect.arrayContaining(['w-full', 'flex-wrap', 'sm:w-auto']))
    expect(toolbar.findAll('button')).toHaveLength(3)
    toolbar.findAll('button').forEach((button) => {
      expect(button.classes()).toContain('shrink-0')
      expect(button.classes()).not.toContain('ml-2')
    })
    wrapper.unmount()
  })
})

vi.mock('@/features/operations/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/operations/services')>('@/features/operations/services')
  const { createOperationsTestServices } = await import('@/features/operations/__tests__/services')
  const services = createOperationsTestServices({

  })
  return { ...actual, useOperationsServices: () => services }
})


const chartProps = {
  points: [{ bucket_start: '2026-10-26T00:00:00Z', request_count: 10, token_consumed: 200, qps: 0, tps: 0, switch_count: 0 }],
  loading: false,
  timeRange: '1h'
}
const chartStubs = { stubs: { EmptyState: true, HelpTooltip: true } }

describe('Chart.js callback and capability boundaries', () => {
  it.each([undefined, 'not a function'])('allows reset without a zoom plugin function (%s)', async (capability) => {
    chart.resetZoom = capability
    const wrapper = mount(OpsThroughputTrendChart, { props: chartProps, global: chartStubs })
    try {
      await wrapper.get('[title="admin.ops.charts.resetZoomHint"]').trigger('click')
      await wrapper.setProps({ timeRange: '6h' })
      await vi.advanceTimersByTimeAsync(100)
    } finally { wrapper.unmount() }
    expect(vi.getTimerCount()).toBe(0)
  })

  it('resets an installed plugin on click and time-range change, and cancels its timer on unmount', async () => {
    const resetZoom = vi.fn(function (this: unknown) { expect(this).toBe(chart) })
    chart.resetZoom = resetZoom
    const wrapper = mount(OpsThroughputTrendChart, { props: chartProps, global: chartStubs })
    try {
      await wrapper.get('[title="admin.ops.charts.resetZoomHint"]').trigger('click')
      expect(resetZoom).toHaveBeenCalledTimes(1)
      await wrapper.setProps({ timeRange: '6h' })
      await vi.advanceTimersByTimeAsync(100)
      expect(resetZoom).toHaveBeenCalledTimes(2)
      await wrapper.setProps({ timeRange: '24h' })
    } finally { wrapper.unmount() }
    await vi.advanceTimersByTimeAsync(100)
    expect(resetZoom).toHaveBeenCalledTimes(2)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('downloads using the Chart.js image method', async () => {
    vi.setSystemTime(new Date('2026-10-26T00:00:00Z'))
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      expect(this.href).toBe('data:image/png;base64,chart')
      expect(this.download).toBe('ops-throughput-2026-10-26-00-00-00.png')
    })
    const wrapper = mount(OpsThroughputTrendChart, { props: chartProps, global: chartStubs })
    try {
      await wrapper.get('[title="admin.ops.charts.downloadChartHint"]').trigger('click')
      expect(chart.toBase64Image).toHaveBeenCalledWith('image/png', 1)
      expect(click).toHaveBeenCalledTimes(1)
    } finally { wrapper.unmount() }
  })

  it('keeps zero distinct from missing parsed throughput values', () => {
    const wrapper = mount(OpsThroughputTrendChart, { props: chartProps, global: chartStubs })
    try {
      const label = wrapper.getComponent(Line).props('options')?.plugins?.tooltip?.callbacks?.label
      if (typeof label !== 'function') throw new Error('Missing Chart.js tooltip callback')
      // Supply the fields read by this callback, including a null parsed value from Chart.js.
      expect(Reflect.apply(label, {}, [{ dataset: { label: 'QPS' }, raw: 0, parsed: { y: 0 } }])).toBe('QPS: 0.0')
      expect(Reflect.apply(label, {}, [{ dataset: { label: 'QPS' }, raw: null, parsed: { y: null } }])).toBe('QPS: ')
      expect(Reflect.apply(label, {}, [{ dataset: { label: 'QPS' }, raw: {}, parsed: { y: null } }])).toBe('QPS: ')
    } finally { wrapper.unmount() }
  })

  it('formats numeric and string ticks and keeps unknown switch rate distinct from zero', () => {
    const wrapper = mount(OpsSwitchRateTrendChart, { props: chartProps, global: chartStubs })
    try {
      const options = wrapper.getComponent(Line).props('options')
      const label = options?.plugins?.tooltip?.callbacks?.label
      const tick = options?.scales?.y?.ticks?.callback
      if (typeof label !== 'function' || typeof tick !== 'function') throw new Error('Missing Chart.js callbacks')
      expect(Reflect.apply(label, {}, [{ parsed: { y: 0 } }])).toBe('admin.ops.switchRate: 0.000')
      expect(Reflect.apply(label, {}, [{ parsed: { y: null } }])).toBe('admin.ops.switchRate: -')
      expect(Reflect.apply(tick, {}, [0, 0, []])).toBe('0.000')
      expect(Reflect.apply(tick, {}, ['0.125', 0, []])).toBe('0.125')
    } finally { wrapper.unmount() }
  })
})
