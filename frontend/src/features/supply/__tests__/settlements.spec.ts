import { beforeEach, describe, it, expect, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import SettlementWorkbench from '../components/SettlementWorkbench.vue'
import { useSupplyStore } from '../store'
import type { Vendor } from '../types'
import Select from '@/shared/ui/Select.vue'
import { statementPeriod } from '../helpers'
import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const en = assembledEnMessages.vendor
const api = vi.hoisted(() => ({
  settlements: vi.fn(),
  statement: vi.fn(),
  list: vi.fn(),
  detail: vi.fn(),
  preview: vi.fn(),
  drafts: vi.fn(),
  statementAction: vi.fn(),
  exportStatement: vi.fn(),
}))
const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
const download = vi.hoisted(() => vi.fn())
vi.mock('@/shared/http', () => ({ apiClient: http }))
vi.mock('../helpers', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../helpers')>()),
  download,
}))
vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  vendorAPI: api,
}))
const actualAPI = await vi.importActual<typeof import('../api')>('../api')
function useRealSettlementAPI() {
  for (const key of ['settlements', 'statement', 'preview', 'drafts', 'statementAction', 'exportStatement'] as const)
    api[key].mockImplementation(actualAPI.vendorAPI[key] as any)
}
const statement = {
  id: 11,
  number: 'S-11',
  vendor_id: 8,
  status: 'pending_payment',
  version: 4,
  paid: '0.00',
  unpaid: '20.50',
  cost_usd: '50.00',
  total: '20.50',
  start_at: '2026-10-11T16:00:00Z',
  end_at: '2026-10-14T16:00:00Z',
  timezone: 'Asia/Shanghai',
  payee: { vendor_name: 'Supplier' },
  payments: [],
  lines: [],
}
function render(
  admin = true,
  initialDraft?: Record<string, unknown>,
  props: { payer?: boolean; vendorId?: number; vendor?: Vendor } = {},
) {
  const pinia = createPinia()
  useSupplyStore(pinia).vendor = {
    id: 4, name: 'Parent', tier: 1, parent_vendor_id: null, default_coefficient: '99', contract_version: 99,
    group_coefficients: { '5': '88' }, groups: [],
  } as any
  return mount(SettlementWorkbench, {
    props: { admin, vendorId: 8, initialDraft: initialDraft as any, ...props },
    global: {
      plugins: [
        pinia,
        createI18n({
          legacy: false,
          locale: 'en',
          messages: { en: { vendor: en } },
        }),
      ],
      stubs: {
        BaseDialog: { template: '<div class="dialog"><slot /></div>' },
        Pagination: true,
        UsageTable: true,
      },
    },
  })
}
const click = async (w: VueWrapper, label: string) => {
  const b = w.findAll('button').find((b) => b.text() === label)
  expect(b).toBeDefined()
  await b!.trigger('click')
  await flushPromises()
}
const field = (w: VueWrapper, label: string) =>
  w
    .findAll('label')
    .find((l) => l.text().startsWith(label))!
    .find('input')
beforeEach(() => {
  vi.clearAllMocks()
  api.settlements.mockResolvedValue({
    items: [statement],
    total: 1,
    page: 1,
    page_size: 20,
  })
  api.statement.mockResolvedValue(statement)
  api.list.mockResolvedValue({ items: [{ id: 8, name: 'Supplier', tier: 1, parent_vendor_id: null }], total: 1 })
  api.detail.mockResolvedValue({ id: 8, name: 'Supplier', tier: 1, parent_vendor_id: null })
  api.preview.mockResolvedValue([
    {
      vendor_id: 8,
      preview_token: 'receipt',
      expires_at: '2099-01-01T00:00:00Z',
      available_count: 1,
      occupied_count: 0,
      statement,
    },
  ])
  api.drafts.mockResolvedValue([{ vendor_id: 8, statement }])
  api.statementAction.mockResolvedValue(statement)
  api.exportStatement.mockResolvedValue(new Blob(['statement csv']))
  http.get.mockResolvedValue({ data: { items: [{ id: 8, name: 'Child', tier: 2, parent_vendor_id: 4 }], total: 1 } })
  http.post.mockResolvedValue({ data: statement })
})
describe('settlement workflow boundaries', () => {
  it.each([
    [
      '2026-10-11T16:00:00Z',
      '2026-10-14T16:00:00Z',
      'Asia/Shanghai',
      '2026-10-12 — 2026-10-14',
    ],
    [
      '2026-03-08T05:00:00Z',
      '2026-03-09T04:00:00Z',
      'America/New_York',
      '2026-03-08 — 2026-03-08',
    ],
    [
      '2026-11-01T04:00:00Z',
      '2026-11-02T05:00:00Z',
      'America/New_York',
      '2026-11-01 — 2026-11-01',
    ],
  ])(
    'renders inclusive calendar dates in %s .. %s / %s',
    (start, end, timezone, expected) => {
      expect(statementPeriod(start, end, timezone)).toBe(expected)
    },
  )
  it('does not substitute the browser timezone or invent dates for invalid statement periods', () => {
    expect(
      statementPeriod(statement.start_at, statement.end_at, undefined),
    ).toBe('—')
    expect(
      statementPeriod(statement.start_at, statement.end_at, 'Invalid/Zone'),
    ).toBe('—')
    expect(
      statementPeriod('not a date', statement.end_at, statement.timezone),
    ).toBe('—')
    expect(
      statementPeriod(statement.end_at, statement.start_at, statement.timezone),
    ).toBe('—')
  })
  it.each([true, false])(
    'shows the same inclusive period in the list and detail (admin=%s)',
    async (admin) => {
      const w = render(admin)
      await flushPromises()
      const period = '2026-10-12 — 2026-10-14'
      expect(w.find('tbody tr').text()).toContain(period)
      expect(w.find('tbody tr').text()).toContain(statement.timezone)
      await click(w, 'S-11')
      expect(w.find('.dialog').text()).toContain(period)
      expect(w.find('.dialog').text()).toContain(statement.timezone)
      expect(w.text()).not.toContain(statement.start_at)
      expect(w.text()).not.toContain(statement.end_at)
      w.unmount()
    },
  )
  it('sends the backend void status while displaying the translated past-tense label', async () => {
    api.settlements.mockResolvedValue({
      items: [{ ...statement, status: 'void' }],
      total: 1,
    })
    const w = render()
    await flushPromises()
    const select = w.getComponent(Select)
    expect(select.props('options')).toContainEqual({
      value: 'void',
      label: en.voided,
    })
    expect(
      select.props('options').some((option) => option.value === 'voided'),
    ).toBe(false)
    expect(w.find('tbody tr').text()).toContain(en.voided)
    expect(w.find('tbody tr').text()).not.toContain(en.void)
    select.vm.$emit('update:modelValue', 'void')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(api.settlements).toHaveBeenLastCalledWith(
      true,
      expect.objectContaining({ status: 'void', vendor_id: 8 }),
      expect.any(AbortSignal),
      false,
    )
    w.unmount()
  })
  it('keeps supplier statements read-only even when opened', async () => {
    const w = render(false)
    await flushPromises()
    expect(w.find('option[value=void]').exists()).toBe(false)
    await click(w, 'S-11')
    const buttons = w.findAll('button').map((b) => b.text())
    for (const key of [
      'newStatement',
      'registerPayment',
      'void',
      'linkedAdjustment',
      'reversePayment',
    ] as const)
      expect(buttons).not.toContain(en[key])
    expect(api.settlements.mock.calls[0][0]).toBe(false)
    expect(api.statement).toHaveBeenCalledWith(false, 11, {
      page: 1,
      page_size: 20,
    }, false, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    w.unmount()
  })
  it('reuses a receipt idempotency key after an uncertain save and never resaves a completed supplier', async () => {
    api.drafts.mockRejectedValueOnce(new Error('Network interrupted'))
    const w = render()
    await flushPromises()
    await click(w, en.newStatement)
    await field(w, en.exchangeRate).setValue('6.7')
    await field(w, en.exchangeSource).setValue('Agreed')
    await w.findAll('form')[1].trigger('submit')
    await flushPromises()
    await click(w, en.save)
    const first = api.drafts.mock.calls[0][0]
    await click(w, en.save)
    expect(api.drafts.mock.calls[1][0]).toEqual(first)
    expect(first[0].idempotency_key).toBeTruthy()
    expect(w.findAll('button').some((b) => b.text() === en.save)).toBe(false)
    w.unmount()
  })
  it('invalidates a preview if any settlement terms change', async () => {
    const w = render()
    await flushPromises()
    await click(w, en.newStatement)
    await w.findAll('form')[1].trigger('submit')
    await flushPromises()
    expect(w.findAll('button').some((b) => b.text() === en.save)).toBe(true)
    await field(w, en.exchangeRate).setValue('7.1')
    expect(w.findAll('button').some((b) => b.text() === en.save)).toBe(false)
    w.unmount()
  })
  it('preserves each supplier contract when previewing across suppliers', async () => {
    const w = render()
    await w.setProps({ vendorId: undefined })
    await flushPromises()
    await click(w, en.newStatement)
    await w.get('fieldset input[type=checkbox]').setValue(true)
    await w.findAll('form')[1].trigger('submit')
    await flushPromises()
    expect(api.preview.mock.calls[0][0].group_coefficients).toBeUndefined()
    expect(api.preview.mock.calls[0][0].contract_version).toBeUndefined()
    w.unmount()
  })
  it('keeps a scoped contract and editable group coefficients on that supplier only', async () => {
    const w = render()
    await w.setProps({
      vendor: {
        id: 8,
        name: 'Supplier',
        tier: 1,
        parent_vendor_id: null,
        contract_version: 3,
        default_coefficient: '1.00000000',
        group_coefficients: { '5': '1.6' },
        groups: [
          { id: 5, name: 'Coding', platform: 'openai', is_default: true },
        ],
      } as any,
    })
    await flushPromises()
    await click(w, en.newStatement)
    expect(api.list).not.toHaveBeenCalled()
    expect(w.findAll('fieldset input[type=checkbox]')).toHaveLength(1)
    const price = field(w, en.coefficient)
    expect((price.element as HTMLInputElement).checkValidity()).toBe(true)
    const exchangeRate = field(w, en.exchangeRate)
    await exchangeRate.setValue('6.1234567890')
    expect((exchangeRate.element as HTMLInputElement).checkValidity()).toBe(true)
    await field(w, 'Coding').setValue('1.23456789')
    await w.findAll('form')[1].trigger('submit')
    await flushPromises()
    expect(api.preview.mock.calls[0][0]).toMatchObject({
      vendor_ids: [8],
      default_coefficient: '1',
      exchange_rate: '6.1234567890',
      group_coefficients: { '5': '1.23456789' },
    })
    expect(w.text()).not.toContain('openai')
    w.unmount()
  })
  it('preserves usage scope when opening the draft form', async () => {
    const w = render(true, {
      account_id: 27,
      group_id: 5,
      platform: 'openai',
      start_date: '2026-09-01',
      end_date: '2026-09-07',
      timezone: 'Asia/Tokyo',
    })
    await flushPromises()
    await w.findAll('form')[1].trigger('submit')
    await flushPromises()
    expect(api.preview).toHaveBeenCalledWith(
      expect.objectContaining({
        vendor_ids: [8],
        account_id: 27,
        group_id: 5,
        platform: 'openai',
        start_date: '2026-09-01',
        end_date: '2026-09-07',
        timezone: 'Asia/Tokyo',
      }),
      false, expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(w.emitted('draftConsumed')).toHaveLength(1)
    w.unmount()
  })
  it('requires offline payment confirmation and sends the displayed version and decimal amount', async () => {
    api.statementAction.mockResolvedValue(statement)
    const w = render()
    await flushPromises()
    await click(w, 'S-11')
    await click(w, en.registerPayment)
    const form = w.findAll('form').at(-1)!
    await form.trigger('submit')
    expect(api.statementAction).not.toHaveBeenCalled()
    await field(w, en.amount).setValue('10.25')
    await field(w, en.channel).setValue('Offline')
    await field(w, en.reference).setValue('receipt-2')
    await form.find('input[type=checkbox]').setValue(true)
    expect(field(w, en.amount).element.checkValidity()).toBe(true)
    await form.trigger('submit')
    await flushPromises()
    expect(api.statementAction).toHaveBeenCalledWith(
      11,
      'payments',
      expect.objectContaining({
        version: 4,
        amount: '10.25',
        channel: 'Offline',
        reference: 'receipt-2',
        idempotency_key: expect.any(String),
      }),
      false, expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    w.unmount()
  })
  it('renders per-supplier preview failures without enabling a false save', async () => {
    api.preview.mockResolvedValue([
      {
        vendor_id: 8,
        error: 'Contract changed',
        available_count: 0,
        occupied_count: 0,
      },
    ])
    const w = render()
    await flushPromises()
    await click(w, en.newStatement)
    await w.findAll('form')[1].trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('Contract changed')
    expect(w.findAll('button').some((b) => b.text() === en.save)).toBe(false)
    w.unmount()
  })
  it('shows the resolved contract but does not offer to save already occupied usage', async () => {
    api.preview.mockResolvedValue([
      {
        vendor_id: 8,
        preview_token: 'empty',
        available_count: 0,
        occupied_count: 6,
        statement: { ...statement, contract_version: 7 },
      },
    ])
    const w = render()
    await flushPromises()
    await click(w, en.newStatement)
    await w.findAll('form')[1].trigger('submit')
    await flushPromises()
    expect(w.text()).toContain(en.contractVersion + ': 7')
    expect(w.findAll('button').some((b) => b.text() === en.save)).toBe(false)
    w.unmount()
  })
})

describe('bilateral settlement scopes', () => {
  it.each([
    { admin: true, path: '/admin/vendors', payeeId: 8, payeeName: 'Tier one', rejectedId: 9 },
    { admin: false, path: '/vendor/vendors', payeeId: 9, payeeName: 'Own child', rejectedId: 10 },
  ])('filters every payee page by tier and ownership before allowing $path previews', async ({ admin, path, payeeId, payeeName, rejectedId }) => {
    api.list.mockImplementation(actualAPI.vendorAPI.list)
    const rows = [
      { id: 10, name: 'Other parent child', tier: 2, parent_vendor_id: 99 },
      { id: 11, name: 'Invalid hierarchy', tier: 1, parent_vendor_id: 4 },
      { id: 8, name: 'Tier one', tier: 1, parent_vendor_id: null },
      { id: 9, name: 'Own child', tier: 2, parent_vendor_id: 4 },
      { id: 12, name: 'Unknown tier' },
    ]
    http.get.mockImplementation(async (url, config) => {
      expect(url).toBe(path)
      const start = (config.params.page - 1) * 2
      return { data: { items: rows.slice(start, start + 2), total: rows.length } }
    })
    const w = render(admin, undefined, { payer: !admin, vendorId: undefined })
    await flushPromises()
    await click(w, en.newStatement)
    expect(http.get).toHaveBeenCalledTimes(3)
    for (const [, config] of http.get.mock.calls) {
      expect(config.params.tier).toBe(admin ? 1 : 2)
      if (admin) expect(config.params).not.toHaveProperty('parent_vendor_id')
      else expect(config.params.parent_vendor_id).toBe(4)
    }
    const choices = w.findAll('fieldset input[type=checkbox]')
    expect(choices).toHaveLength(1)
    expect(w.get('fieldset').text()).toContain(payeeName)
    for (const row of rows.filter((row) => row.id !== payeeId))
      expect(w.get('fieldset').text()).not.toContain(row.name)
    const vm = w.vm as any
    vm.draft.vendor_ids = [rejectedId]
    vm.previews = [{ vendor_id: rejectedId, preview_token: 'wrong-edge', available_count: 1 }]
    await vm.preview()
    await vm.saveDrafts()
    expect(api.preview).not.toHaveBeenCalled()
    expect(api.drafts).not.toHaveBeenCalled()
    await flushPromises()
    expect(w.findAll('button').find((button) => button.text() === en.preview)!.attributes('disabled')).toBeDefined()
    await choices[0].setValue(true)
    vm.draft.vendor_ids = [payeeId]
    await w.findAll('form')[1].trigger('submit')
    await flushPromises()
    expect(api.preview).toHaveBeenCalledWith(expect.objectContaining({ vendor_ids: [payeeId] }), !admin, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    w.unmount()
  })

  it.each([true, false])('blocks root drafts for a tier-two scope before opening a form (metadata supplied=%s)', async (supplied) => {
    const child = { id: 8, name: 'Tier two', tier: 2, parent_vendor_id: 4 } as Vendor
    api.detail.mockResolvedValue(child)
    const w = render(true, supplied ? { account_id: 27 } : undefined, { vendor: supplied ? child : undefined })
    await flushPromises()
    if (!supplied) await click(w, en.newStatement)
    expect(w.text()).toContain(en.selectParent)
    expect(w.find('.dialog').exists()).toBe(false)
    expect(w.findAll('button').map((button) => button.text())).not.toContain(en.newStatement)
    expect(api.detail).toHaveBeenCalledTimes(supplied ? 0 : 1)
    await click(w, 'S-11')
    const vm = w.vm as any
    expect(w.get('.dialog').text()).toContain('20.50')
    for (const key of ['confirmStatement', 'registerPayment', 'void', 'reversePayment', 'linkedAdjustment'] as const)
      expect(w.findAll('button').map((button) => button.text())).not.toContain(en[key])
    await vm.newDraft()
    vm.creating = true
    vm.draft.vendor_ids = [8]
    vm.vendors = [child]
    vm.previews = [{ vendor_id: 8, preview_token: 'wrong-edge', available_count: 1 }]
    await vm.preview()
    await vm.saveDrafts()
    vm.addContractPeriod()
    for (const action of ['confirm', 'void', 'payments', 'reverse', 'adjustments']) {
      vm.openMutation(action, { id: 71, amount: '10' })
      expect(vm.mutation).toBeUndefined()
      vm.mutation = action
      vm.payment = { id: 71, amount: '10' }
      vm.mutationForm.confirmed = true
      await vm.mutate()
      vm.mutation = undefined
    }
    await flushPromises()
    expect(vm.contractPeriods).toEqual([])
    expect(w.findAll('form')).toHaveLength(1)
    expect(api.preview).not.toHaveBeenCalled()
    expect(api.drafts).not.toHaveBeenCalled()
    expect(api.statementAction).not.toHaveBeenCalled()
    expect(http.post).not.toHaveBeenCalled()
    await click(w, en.export)
    expect(api.exportStatement).toHaveBeenCalledWith(true, 11, false, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    w.unmount()
  })

  it('rejects a payer scope belonging to another parent without opening the draft form', async () => {
    http.get.mockResolvedValue({
      data: {
        items: [
          { id: 8, name: 'Other child', tier: 2, parent_vendor_id: 99 },
          { id: 9, name: 'Own child', tier: 2, parent_vendor_id: 4 },
        ], total: 2
      }
    })
    const w = render(false, undefined, { payer: true })
    await flushPromises()
    await click(w, en.newStatement)
    expect(w.text()).toContain(en.unavailable)
    expect(w.find('.dialog').exists()).toBe(false)
    expect((w.vm as any).draft.vendor_ids).toEqual([])
    expect((w.vm as any).vendors).toEqual([])
    expect(api.preview).not.toHaveBeenCalled()
    expect(api.drafts).not.toHaveBeenCalled()
    expect(http.get.mock.calls.every(([url]) => url === '/vendor/vendors')).toBe(true)
    w.unmount()
  })

  it.each([
    { admin: true, payer: false, payerId: 4 },
    { admin: false, payer: true, payerId: null },
    { admin: false, payer: true, payerId: 99 },
  ])('keeps a statement for another payer read-only (admin=$admin, statement payer=$payerId)', async ({ admin, payer, payerId }) => {
    api.statement.mockResolvedValue({ ...statement, payer_vendor_id: payerId, payments: [{ id: 71, amount: '10' }] })
    const w = render(admin, undefined, { payer })
    await flushPromises()
    await click(w, 'S-11')
    const vm = w.vm as any
    for (const key of ['confirmStatement', 'registerPayment', 'void', 'reversePayment', 'linkedAdjustment'] as const)
      expect(w.findAll('button').map((button) => button.text())).not.toContain(en[key])
    vm.openMutation('void')
    expect(vm.mutation).toBeUndefined()
    vm.mutation = 'void'
    await vm.mutate()
    expect(api.statementAction).not.toHaveBeenCalled()
    expect(w.get('.dialog').text()).toContain('20.50')
    w.unmount()
  })

  it('closes root dialogs and discards pending detail metadata when the scope becomes tier two', async () => {
    const root = { id: 8, name: 'Tier one', tier: 1, parent_vendor_id: null } as Vendor
    const child = { id: 9, name: 'Tier two', tier: 2, parent_vendor_id: 4 } as Vendor
    const w = render(true, undefined, { vendor: root })
    await flushPromises()
    await click(w, 'S-11')
    await click(w, en.registerPayment)
    await w.setProps({ vendorId: 9, vendor: child })
    expect(w.find('.dialog').exists()).toBe(false)
    expect(w.text()).toContain(en.selectParent)
    expect((w.vm as any).mutation).toBeUndefined()
    await w.setProps({ vendorId: 8, vendor: undefined })
    let finish!: (value: unknown) => void
    api.detail.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    const pending = (w.vm as any).newDraft()
    expect(w.find('.dialog').exists()).toBe(false)
    await w.setProps({ vendorId: 9, vendor: child })
    finish(root)
    await pending
    await flushPromises()
    expect(w.find('.dialog').exists()).toBe(false)
    expect(w.text()).toContain(en.selectParent)
    expect((w.vm as any).vendors).toEqual([])
    expect((w.vm as any).draft.vendor_ids).toEqual([9])
    expect(api.preview).not.toHaveBeenCalled()
    expect(api.drafts).not.toHaveBeenCalled()
    w.unmount()
  })

  it.each([
    { admin: true, payer: false, path: '/admin/vendors/settlements' },
    { admin: true, payer: true, path: '/admin/vendors/settlements' },
    { admin: false, payer: true, path: '/vendor/payables' },
    { admin: false, payer: false, path: '/vendor/settlements' },
  ])('uses the real list, detail and export endpoints for $path (admin=$admin, payer=$payer)', async ({ admin, payer, path }) => {
    useRealSettlementAPI()
    const csv = new Blob(['historical snapshot'])
    const snapshot = {
      ...statement, payer_vendor_id: payer && !admin ? 4 : null, contract_version: 3,
      lines: [{ group_name: 'Original contract', coefficient: '1.25', cost_usd: '50', amount: '9.33' }],
    }
    http.get.mockImplementation(async (url) => ({
      data: url.endsWith('/export') ? csv : url.endsWith('/11') ? snapshot : { items: [snapshot], total: 1 },
    }))
    const w = render(admin, undefined, {
      payer,
      vendor: { id: 8, tier: admin ? 1 : 2, parent_vendor_id: admin ? null : 4, contract_version: 99, default_coefficient: '99', group_coefficients: { '5': '99' } } as any,
    })
    await flushPromises()
    const query = http.get.mock.calls[0][1].params
    expect(http.get.mock.calls[0][0]).toBe(path)
    if (admin || payer) expect(query.vendor_id).toBe(8)
    else expect(query).not.toHaveProperty('vendor_id')
    await click(w, 'S-11')
    expect(http.get).toHaveBeenCalledWith(path + '/11', expect.objectContaining({ params: { page: 1, page_size: 20 }, }))
    expect(w.get('.dialog').text()).toContain('Original contract')
    expect(w.get('.dialog').text()).toContain('1.25')
    expect((w.vm as any).detail.contract_version).toBe(3)
    expect((w.vm as any).detail.payer_vendor_id).toBe(snapshot.payer_vendor_id)
    expect(api.detail).not.toHaveBeenCalled()
    await click(w, en.export)
    expect(http.get).toHaveBeenCalledWith(path + '/11/export', expect.objectContaining({ responseType: 'blob', timeout: 90000 }))
    expect(download).toHaveBeenCalledWith(csv, 'settlement-11.csv')
    expect(http.post).not.toHaveBeenCalled()
    expect(http.get.mock.calls.every(([url]) => url.startsWith(path))).toBe(true)
    w.unmount()
  })

  it('loads only direct children and leaves each selected child contract to the payables preview endpoint', async () => {
    useRealSettlementAPI()
    const children = [
      { id: 8, name: 'Child A', tier: 2, parent_vendor_id: 4, default_coefficient: '1.25', contract_version: 3 },
      { id: 9, name: 'Child B', tier: 2, parent_vendor_id: 4, default_coefficient: '2.50', contract_version: 7 },
    ]
    http.get.mockImplementation(async (url, config) => {
      if (url === '/vendor/vendors') return { data: { items: [children[config.params.page - 1]], total: 2 } }
      if (url === '/vendor/payables') return { data: { items: [statement], total: 1 } }
      throw new Error('Unexpected read: ' + url)
    })
    http.post.mockImplementation(async (url, body) => {
      if (url === '/vendor/payables/preview') {
        const child = children.find((vendor) => vendor.id === body.vendor_ids[0])!
        return {
          data: [{
            vendor_id: child.id, preview_token: 'receipt-' + child.id, expires_at: '2099-01-01T00:00:00Z',
            available_count: 1, occupied_count: 0,
            statement: {
              ...statement, contract_version: child.contract_version, payee: { vendor_name: child.name },
              lines: [{ group_name: 'Child contract', coefficient: child.default_coefficient, cost_usd: '50', amount: '9.33' }]
            },
          }]
        }
      }
      if (url === '/vendor/payables') return { data: [{ vendor_id: 9, statement }] }
      throw new Error('Unexpected mutation: ' + url)
    })
    const w = render(false, undefined, { payer: true, vendorId: undefined })
    await flushPromises()
    expect(w.text()).toContain(en.downstreamPayables)
    await click(w, en.newStatement)
    expect(http.get).toHaveBeenCalledWith('/vendor/vendors', expect.objectContaining({ params: { page: 2, page_size: 100, tier: 2, parent_vendor_id: 4 }, }))
    expect(w.get('fieldset').text()).toContain('Child A')
    expect(w.get('fieldset').text()).toContain('Child B')
    expect(w.findAll('label').some((label) => [en.coefficient, en.contractVersion].includes(label.text()))).toBe(false)
    expect(w.text()).not.toContain(en.contractPeriods)
    const vm = w.vm as any
    // Even stale editable root terms cannot become payer overrides or identity fields.
    vm.draft.default_coefficient = '99'
    vm.draft.contract_version = 99
    vm.draft.payer_vendor_id = 999
    vm.coefficients['5'] = '88'
    vm.contractPeriods = [{ start_date: '2026-10-01', end_date: '2026-10-02', contract_version: 99, default_coefficient: '77', group_coefficients: {} }]
    await field(w, en.exchangeRate).setValue('6.7')
    await field(w, en.exchangeSource).setValue('Agreed')
    const checkboxes = w.findAll('fieldset input[type=checkbox]')
    await checkboxes[0].setValue(true)
    await w.findAll('form')[1].trigger('submit')
    await flushPromises()
    expect(w.get('.dialog').text()).toContain('50 USD × 1.25 CNY/USD')
    await checkboxes[0].setValue(false)
    await checkboxes[1].setValue(true)
    expect(w.get('.dialog').text()).not.toContain('50 USD × 1.25 CNY/USD')
    await w.findAll('form')[1].trigger('submit')
    await flushPromises()
    expect(w.get('.dialog').text()).toContain('50 USD × 2.5 CNY/USD')
    const previewCalls = http.post.mock.calls.filter(([url]) => url === '/vendor/payables/preview')
    expect(previewCalls.map(([, body]) => body.vendor_ids)).toEqual([[8], [9]])
    for (const [, body] of previewCalls) {
      expect(body).toMatchObject({ exchange_rate: '6.7', exchange_rate_source: 'Agreed', coefficient_unit: 'CNY/USD', settlement_currency: 'USDT' })
      for (const key of ['default_coefficient', 'group_coefficients', 'contract_version', 'contract_periods', 'payer_vendor_id'])
        expect(body).not.toHaveProperty(key)
    }
    await click(w, en.save)
    expect(http.post).toHaveBeenLastCalledWith('/vendor/payables', {
      drafts: [{ preview_token: 'receipt-9', idempotency_key: expect.any(String) }],
    }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect([...http.get.mock.calls, ...http.post.mock.calls].every(([url]) => !url.startsWith('/admin'))).toBe(true)
    expect(api.list).not.toHaveBeenCalled()
    expect(api.detail).not.toHaveBeenCalled()
    w.unmount()
  })

  it.each([
    ['confirm', 'draft', en.confirmStatement],
    ['void', 'pending_payment', en.void],
    ['payments', 'pending_payment', en.registerPayment],
    ['payments/71/reverse', 'pending_payment', en.reversePayment],
    ['adjustments', 'paid', en.linkedAdjustment],
  ])('sends payer action %s to payables with the displayed statement version', async (action, status, label) => {
    useRealSettlementAPI()
    const snapshot = { ...statement, status, payer_vendor_id: 4, payments: [{ id: 71, amount: '10.25', paid_at: '2026-10-15', channel: 'Offline', reference: 'receipt' }] }
    http.get.mockImplementation(async (url) => ({ data: url.endsWith('/11') ? snapshot : { items: [snapshot], total: 1 } }))
    http.post.mockResolvedValue({ data: snapshot })
    const w = render(false, undefined, { payer: true })
    await flushPromises()
    await click(w, 'S-11')
    await click(w, label)
    const vm = w.vm as any
    Object.assign(vm.mutationForm, { amount: '10.25', reason: 'Correction', channel: 'Offline', reference: 'receipt-2', confirmed: true })
    await w.findAll('form').at(-1)!.trigger('submit')
    await flushPromises()
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(http.post).toHaveBeenCalledWith('/vendor/payables/11/' + action, expect.objectContaining({ version: 4 }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    const body = http.post.mock.calls[0][1]
    expect(body).not.toHaveProperty('payer_vendor_id')
    if (action !== 'confirm' && action !== 'void') expect(body).toMatchObject({ amount: '10.25', idempotency_key: expect.any(String) })
    expect([...http.get.mock.calls, ...http.post.mock.calls].every(([url]) => !url.startsWith('/admin'))).toBe(true)
    w.unmount()
  })

  it('guards receivable mutations even if called directly, while preserving read-only details and exports', async () => {
    const w = render(false)
    await flushPromises()
    await click(w, 'S-11')
    const vm = w.vm as any
    expect(w.text()).toContain(en.upstreamReceivables)
    await vm.newDraft()
    vm.creating = true
    await vm.preview()
    await vm.saveDrafts()
    vm.addContractPeriod()
    for (const action of ['confirm', 'void', 'payments', 'reverse', 'adjustments']) {
      vm.openMutation(action, { id: 71, amount: '10' })
      expect(vm.mutation).toBeUndefined()
      vm.mutation = action
      vm.mutationForm.confirmed = true
      await vm.mutate()
      vm.mutation = undefined
    }
    await flushPromises()
    expect(vm.contractPeriods).toEqual([])
    expect(w.findAll('form')).toHaveLength(1)
    expect(api.preview).not.toHaveBeenCalled()
    expect(api.drafts).not.toHaveBeenCalled()
    expect(api.statementAction).not.toHaveBeenCalled()
    expect(api.list).not.toHaveBeenCalled()
    expect(http.get).not.toHaveBeenCalled()
    w.unmount()
  })

  it('never falls back to admin vendor reads when payer identity is unavailable', async () => {
    const w = render(false, undefined, { payer: true })
    await flushPromises()
    useSupplyStore().clear()
    await flushPromises()
    expect(w.findAll('button').map((button) => button.text())).not.toContain(en.newStatement)
    await (w.vm as any).newDraft()
    expect(w.text()).toContain(en.unavailable)
    expect(api.list).not.toHaveBeenCalled()
    expect(api.detail).not.toHaveBeenCalled()
    expect(http.get).not.toHaveBeenCalled()
    expect(w.find('.dialog').exists()).toBe(false)
    w.unmount()
  })

  it('drops a previous child picker response after vendor scope changes', async () => {
    let finish!: (value: unknown) => void
    http.get.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    const w = render(false, undefined, { payer: true })
    await flushPromises()
    const pending = (w.vm as any).newDraft()
    await w.setProps({ vendorId: 9 })
    finish({ data: { items: [{ id: 8, name: 'Old child' }], total: 1 } })
    await pending
    await flushPromises()
    expect((w.vm as any).vendors).toEqual([])
    expect(w.find('.dialog').exists()).toBe(false)
    expect(api.settlements).toHaveBeenLastCalledWith(false, expect.objectContaining({ vendor_id: 9 }), expect.any(AbortSignal), true)
    w.unmount()
  })

  it('resets root contract overrides when the selected supplier changes before its prop snapshot arrives', async () => {
    const w = render(true, undefined, {
      vendor: { id: 8, name: 'Old supplier', tier: 1, parent_vendor_id: null, contract_version: 3, default_coefficient: '1.25', group_coefficients: { '5': '1.1' }, groups: [{ id: 5, name: 'Old group' }] } as any,
    })
    await flushPromises()
    await click(w, en.newStatement)
    const vm = w.vm as any
    vm.addContractPeriod()
    api.detail.mockResolvedValueOnce({ id: 9, name: 'New supplier', tier: 1, parent_vendor_id: null, contract_version: 7, default_coefficient: '2.50' })
    await w.setProps({ vendorId: 9 })
    await flushPromises()
    await click(w, en.newStatement)
    expect(w.get('.dialog').text()).not.toContain('Old group')
    expect(vm.contractPeriods).toEqual([])
    await vm.preview()
    expect(api.preview).toHaveBeenLastCalledWith(expect.objectContaining({
      vendor_ids: [9], default_coefficient: '2.5', contract_version: 7,
    }), false, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(api.preview.mock.calls[0][0].group_coefficients).toBeUndefined()
    expect(api.preview.mock.calls[0][0]).not.toHaveProperty('contract_periods')
    w.unmount()
  })

  it('does not let late payable details replace or unlock a receivable request', async () => {
    const finish: ((value: unknown) => void)[] = []
    api.statement.mockImplementation(() => new Promise((resolve) => finish.push(resolve)))
    const w = render(false, undefined, { payer: true })
    await flushPromises()
    const vm = w.vm as any
    const payable = vm.openStatement(11)
    await w.setProps({ payer: false })
    const receivable = vm.openStatement(22)
    finish[0]({ ...statement, number: 'OLD-PAYABLE' })
    await payable
    expect(vm.detail).toBeUndefined()
    expect(vm.busy).toBe(true)
    finish[1]({ ...statement, id: 22, number: 'CURRENT-RECEIVABLE' })
    await receivable
    await flushPromises()
    expect(w.get('.dialog').text()).not.toContain('OLD-PAYABLE')
    expect(vm.detail.id).toBe(22)
    expect(vm.busy).toBe(false)
    expect(w.findAll('button').map((button) => button.text())).not.toContain(en.registerPayment)
    w.unmount()
  })

  it('invalidates an in-flight preview when the selected child changes', async () => {
    let finish!: (value: unknown) => void
    api.preview.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    const w = render(false, undefined, { payer: true })
    await flushPromises()
    await click(w, en.newStatement)
    const vm = w.vm as any
    const pending = vm.preview()
    vm.draft.vendor_ids = [9]
    finish([{ vendor_id: 8, preview_token: 'OLD-CHILD', available_count: 1, statement }])
    await pending
    await flushPromises()
    expect(vm.previews).toEqual([])
    expect(w.findAll('button').map((button) => button.text())).not.toContain(en.save)
    expect(vm.busy).toBe(false)
    w.unmount()
  })

  it.each(['preview', 'save', 'mutation', 'export'])('ignores a late %s after switching from payable management to receivables', async (operation) => {
    const w = render(false, undefined, { payer: true })
    await flushPromises()
    const vm = w.vm as any
    let finish!: (value: unknown) => void
    let pending: Promise<void>
    if (operation === 'preview' || operation === 'save') {
      await vm.newDraft()
      if (operation === 'save') await vm.preview()
      const target = operation === 'preview' ? api.preview : api.drafts
      target.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
      pending = operation === 'preview' ? vm.preview() : vm.saveDrafts()
    } else if (operation === 'mutation') {
      api.statement.mockResolvedValueOnce({ ...statement, payer_vendor_id: 4 })
      await vm.openStatement(11)
      vm.openMutation('void')
      api.statementAction.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
      pending = vm.mutate()
    } else {
      api.exportStatement.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
      pending = vm.exportStatement(11)
    }
    await w.setProps({ payer: false })
    finish(operation === 'export' ? new Blob(['OLD-PAYABLE']) : operation === 'mutation' ? statement : [])
    await pending
    await flushPromises()
    expect(vm.previews).toEqual([])
    expect(vm.draftResults).toEqual([])
    expect(vm.actionError).toBe('')
    expect(vm.busy).toBe(false)
    expect(w.find('.dialog').exists()).toBe(false)
    expect(download).not.toHaveBeenCalled()
    expect(api.settlements).toHaveBeenCalledTimes(2)
    w.unmount()
  })
})

it('shows one payment rate and explicit USD to CNY to USDT operands', async () => {
  api.preview.mockResolvedValue([{
    vendor_id: 8, preview_token: 'receipt', expires_at: '2099-01-01T00:00:00Z', available_count: 1, occupied_count: 0,
    statement: { ...statement, exchange_rate: '6.7', cost_usd: '100', total: '23.88', lines: [{ group_name: 'Claude', cost_usd: '100', coefficient: '1.6', amount: '23.88' }] }
  }])
  const w = render(true)
  await flushPromises()
  await click(w, en.newStatement)
  expect(w.findAll('[data-testid="settlement-exchange-rate"]')).toHaveLength(1)
  const vm = w.vm as any
  vm.draft.start_date = '2026-10-01'
  vm.draft.end_date = '2026-10-02'
  vm.draft.exchange_rate = '6.7'
  vm.draft.exchange_rate_source = 'agreed payment rate'
  await w.findAll('form')[1].trigger('submit')
  await flushPromises()
  expect(w.text()).toMatch(/100 USD × 1.6 CNY\/USD\s+÷ 6.7 CNY\/USDT = 23.88 USDT/)
  expect(api.preview.mock.calls[0][0]).toMatchObject({ exchange_rate: '6.7', settlement_currency: 'USDT' })
  w.unmount()
})

it('identifies the preserved historical multiplier basis without changing amounts', async () => {
  api.statement.mockResolvedValue({ ...statement, cost_basis: 'COALESCE(account_stats_cost,total_cost)*COALESCE(account_rate_multiplier,1)' })
  const w = render(true)
  await flushPromises()
  await click(w, 'S-11')
  expect(w.get('[data-testid="legacy-cost-basis"]').text()).toBe(en.legacySettlementCost)
  expect(w.get('.dialog').text()).toContain('20.50')
  w.unmount()
})

it('prefills a supplier settlement rate and keeps the payment rate editable', async () => {
  api.detail.mockResolvedValue({ id: 8, name: 'Supplier', tier: 1, parent_vendor_id: null, default_exchange_rate: '6.7000000001' })
  const w = render(true)
  await flushPromises()
  await click(w, en.newStatement)
  expect((w.get('[data-testid="settlement-exchange-rate"]').element as HTMLInputElement).value).toBe('6.7000000001')
  await w.get('[data-testid="settlement-exchange-rate"]').setValue('7.1')
  await w.findAll('form')[1].trigger('submit')
  await flushPromises()
  expect(api.preview.mock.calls[0][0].exchange_rate).toBe('7.1')
  w.unmount()
})

vi.mock('@/features/supply/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/supply/services')>('@/features/supply/services')
  const { createSupplyTestServices } = await import('@/features/supply/__tests__/services')
  const services = createSupplyTestServices()
  return { ...actual, useSupplyServices: () => services }
})
