import { beforeEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import UsageWorkbench from '../components/UsageWorkbench.vue'
import UsageTable from '../components/UsageTable.vue'
import UsageMetrics from '../components/UsageMetrics.vue'
import UsageAccountPicker from '../components/UsageAccountPicker.vue'
import Select from '@/components/common/Select.vue'
import Pagination from '@/components/common/Pagination.vue'
import { formatUsageCost, formatUsageNumber, otherModelCost, nonFableCost } from '../helpers'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import en from '@/i18n/locales/en/vendor'
import zh from '@/i18n/locales/zh/vendor'
import ja from '@/i18n/locales/ja/vendor'
import zhTW from '@/i18n/locales/zh-TW/vendor'
import nativeEn from '@/i18n/locales/en'
import type { UsageRow } from '../types'
const api = vi.hoisted(() => ({
  adminUsage: vi.fn(),
  accountUsage: vi.fn(),
  usage: vi.fn()
}))
vi.mock('../api', () => ({ vendorAPI: api }))
const global = {
  plugins: [
    createI18n({
      legacy: false,
      locale: 'en',
      messages: { en: nativeEn }
    })
  ],
  stubs: {
    teleport: true,
    BaseDialog: { template: '<div class="dialog"><slot /></div>' },
    Pagination: true,
    UsageTrend: true,
    UsageAccountPicker: true
  }
}
beforeEach(() => {
  vi.clearAllMocks()
  api.adminUsage.mockResolvedValue({
    items: [{ vendor_id: 8, account_id: 12, account_name: 'Owned account' }],
    total: 1,
    summary: {}
  })
  api.accountUsage.mockResolvedValue({
    items: [],
    total: 0,
    summary: {},
    fixed_windows: { '5h': { cost_usd: '2.5' } }
  })
})
it('retains the execution-mode filter through report pagination without changing owner scope', async () => {
  const w = mount(UsageWorkbench, { props: { admin: true, vendorId: 8 }, global })
  await flushPromises()
  w.findAllComponents(Select).find(s => s.props('ariaLabel') === en.authorization.executionMode)!.vm.$emit('update:modelValue', 'bps')
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(api.adminUsage.mock.lastCall?.[0]).toMatchObject({ authorization_mode: 'bps', vendor_id: 8, page: 1 })
  w.findAllComponents(Pagination)[0].vm.$emit('update:page', 2)
  await flushPromises()
  expect(api.adminUsage.mock.lastCall?.[0]).toMatchObject({ authorization_mode: 'bps', vendor_id: 8, page: 2 })
  w.unmount()
})
it.each(['request','date','account','vendor','group','account_date'])(
  'omits request authorization mode from %s tables while preserving usage totals', dimension => {
    const w=mount(UsageTable,{props:{dimension,rows:[{account_id:12,account_name:'Combined usage',date:'2026-10-23',authorization_mode:'mixed',requests:3,cost_usd:'1.25'}] as UsageRow[]},global})
    expect(w.findAll('thead th').map(cell=>cell.text())).not.toContain(en.authorization.executionMode)
    expect(w.get('tbody tr').text()).not.toContain(en.authorization.mixed)
    expect(w.get('tbody tr').text()).toContain('$1.25')
    w.unmount()
  }
)
it('paginates models independently from daily rows within the same account snapshot', async () => {
  api.accountUsage.mockImplementation(async (_id, query) => ({
    items: [{ date: `2026-10-0${query.page}`, cost_usd: '2' }],
    total: 2,
    summary: {},
    snapshot_at: '2026-10-16T00:00:00Z',
    watermark: 0,
    model_total: 101,
    model_page: query.model_page,
    model_page_size: 100,
    models: [
      {
        requested_model: query.model_page === 2 ? 'Last model' : 'First models'
      }
    ]
  }))
  const w = mount(UsageWorkbench, { props: { accountId: 12 }, global })
  await flushPromises()
  w.findAllComponents(Pagination)
    .find((p) => p.props('pageSize') !== 100)!
    .vm.$emit('update:page', 2)
  await flushPromises()
  expect(w.text()).toContain('First models')
  expect(api.accountUsage.mock.lastCall?.[1]).toMatchObject({
    page: 2,
    model_page: 1,
    watermark: 0
  })
  w.findAllComponents(Pagination)
    .find((p) => p.props('pageSize') === 100)!
    .vm.$emit('update:page', 2)
  await flushPromises()
  expect(w.text()).toContain('Last model')
  expect(api.accountUsage.mock.lastCall?.[1]).toMatchObject({
    page: 2,
    model_page: 2,
    snapshot_at: '2026-10-16T00:00:00Z',
    watermark: 0
  })
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(api.accountUsage.mock.lastCall?.[1]).toMatchObject({
    page: 1,
    model_page: 1
  })
  w.unmount()
})
it('carries the row supplier into a full historical account detail from all-supplier reporting', async () => {
  const w = mount(UsageWorkbench, { props: { admin: true }, global })
  await flushPromises()
  await w
    .find('[role=group]')
    .findAll('button')
    .find((b) => b.text() === en.usageBy.replace('{dimension}', en.account))!
    .trigger('click')
  await flushPromises()
  await w.find('tbody button').trigger('click')
  await flushPromises()
  expect(api.accountUsage).toHaveBeenCalledWith(
    12,
    expect.objectContaining({
      vendor_id: 8,
      account_id: 12,
      start_date: '',
      end_date: ''
    }),
    expect.any(AbortSignal),
    { admin: true, vendorId: 8, readOnly: false }
  )
  expect(w.find('.dialog').text()).toContain('2.5')
  expect(w.find('.dialog').text()).toContain(en.fixedWindows)
  expect(api.adminUsage).toHaveBeenCalledTimes(2)
  w.unmount()
})
it('does not query unscoped native account details when an aggregate lacks its supplier ID', async () => {
  api.adminUsage.mockResolvedValue({
    items: [{ account_id: 12, account_name: 'Missing supplier', vendor_name: 'Display name only' }],
    total: 1,
    summary: {}
  })
  const w = mount(UsageWorkbench, { props: { admin: true }, global })
  await flushPromises()
  await w
    .find('[role=group]')
    .findAll('button')
    .find((b) => b.text() === en.usageBy.replace('{dimension}', en.account))!
    .trigger('click')
  await flushPromises()
  expect(w.find('[data-usage-scope]').text()).toBe(en.usageSupplier + ': Display name only · ' + en.unknown)
  await w.find('tbody button').trigger('click')
  await flushPromises()
  expect(api.accountUsage).not.toHaveBeenCalled()
  expect(w.find('[role=alert]').text()).toContain(en.selectVendor)
  w.unmount()
})
it('preserves supplier identity in emitted account selections', async () => {
  const w = mount(UsageTable, {
    props: {
      rows: [{ account_id: 12, vendor_id: 8, account_name: 'Account' }]
    },
    global
  })
  await w.find('button').trigger('click')
  expect(w.emitted('account')).toEqual([[12, 8]])
  w.unmount()
})
it.each([
  ['account', 'Owned account'],
  ['account_date', 'Owned account'],
  ['date', '2026-10-15'],
  ['group', 'Coding group'],
  ['model', 'Requested model'],
  ['vendor', 'Supplier eight · #8']
])('labels %s rows by their selected dimension before supplier scope', (dimension, label) => {
  const w = mount(UsageTable, {
    props: {
      dimension,
      rows: [
        {
          account_id: 12,
          account_name: 'Owned account',
          vendor_id: 8,
          vendor_name: 'Supplier eight',
          group_id: 3,
          group_name: 'Coding group',
          date: '2026-10-15',
          requested_model: 'Requested model',
          upstream_model: 'Upstream model'
        }
      ]
    },
    global
  })
  const cell = w.find('tbody td')
  expect(cell.find('button, [data-usage-label]').text()).toBe(label)
  if (dimension === 'vendor') expect(cell.find('[data-usage-scope]').exists()).toBe(false)
  else expect(cell.find('[data-usage-scope]').text()).toBe(en.usageSupplier + ': Supplier eight · #8')
  w.unmount()
})
it('uses group labels before supplier fallback and omits internal source paths from report captions', async () => {
  const row = {
    vendor_id: 8,
    vendor_name: 'Supplier eight',
    group_id: 3,
    group_name: 'Coding group'
  }
  const table = mount(UsageTable, { props: { rows: [row] }, global })
  expect(table.find('[data-usage-label]').text()).toBe('Coding group')
  table.unmount()
  api.adminUsage.mockResolvedValue({
    items: [row],
    total: 1,
    summary: {},
    snapshot_at: '2026-10-15T08:30:00Z',
    timezone: 'Asia/Shanghai',
    source: 'usage_logs.account_stats_cost'
  })
  const w = mount(UsageWorkbench, {
    props: { admin: true, vendorId: 8 },
    global
  })
  await flushPromises()
  expect(
    w.findAll('h3').some((h) => h.text() === en.usageBy.replace('{dimension}', en.vendor))
  ).toBe(true)
  await w
    .find('[role=group]')
    .findAll('button')
    .find((b) => b.text() === en.usageBy.replace('{dimension}', en.group))!
    .trigger('click')
  await flushPromises()
  expect(
    w.findAll('h3').some((h) => h.text() === en.usageBy.replace('{dimension}', en.group))
  ).toBe(true)
  expect(w.find('[data-usage-label]').text()).toBe('Coding group')
  expect(w.text()).not.toContain('usage_logs.account_stats_cost')
  expect(w.text()).not.toContain('2026-10-15T08:30:00Z')
  expect(w.text()).toContain(en.cost)
  w.unmount()
})
it('discards the previous owner request even if invalid draft dates prevent a new report', async () => {
  let resolveOld!: (value: unknown) => void
  api.adminUsage.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveOld = resolve
      })
  )
  const w = mount(UsageWorkbench, {
    props: { admin: true, vendorId: 8 },
    global
  })
  await flushPromises()
  const signal = api.adminUsage.mock.calls[0][1]
  w.getComponent(DateRangePicker).vm.$emit('update:startDate', '2026-02-01')
  w.getComponent(DateRangePicker).vm.$emit('update:endDate', '2026-01-01')
  await w.setProps({ vendorId: 9 })
  expect(signal.aborted).toBe(true)
  resolveOld({
    items: [{ vendor_id: 8, account_id: 12, account_name: 'Previous owner record' }],
    total: 1,
    summary: {}
  })
  await flushPromises()
  expect(w.text()).not.toContain('Previous owner record')
  expect(w.find('[role=alert]').text()).toContain(en.invalidDates)
  expect(
    w
      .findAll('button')
      .find((b) => b.text() === en.exportAll)
      ?.attributes('disabled')
  ).toBeDefined()
  w.unmount()
})
it('keeps the historical catalog independent of draft/report filters and clears owner-specific filters', async () => {
  const w = mount(UsageWorkbench, {
    props: {
      admin: true,
      vendorId: 8,
      groups: [{ id: 3, name: 'Group three', platform: 'openai', is_default: false }]
    },
    global: {
      ...global,
      stubs: { ...global.stubs, UsageAccountPicker: false, teleport: true }
    }
  })
  await flushPromises()
  await w.getComponent(UsageAccountPicker).find('.select-trigger').trigger('click')
  await flushPromises()
  w.getComponent(UsageAccountPicker).vm.$emit('update:modelValue', '12')
  w.getComponent(DateRangePicker).vm.$emit('update:startDate', '2026-01-01')
  w.getComponent(DateRangePicker).vm.$emit('update:endDate', '2026-01-02')
  w.findAllComponents(Select)
    .find((c) => c.props('ariaLabel') === en.group)!
    .vm.$emit('update:modelValue', '3')
  await w.find('form').trigger('submit')
  await flushPromises()
  const reports = () => api.adminUsage.mock.calls.filter(([query]) => !query.all_history)
  const catalogs = () => api.adminUsage.mock.calls.filter(([query]) => query.all_history)
  expect(reports().at(-1)![0]).toEqual(
    expect.objectContaining({
      account_id: '12',
      group_id: '3',
      start_date: '2026-01-01',
      end_date: '2026-01-02',
      vendor_id: 8
    })
  )
  expect(catalogs()).toHaveLength(1)
  expect(catalogs()[0][0]).toEqual({
    all_history: true,
    dimension: 'account',
    page: 1,
    page_size: 20,
    vendor_id: 8
  })
  await w.setProps({ vendorId: 9 })
  await flushPromises()
  expect(reports().at(-1)![0]).toEqual(
    expect.objectContaining({ account_id: '', group_id: '', vendor_id: 9 })
  )
  await w.getComponent(UsageAccountPicker).find('.select-trigger').trigger('click')
  await flushPromises()
  expect(catalogs().at(-1)![0]).toEqual({
    all_history: true,
    dimension: 'account',
    page: 1,
    page_size: 20,
    vendor_id: 9
  })
  expect(w.getComponent(UsageAccountPicker).props('modelValue')).toBe('')
  w.unmount()
})
it('loads supplier totals and two reports with two queries, then pages only the requested report at the same snapshot', async () => {
  const snapshot = '2026-10-15T08:30:00Z'
  api.usage.mockResolvedValue({
    items: [],
    total: 60,
    summary: {},
    snapshot_at: snapshot,
    watermark: 0
  })
  const w = mount(UsageWorkbench, { global })
  await flushPromises()
  expect(api.usage).toHaveBeenCalledTimes(2)
  expect(api.usage.mock.calls.map(([kind]) => kind)).toEqual(['trend', 'accounts'])
  expect(api.usage.mock.calls[1][1]).toEqual(
    expect.objectContaining({
      snapshot_at: snapshot,
      watermark: 0,
      sort_by: 'cost_usd',
      order: 'desc'
    })
  )
  w.getComponent(DateRangePicker).vm.$emit('update:startDate', '2025-01-01')
  w.findAllComponents(Pagination)[1].vm.$emit('update:page', 2)
  await flushPromises()
  expect(api.usage).toHaveBeenCalledTimes(3)
  expect(api.usage.mock.calls[2][0]).toBe('accounts')
  expect(api.usage.mock.calls[2][1]).toEqual(
    expect.objectContaining({ page: 2, snapshot_at: snapshot, watermark: 0 })
  )
  expect(api.usage.mock.calls[2][1].start_date).toBe(api.usage.mock.calls[0][1].start_date)
  expect(api.adminUsage).not.toHaveBeenCalled()
  w.unmount()
})
it('keeps applied filters and the watermark while switching dimension and sorting, including usage records', async () => {
  api.adminUsage.mockResolvedValue({
    items: [{ account_id: 12, vendor_id: 8, account_name: 'Owned account' }],
    total: 1,
    summary: {},
    snapshot_at: '2026-10-15T08:30:00Z',
    watermark: 57
  })
  const w = mount(UsageWorkbench, {
    props: { admin: true, vendorId: 8 },
    global
  })
  await flushPromises()
  const initial = api.adminUsage.mock.calls[0][0]
  w.getComponent(DateRangePicker).vm.$emit('update:startDate', '2025-01-01')
  await w
    .find('[role=group]')
    .findAll('button')
    .find((b) => b.text() === en.usageBy.replace('{dimension}', en.account))!
    .trigger('click')
  await flushPromises()
  expect(api.adminUsage.mock.lastCall![0]).toEqual(
    expect.objectContaining({
      ...initial,
      dimension: 'account',
      watermark: 57,
      snapshot_at: '2026-10-15T08:30:00Z'
    })
  )
  w.getComponent(UsageTable).vm.$emit('sort', 'fable_cost_usd', 'desc')
  await flushPromises()
  expect(api.adminUsage.mock.lastCall![0]).toEqual(
    expect.objectContaining({
      dimension: 'account',
      sort_by: 'fable_cost_usd',
      order: 'desc',
      page: 1,
      start_date: initial.start_date,
      watermark: 57
    })
  )
  await w
    .find('[role=group]')
    .findAll('button')
    .find((b) => b.text() === en.usageRecords)!
    .trigger('click')
  await flushPromises()
  expect(api.adminUsage.mock.lastCall![0]).toEqual(
    expect.objectContaining({
      dimension: 'request',
      sort_by: 'created_at',
      order: 'desc',
      start_date: initial.start_date,
      watermark: 57
    })
  )
  expect(w.text()).toContain(en.usageRecordsHelp)
  expect(w.text()).toContain(en.pendingFilters)
  w.unmount()
})
it('preserves exact USD and integer values and distinguishes unavailable metrics from zero', () => {
  expect(formatUsageCost('9007199254740993.00000001')).toBe('$9,007,199,254,740,993.00000001')
  expect(formatUsageCost('0.00005200')).toBe('$0.000052')
  expect(formatUsageCost('0')).toBe('$0.00')
  expect(formatUsageCost(null)).toBe('—')
  expect(formatUsageCost('invalid')).toBe('—')
  expect(formatUsageCost('9007199254740993.005', 2)).toBe('$9,007,199,254,740,993.01')
  expect(formatUsageCost('0.0049', 2)).toBe('$0.00')
  expect(nonFableCost({cost_usd: '0.3', fable_cost_usd: '0.1'})).toBe('0.2')
  expect(nonFableCost({cost_usd: '0.3', fable_cost_usd: '0.4'})).toBeNull()
  expect(nonFableCost({cost_usd: '0.3'})).toBeNull()
  expect(formatUsageNumber('9007199254740993')).toBe('9,007,199,254,740,993')
  expect(formatUsageNumber(null)).toBe('—')
  expect(otherModelCost({
    cost_usd: '9007199254740993.00000001',
    opus_cost_usd: '9007199254740992.99999999',
    fable_cost_usd: '0.00000001'
  })).toBe('0.00000001')
  expect(otherModelCost({ cost_usd: '0.3', opus_cost_usd: '0.1', fable_cost_usd: '0.2' })).toBe('0.0')
  expect(otherModelCost({ cost_usd: '0', opus_cost_usd: '0', fable_cost_usd: '0' })).toBe('0')
  for (const value of [undefined, null, '', 'invalid', '-1', '1e3']) {
    expect(otherModelCost({ cost_usd: value, opus_cost_usd: '0', fable_cost_usd: '0' })).toBeNull()
    expect(otherModelCost({ cost_usd: '1', opus_cost_usd: value, fable_cost_usd: '0' })).toBeNull()
    expect(otherModelCost({ cost_usd: '1', opus_cost_usd: '0', fable_cost_usd: value })).toBeNull()
  }
  expect(otherModelCost({ cost_usd: '0.3', opus_cost_usd: '0.2', fable_cost_usd: '0.2' })).toBeNull()
})

it.each([false, true])('keeps account report metrics, attribution and distinct archive/deletion states in supplierLayout=%s', async (supplierLayout) => {
  const row = {
    account_id: 12,
    account_name: 'Historical account',
    account_deleted: true,
    requests: 2,
    input_tokens: 100,
    output_tokens: 20,
    total_tokens: 120,
    cache_tokens: 50,
    cache_creation_tokens: 10,
    cache_read_tokens: 40,
    cache_hit_rate: 0.25,
    cost_usd: '123456789012345.6789',
    opus_cost_usd: '5',
    fable_cost_usd: '0.6788'
  }
  const archived = {
    ...row,
    account_id: 13,
    account_name: 'Retained account',
    account_deleted: false,
    account_archived: true,
    account_status: 'error',
    vendor_id: 8,
    vendor_name: 'Supplier eight'
  }
  const w = mount(UsageTable, {
    props: { rows: [row, archived], dimension: 'account', supplierLayout, sortable: true },
    global
  })
  const headers = w.findAll('th').map((th) => th.text())
  expect(headers).toEqual([
    en.account,
    ...(supplierLayout ? [en.status] : [en.opusCost, en.fableCost, en.otherModelCost]),
    en.requests,
    en.tokenConsumption,
    en.cacheTokens,
    en.cacheHit,
    supplierLayout ? en.costShort : en.cost
  ])
  const rows = w.findAll('tbody tr')
  expect(rows[0].text()).toContain(en.deleted)
  expect(rows[0].find('.badge-gray').exists()).toBe(false)
  expect(rows[1].get('.badge-gray').text()).toBe(en.archived)
  expect(rows[1].text()).toContain(en.error)
  expect(rows[1].text()).not.toContain(en.deleted)
  const cells = rows[1].findAll('td')
  const cell = (label: string) => cells[headers.indexOf(label)]
  expect(cell(en.requests).text()).toBe('2')
  expect(cell(en.tokenConsumption).text()).toContain('100')
  expect(cell(en.tokenConsumption).text()).toContain('20')
  expect(cell(en.cacheTokens).text()).toContain('10')
  expect(cell(en.cacheTokens).text()).toContain('40')
  expect(cell(en.cacheHit).text()).toBe('25.0%')
  expect(cell(supplierLayout ? en.costShort : en.cost).text()).toBe(formatUsageCost(row.cost_usd))
  if (!supplierLayout) {
    expect(rows[0].get('[data-usage-scope]').text()).toBe(en.usageSupplier + ': ' + en.unknown)
    expect(rows[1].get('[data-usage-scope]').text()).toBe(en.usageSupplier + ': Supplier eight · #8')
    expect(cell(en.opusCost).text()).toBe('$5.00')
    expect(cell(en.fableCost).text()).toBe('$0.6788')
    expect(cell(en.otherModelCost).text()).toBe('$123,456,789,012,340.0001')
    await w.findAll('th')[headers.indexOf(en.otherModelCost)].trigger('click')
    expect(w.emitted('sort')).toBeUndefined()
    await w.findAll('th')[headers.indexOf(en.fableCost)].trigger('click')
    expect(w.emitted('sort')?.at(-1)?.[0]).toBe('fable_cost_usd')
  }
  await rows[1].get('button').trigger('click')
  expect(w.emitted('account')).toEqual([[13, 8]])
  w.unmount()
})

it.each([false, true])('reconciles summary model costs exactly and keeps missing components unknown in compact=%s', async (compact) => {
  const w = mount(UsageMetrics, {
    props: {
      compact,
      metrics: {
        cost_usd: '9007199254740993.00000001',
        opus_cost_usd: '9007199254740992.99999999',
        fable_cost_usd: '0.00000001'
      }
    },
    global
  })
  const costs = w.get('[data-metric=cost_usd]')
  expect(costs.text()).toContain('$9,007,199,254,740,993.00000001')
  expect(costs.get('[data-cost-family=opus_cost_usd]').text()).toContain('$9,007,199,254,740,992.99999999')
  expect(costs.get('[data-cost-family=fable_cost_usd]').text()).toContain('$0.00000001')
  expect(costs.get('[data-cost-family=other_cost_usd]').text()).toContain('$0.00000001')
  await w.setProps({ metrics: { cost_usd: '5', opus_cost_usd: '0', fable_cost_usd: null } })
  expect(costs.get('[data-cost-family=opus_cost_usd]').text()).toContain('$0.00')
  expect(costs.get('[data-cost-family=fable_cost_usd]').text()).toContain('—')
  expect(costs.get('[data-cost-family=other_cost_usd]').text()).toContain('—')
  w.unmount()
})

it('renders account durations in seconds and keeps absent outcomes distinct from returned zero', async () => {
  const w = mount(UsageMetrics, { props: { metrics: {
    requests: 100, cost_usd: '12.3456', avg_duration_ms: 33200, avg_first_token_ms: 9100,
    successful_requests: null, failed_requests: null, success_rate: null
  } }, global })
  expect(w.get('[data-metric=avg_duration_ms]').text().replace(/\s+/g, ' ')).toContain('33.2 s')
  expect(w.get('[data-metric=avg_first_token_ms]').text().replace(/\s+/g, ' ')).toContain('9.1 s')
  expect(w.get('[data-metric=cost_usd]').text()).toContain('$12.3456')
  expect(w.get('[data-metric=success_rate]').text()).toContain(en.outcomesUnavailable)
  expect(w.get('[data-metric=success_rate]').text()).not.toContain('100%')
  await w.setProps({ metrics: { requests: 0, avg_duration_ms: 0, avg_first_token_ms: null, success_rate: 0 } })
  expect(w.get('[data-metric=avg_duration_ms]').text().replace(/\s+/g, ' ')).toContain('0 s')
  expect(w.get('[data-metric=avg_first_token_ms]').text()).toContain('—')
  expect(w.get('[data-metric=success_rate]').text()).toContain('0.0%')
  w.unmount()
})
it('keeps trend controls available for an empty account and shows no invented result distribution', async () => {
  api.accountUsage.mockResolvedValue({ summary: { requests: 0, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0 }, items: [], trends: [], models: [], results: [{ result: 'unknown', requests: 0 }] })
  const w = mount(UsageWorkbench, { attachTo: document.body, props: { accountId: 12 }, global })
  await flushPromises()
  const chart = w.get('[data-usage-chart]')
  expect(chart.text()).toContain(en.noUsageInPeriod)
  expect(chart.find('usage-trend-stub').exists()).toBe(false)
  await chart.findAll('button')[2].trigger('click')
  expect(chart.findAll('button')[2].attributes('aria-pressed')).toBe('true')
  expect(api.accountUsage).toHaveBeenCalledTimes(1)
  expect(w.get('[data-usage-tokens]').text()).not.toMatch(/NaN|Infinity|100%/)
  expect(w.get('[data-usage-daily]').attributes('open')).toBeUndefined()
  expect(w.findAll('h3').map(h => h.text())).not.toContain(en.results)
  const picker = w.getComponent(DateRangePicker)
  picker.vm.$emit('update:startDate', '2026-10-17')
  picker.vm.$emit('update:endDate', '2026-10-17')
  picker.vm.$emit('change', { startDate: '2026-10-17', endDate: '2026-10-17' })
  await flushPromises()
  expect(api.accountUsage.mock.lastCall?.[1]).toMatchObject({ start_date: '2026-10-17', end_date: '2026-10-17', page: 1, model_page: 1 })
  expect(w.text()).not.toContain(en.pendingFilters)
  expect(document.activeElement).toBe(picker.get('button').element)
  await w.findAll('button').find(b => b.text() === en.allHistory)!.trigger('click')
  await flushPromises()
  expect(api.accountUsage.mock.lastCall?.[1]).toMatchObject({ start_date: '', end_date: '' })
  w.unmount()
})
it('uses requests consistently for model bars and partitions tokens with cache in the scoped report', async () => {
  api.accountUsage.mockResolvedValue({ summary: { requests: 15, input_tokens: 40, output_tokens: 10, cache_creation_tokens: 50, cache_read_tokens: 100 }, items: [], models: [
    { requested_model: 'Model A', requests: 10, cost_usd: '1' },
    { requested_model: 'Model B', requests: 5, cost_usd: '50' }
  ], trends: [{ date: '2026-10-17', requests: 15 }], model_total: 2, model_page_size: 100 })
  const w = mount(UsageWorkbench, { props: { accountId: 12, vendorId: 9, readOnly: true }, global })
  await flushPromises()
  expect(api.accountUsage.mock.lastCall?.[3]).toEqual({ admin: false, vendorId: 9, readOnly: true })
  const bars = w.get('[data-usage-models]').findAll('[aria-hidden=true] > div')
  expect(bars[0].attributes('style')).toContain('width: 100%')
  expect(bars[1].attributes('style')).toContain('width: 50%')
  expect(w.get('[data-usage-tokens]').text()).toContain('50.0%')
  expect(w.find('pagination-stub').exists()).toBe(false)
  expect(w.get('usage-trend-stub').attributes('metric')).toBe('requests')
  await w.get('[data-usage-chart]').findAll('button')[1].trigger('click')
  expect(w.get('usage-trend-stub').attributes('metric')).toBe('total_tokens')
  w.unmount()
})


it('uses compact token labels and model brand colours without fabricated Claude mode/result columns', () => {
  const w = mount(UsageTable, { props: { dimension: 'request', rows: [{ account_id: 12, platform: 'anthropic', requested_model: 'claude-opus-5-5', authorization_mode: 'unknown', result: 'unknown', input_tokens: 26, output_tokens: 172, cache_creation_tokens: 3277, cache_read_tokens: 528642 }] }, global })
  const headers = w.findAll('th').map(h => h.text())
  expect(headers).not.toContain(en.results)
  expect(headers).not.toContain(en.authorization.executionMode)
  expect(w.text()).toContain(en.inputShort + ' 26')
  expect(w.text()).toContain(en.outputShort + ' 172')
  expect(w.text()).toContain(en.cacheWriteShort + ' 3,277')
  expect(w.find('.text-orange-600').text()).toContain('claude-opus-5-5')
})
