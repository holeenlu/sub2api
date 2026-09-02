import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

import APIKeyTokenRanking from '../APIKeyTokenRanking.vue'

const getAPIKeyBreakdown = vi.fn()

vi.mock('@/api/admin/dashboard', () => ({
  getAPIKeyBreakdown: (...args: unknown[]) => getAPIKeyBreakdown(...args),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const item = (id: number, tokens: number, overrides: Record<string, unknown> = {}) => ({
  api_key_id: id,
  key_name: `key-${id}`,
  key_deleted: false,
  user_id: 100 + id,
  email: `u${id}@test.com`,
  requests: 1,
  input_tokens: tokens,
  output_tokens: 0,
  cache_tokens: 0,
  total_tokens: tokens,
  cost: 1,
  actual_cost: 0.5,
  account_cost: 0.4,
  ...overrides,
})

const mountRanking = (props: Record<string, unknown> = {}) =>
  mount(APIKeyTokenRanking, {
    props: {
      startDate: '2026-07-01',
      endDate: '2026-07-08',
      filters: {},
      ...props,
    },
    global: { stubs: { Select: true, LoadingSpinner: true } },
  })

describe('APIKeyTokenRanking', () => {
  beforeEach(() => {
    getAPIKeyBreakdown.mockReset()
    getAPIKeyBreakdown.mockResolvedValue({ api_keys: [item(1, 100), item(2, 50)] })
  })

  it('loads on mount with the shared filters and emits select-api-key on row click', async () => {
    const wrapper = mountRanking({ filters: { group_id: 3 }, model: 'claude-fable-5' })
    await flushPromises()

    expect(getAPIKeyBreakdown).toHaveBeenCalledWith(expect.objectContaining({
      group_id: 3,
      model: 'claude-fable-5',
      start_date: '2026-07-01',
      end_date: '2026-07-08',
      sort_by: 'total_tokens',
    }))

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('u1@test.com')

    await rows[0].trigger('click')
    expect(wrapper.emitted('select-api-key')?.[0]).toEqual([1, 'key-1'])
  })

  it('reloads with the new sort key when a metric header is clicked', async () => {
    const wrapper = mountRanking()
    await flushPromises()
    getAPIKeyBreakdown.mockClear()

    // 前三列（# / 密钥 / 所属用户）不可排序，指标列从第 4 个 th 起。
    const headers = wrapper.findAll('thead th')
    await headers[3].trigger('click')
    await flushPromises()

    expect(getAPIKeyBreakdown).toHaveBeenCalledWith(
      expect.objectContaining({ sort_by: 'requests' }),
    )
  })

  it('marks soft-deleted keys and keeps their name', async () => {
    getAPIKeyBreakdown.mockResolvedValue({
      api_keys: [item(7, 10, { key_deleted: true })],
    })
    const wrapper = mountRanking()
    await flushPromises()

    expect(wrapper.find('[data-testid="api-key-ranking-deleted"]').exists()).toBe(true)
    const cell = wrapper.findAll('tbody tr')[0].findAll('td')[1]
    expect(cell.text()).toContain('key-7')
    expect(cell.text()).toContain('#7')
  })

  it('falls back to Key #id exactly once when the key row is gone', async () => {
    getAPIKeyBreakdown.mockResolvedValue({
      api_keys: [item(7, 10, { key_deleted: true, key_name: '' })],
    })
    const wrapper = mountRanking()
    await flushPromises()

    expect(wrapper.find('[data-testid="api-key-ranking-deleted"]').exists()).toBe(true)
    const cell = wrapper.findAll('tbody tr')[0].findAll('td')[1]
    expect(cell.text()).toContain('Key #7')
    expect(cell.text().match(/#7/g)).toHaveLength(1)

    await wrapper.findAll('tbody tr')[0].trigger('click')
    expect(wrapper.emitted('select-api-key')?.[0]).toEqual([7, 'Key #7'])
  })

  it('hides columns the parent filtered out and keeps cell order aligned with headers', async () => {
    const wrapper = mountRanking({
      visibleColumnKeys: ['key', 'requests', 'actual_cost'],
    })
    await flushPromises()

    // # / 密钥 / 请求数 / 费用——「所属用户」与其余指标列都被隐藏
    const headers = wrapper.findAll('thead th')
    expect(headers).toHaveLength(4)
    expect(wrapper.text()).not.toContain('u1@test.com')

    // 表头与单元格必须一一对应，否则数字会串列
    const cells = wrapper.findAll('tbody tr')[0].findAll('td')
    expect(cells).toHaveLength(4)
    expect(cells[2].text()).toBe('1')      // requests
    expect(cells[3].text()).toContain('$') // actual_cost
  })

  it('does not request while inactive', async () => {
    const wrapper = mountRanking({ active: false })
    await flushPromises()
    expect(getAPIKeyBreakdown).not.toHaveBeenCalled()

    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(getAPIKeyBreakdown).toHaveBeenCalledTimes(1)
  })

  it('keeps the last response when an earlier in-flight request resolves later', async () => {
    let resolveSlow: (v: unknown) => void = () => {}
    getAPIKeyBreakdown
      .mockImplementationOnce(() => new Promise((resolve) => { resolveSlow = resolve }))
      .mockResolvedValueOnce({ api_keys: [item(9, 999)] })

    const wrapper = mountRanking()
    await wrapper.setProps({ startDate: '2026-07-02' })
    await flushPromises()

    resolveSlow({ api_keys: [item(1, 1)] })
    await flushPromises()

    expect(wrapper.text()).toContain('key-9')
    expect(wrapper.text()).not.toContain('key-1')
  })
})
