import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { h } from 'vue'

import BreakdownRanking from '../BreakdownRanking.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

type Row = {
  id: number
  name: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
}

const row = (id: number, tokens: number): Row => ({
  id,
  name: `row-${id}`,
  requests: 1234,
  input_tokens: tokens,
  output_tokens: 0,
  cache_tokens: 0,
  total_tokens: tokens,
  cost: 1,
  actual_cost: 0.5,
})

const fetch = vi.fn()

const mountRanking = (props: Record<string, unknown> = {}) =>
  mount(BreakdownRanking, {
    props: {
      fetch,
      rowKey: (item: Row) => item.id,
      identityColumns: [{ key: 'name', label: 'col.name' }],
      subtitle: 'sub',
      countLabel: 'count',
      rowHint: 'hint',
      startDate: '2026-07-01',
      endDate: '2026-07-08',
      filters: {},
      ...props,
    },
    slots: {
      'cell-name': ({ item }: { item: Row }) => h('span', item.name),
    },
    global: { stubs: { Select: true, LoadingSpinner: true } },
  })

describe('BreakdownRanking', () => {
  beforeEach(() => {
    fetch.mockReset()
    fetch.mockResolvedValue([row(1, 100), row(2, 50)])
  })

  it('loads on mount with shared filters, renders identity slot and emits select with the row', async () => {
    const wrapper = mountRanking({ filters: { group_id: 3 }, model: 'claude-fable-5' })
    await flushPromises()

    expect(fetch).toHaveBeenCalledWith(expect.objectContaining({
      group_id: 3,
      model: 'claude-fable-5',
      start_date: '2026-07-01',
      end_date: '2026-07-08',
      sort_by: 'total_tokens',
      limit: 50,
    }))

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('row-1')

    await rows[0].trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual([row(1, 100)])
  })

  it('formats requests as a count and cost as currency', async () => {
    const wrapper = mountRanking()
    await flushPromises()

    const cells = wrapper.findAll('tbody tr')[0].findAll('td')
    // # / name / requests / input / output / cache / total / cost
    expect(cells).toHaveLength(8)
    expect(cells[2].text()).toBe((1234).toLocaleString())
    expect(cells[7].text()).toBe('$0.5000')
  })

  it('reloads with the new sort key when a metric header is clicked', async () => {
    const wrapper = mountRanking()
    await flushPromises()
    fetch.mockClear()

    // # 与身份列不可排序，指标列从第 3 个 th 起。
    const headers = wrapper.findAll('thead th')
    await headers[2].trigger('click')
    await flushPromises()

    expect(fetch).toHaveBeenCalledWith(expect.objectContaining({ sort_by: 'requests' }))
  })

  it('hides columns the parent filtered out and keeps header/cell counts aligned', async () => {
    const wrapper = mountRanking({ visibleColumnKeys: ['requests', 'actual_cost'] })
    await flushPromises()

    const headers = wrapper.findAll('thead th')
    expect(headers).toHaveLength(3) // # / requests / cost — identity column hidden too
    expect(wrapper.text()).not.toContain('row-1')

    const cells = wrapper.findAll('tbody tr')[0].findAll('td')
    expect(cells).toHaveLength(3)
    expect(cells[1].text()).toBe((1234).toLocaleString())
    expect(cells[2].text()).toContain('$')
  })

  it('does not request while inactive and catches up once when activated', async () => {
    const wrapper = mountRanking({ active: false })
    await flushPromises()
    expect(fetch).not.toHaveBeenCalled()

    // 隐藏期间筛选变化也不请求
    await wrapper.setProps({ filters: { user_id: 9 } })
    await flushPromises()
    expect(fetch).not.toHaveBeenCalled()

    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch).toHaveBeenCalledWith(expect.objectContaining({ user_id: 9 }))

    // 没有过期数据时切回来不重复请求
    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('keeps the last response when an earlier in-flight request resolves later', async () => {
    let resolveSlow: (v: unknown) => void = () => {}
    fetch
      .mockImplementationOnce(() => new Promise((resolve) => { resolveSlow = resolve }))
      .mockResolvedValueOnce([row(9, 999)])

    const wrapper = mountRanking()
    await wrapper.setProps({ startDate: '2026-07-02' })
    await flushPromises()

    resolveSlow([row(1, 1)])
    await flushPromises()

    expect(wrapper.text()).toContain('row-9')
    expect(wrapper.text()).not.toContain('row-1')
  })
})
