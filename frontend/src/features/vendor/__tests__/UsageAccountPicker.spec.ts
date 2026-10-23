import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import UsageAccountPicker from '../components/UsageAccountPicker.vue'
import nativeEn from '@/i18n/locales/en'

const api = vi.hoisted(() => ({ adminUsage: vi.fn(), usage: vi.fn() }))
vi.mock('../api', () => ({ vendorAPI: api }))
const response = (id = 12, page = 1, vendorId = 8) => ({
  items: [
    {
      vendor_id: vendorId,
      account_id: id,
      account_name: 'Archived ' + id,
      account_deleted: true,
    },
  ],
  total: 45,
  page,
  page_size: 20,
  snapshot_at: '2026-10-15T00:00:00Z',
  watermark: 0,
})
function render(admin = true, vendorId: number | undefined = 8) {
  return mount(UsageAccountPicker, {
    props: { admin, vendorId, modelValue: '' },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: nativeEn } }),
      ],
      stubs: { teleport: true },
    },
  })
}
const button = (wrapper: VueWrapper, label: string) =>
  wrapper.findAll('button').find((b) => b.text() === label)!
const open = async (wrapper: VueWrapper) => {
  await wrapper.find('.select-trigger').trigger('click')
  await flushPromises()
}
beforeEach(() => {
  vi.resetAllMocks()
  api.adminUsage.mockResolvedValue(response())
  api.usage.mockResolvedValue(response())
})
afterEach(() => vi.useRealTimers())

it.each([false, true])(
  'distinguishes archived and deleted identities without report filters in admin=%s',
  async (admin) => {
    const historical = response()
    const archived = {
      ...historical.items[0],
      account_id: 13,
      account_name: 'Retained account',
      account_deleted: false,
      account_archived: true,
    }
    const catalog = { ...historical, items: [...historical.items, archived] }
    api.adminUsage.mockResolvedValue(catalog)
    api.usage.mockResolvedValue(catalog)
    const w = render(admin)
    await flushPromises()
    expect(api.adminUsage).not.toHaveBeenCalled()
    expect(api.usage).not.toHaveBeenCalled()
    await open(w)
    const query = {
      all_history: true,
      dimension: 'account',
      page: 1,
      page_size: 20,
      ...(admin ? { vendor_id: 8 } : {}),
    }
    if (admin) {
      expect(api.adminUsage).toHaveBeenCalledTimes(1)
      expect(api.adminUsage).toHaveBeenCalledWith(
        query,
        expect.any(AbortSignal),
      )
      expect(api.usage).not.toHaveBeenCalled()
    } else {
      expect(api.usage).toHaveBeenCalledTimes(1)
      expect(api.usage).toHaveBeenCalledWith(
        'accounts',
        query,
        expect.any(AbortSignal),
      )
      expect(api.adminUsage).not.toHaveBeenCalled()
    }
    const options = w.findAll('[role=option]')
    expect(options[0].text()).toBe(
      'Archived 12 · #12 · ' + nativeEn.vendor.deleted,
    )
    expect(options[1].text()).toBe('Retained account · #13 · ' + nativeEn.vendor.archived)
    await options[1].trigger('click')
    expect(w.emitted('update:modelValue')).toEqual([['13']])
    await w.setProps({ modelValue: '13' })
    expect(w.find('.select-trigger').text()).toContain(nativeEn.vendor.archived)
    expect(w.find('.select-trigger').text()).not.toContain(nativeEn.vendor.deleted)
    w.unmount()
  },
)

it('keeps its own snapshot, zero watermark and selected label while paging', async () => {
  const w = render()
  await flushPromises()
  await open(w)
  await w.find('[role=option]').trigger('click')
  await w.setProps({ modelValue: '12' })
  api.adminUsage.mockResolvedValueOnce(response(33, 2))
  await open(w)
  await button(w, nativeEn.pagination.next).trigger('click')
  await flushPromises()
  expect(api.adminUsage).toHaveBeenLastCalledWith(
    {
      all_history: true,
      dimension: 'account',
      page: 2,
      page_size: 20,
      vendor_id: 8,
      snapshot_at: '2026-10-15T00:00:00Z',
      watermark: 0,
    },
    expect.any(AbortSignal),
  )
  expect(w.find('.select-trigger').text()).toContain('Archived 12')
  expect(w.find('[role=option]').text()).toContain('Archived 33')
  expect(w.emitted('update:modelValue')).toEqual([['12']])
  w.unmount()
})

it('searches exact IDs with fresh pagination and restores the catalog when reopened', async () => {
  vi.useFakeTimers()
  const w = render()
  await flushPromises()
  await open(w)
  api.adminUsage.mockResolvedValueOnce({
    ...response(99),
    total: 1,
    watermark: 123,
  })
  await w.find('.select-search-input').setValue('99')
  await vi.advanceTimersByTimeAsync(350)
  await flushPromises()
  expect(api.adminUsage).toHaveBeenLastCalledWith(
    {
      all_history: true,
      dimension: 'account',
      page: 1,
      page_size: 20,
      vendor_id: 8,
      account_id: 99,
    },
    expect.any(AbortSignal),
  )
  await w.find('[role=option]').trigger('click')
  await w.setProps({ modelValue: '99' })
  await open(w)
  expect(w.find('.select-search-input').element).toHaveProperty('value', '')
  expect(api.adminUsage).toHaveBeenLastCalledWith(
    {
      all_history: true,
      dimension: 'account',
      page: 1,
      page_size: 20,
      vendor_id: 8,
    },
    expect.any(AbortSignal),
  )
  expect(w.find('.select-trigger').text()).toContain('Archived 99')
  w.unmount()
})

it('rejects unsupported name/invalid ID searches and exposes request retry without losing selection', async () => {
  const w = render()
  await flushPromises()
  await open(w)
  await w.find('[role=option]').trigger('click')
  await w.setProps({ modelValue: '12' })
  for (const query of ['account name', '0', '-1', '1.2', '9007199254740992']) {
    w.getComponent(Select).vm.$emit('search', query)
    await flushPromises()
    expect(w.find('[role=alert]').text()).toBe(
      nativeEn.vendor.accountIdSearchInvalid,
    )
  }
  expect(api.adminUsage).toHaveBeenCalledTimes(1)
  api.adminUsage.mockRejectedValueOnce(new Error('Temporary failure'))
  w.getComponent(Select).vm.$emit('search', '40')
  await flushPromises()
  expect(w.find('[role=alert]').text()).toContain('Temporary failure')
  expect(w.find('.select-trigger').text()).toContain('Archived 12')
  await button(w, nativeEn.vendor.retry).trigger('click')
  await flushPromises()
  expect(w.find('[role=alert]').exists()).toBe(false)
  expect(api.adminUsage.mock.calls.at(-1)![0]).toEqual(
    api.adminUsage.mock.calls.at(-2)![0],
  )
  w.unmount()
})

it('clears selection, cached names, search and snapshots before changing supplier and rejects stale results', async () => {
  const w = render()
  await flushPromises()
  await open(w)
  await w.find('[role=option]').trigger('click')
  await w.setProps({ modelValue: '12' })
  let resolveOld!: (value: unknown) => void
  api.adminUsage.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveOld = resolve
      }),
  )
  w.getComponent(Select).vm.$emit('search', '12')
  await flushPromises()
  const oldSignal = api.adminUsage.mock.calls.at(-1)![1]
  api.adminUsage.mockResolvedValueOnce(response(90, 1, 9))
  await w.setProps({ vendorId: 9, modelValue: '' })
  await flushPromises()
  await open(w)
  expect(oldSignal.aborted).toBe(true)
  expect(w.emitted('update:modelValue')?.at(-1)).toEqual([''])
  expect(api.adminUsage).toHaveBeenLastCalledWith(
    {
      all_history: true,
      dimension: 'account',
      page: 1,
      page_size: 20,
      vendor_id: 9,
    },
    expect.any(AbortSignal),
  )
  resolveOld(response(12))
  await flushPromises()
  expect(w.text()).not.toContain('Archived 12')
  expect(w.find('[role=option]').text()).toContain('Archived 90')
  await w.setProps({ admin: false, vendorId: 8 })
  await flushPromises()
  await open(w)
  expect(api.usage).toHaveBeenLastCalledWith(
    'accounts',
    {
      all_history: true,
      dimension: 'account',
      page: 1,
      page_size: 20,
    },
    expect.any(AbortSignal),
  )
  w.unmount()
})

it('supports the superadmin aggregate without inheriting a supplier and rejects mismatched scoped rows', async () => {
  const w = render()
  await w.setProps({ vendorId: undefined })
  await flushPromises()
  await open(w)
  expect(api.adminUsage).toHaveBeenLastCalledWith(
    {
      all_history: true,
      dimension: 'account',
      page: 1,
      page_size: 20,
    },
    expect.any(AbortSignal),
  )
  expect(w.find('[role=option]').text()).toContain(
    nativeEn.vendor.usageSupplier + ' #8',
  )
  api.adminUsage.mockResolvedValueOnce(response(12, 1, 8))
  await w.setProps({ vendorId: 9 })
  await flushPromises()
  await open(w)
  expect(w.find('[role=option]').exists()).toBe(false)
  const count = api.adminUsage.mock.calls.length
  await w.setProps({ vendorId: 0 })
  await flushPromises()
  expect(api.adminUsage).toHaveBeenCalledTimes(count)
  w.unmount()
})
