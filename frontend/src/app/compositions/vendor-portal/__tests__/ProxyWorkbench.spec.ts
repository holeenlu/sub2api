import Select from '@/shared/ui/Select.vue'
import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const nativeEn = assembledEnMessages
const en = assembledEnMessages.vendor
import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia } from 'pinia'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ProxyWorkbench from '../components/ProxyWorkbench.vue'

import type { ResourceScope } from '@/features/supply/types'
const api = vi.hoisted(() => ({
  proxies: vi.fn(),
  proxy: vi.fn(),
  deleteProxies: vi.fn(),
  testProxy: vi.fn(),
  proxyExport: vi.fn(),
  scope: vi.fn()
}))
const download = vi.hoisted(() => vi.fn())
const copyToClipboard = vi.hoisted(() => vi.fn())
vi.mock('@/features/supply/helpers', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/supply/helpers')>()),
  download
}))
vi.mock('@/shared/lib/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
const notices = vi.hoisted(() => ({
  showSuccess: vi.fn(),
  showError: vi.fn(),
  showInfo: vi.fn(),
  showWarning: vi.fn()
}))
vi.mock('@/app/model/app', () => ({ useAppStore: () => notices }))
vi.mock('@/features/supply/api', () => ({
  resourceAPI: (scope: ResourceScope) => {
    api.scope(scope)
    return api
  }
}))
const qualityReport = {
  proxy_id: 1, grade: 'A', score: 100, checked_at: 1720000000,
  passed_count: 3, warn_count: 0, failed_count: 0, challenge_count: 0,
  summary: '通过 3 项', exit_ip: '192.0.2.10', country: 'US', base_latency_ms: 64,
  items: [
    { target: 'base_connectivity', status: 'pass', latency_ms: 64, message: 'Target reachable' },
    { target: 'openai', status: 'pass', http_status: 401, latency_ms: 93, message: 'Target reachable' },
    { target: 'anthropic', status: 'pass', http_status: 405, latency_ms: 61, message: 'Target reachable' }
  ]
}
const proxies = [1, 2, 3].map((id) => ({
  id,
  name: 'Proxy ' + id,
  protocol: 'http',
  host: 'proxy.invalid',
  port: 80,
  status: 'active',
  config_version: 2
}))
const render = (scope: ResourceScope = { admin: false }) =>
  mount(ProxyWorkbench, {
    props: { scope },
    global: {
      plugins: [
        createPinia(),
        createI18n({
          legacy: false,
          locale: 'en',
          messages: { en: nativeEn }
        })
      ],
      stubs: {
        teleport: true,
        BaseDialog: {
          name: 'BaseDialog',
          props: ['width'],
          template: '<div class="dialog"><slot /><slot name="footer" /></div>'
        },
        Pagination: true,
        ProxyForm: true,
        ProxyImport: true
      }
    }
  })
it.each([
  { admin: false, children: true, readOnly: true },
  { admin: true, vendorId: 0, readOnly: true }
])('shows proxy owner and uses its scope for details %j', async scope => {
  api.proxies.mockResolvedValue({ items: [{ ...proxies[0], vendor_id: 23, vendor_name: 'Owner twenty-three' }], total: 1 })
  const w = render(scope)
  await flushPromises()
  expect(w.get('[data-resource-owner]').text()).toContain('Owner twenty-three')
  expect(w.get('[data-resource-owner]').text()).toContain('#23')
  await w.findAll('button').find(b => b.text() === 'Proxy 1')!.trigger('click')
  await flushPromises()
  expect(api.scope).toHaveBeenLastCalledWith({ admin: scope.admin, readOnly: true, vendorId: 23 })
  expect(w.findAll('input[type=checkbox]')).toHaveLength(0)
  await w.get('[data-resource-owner] button').trigger('click')
  expect(w.emitted('selectVendor')).toEqual([[23]])
  w.unmount()
})
beforeEach(() => {
  vi.clearAllMocks()
  api.testProxy.mockResolvedValue({ status: 'success', test: { latency_ms: 42 } })
  api.proxy.mockImplementation(async (id) => ({
    ...proxies.find((p) => p.id === id),
    references: {
      accounts: [101],
      backups: [202, 203],
      oauth: 2,
      inflight: 0,
      total: 5
    }
  }))
  api.deleteProxies.mockResolvedValue({
    items: [
      { id: 1, status: 'deleted' },
      { id: 2, status: 'failed', code: 'PROXY_IN_USE' }
    ]
  })
  api.proxies.mockImplementation(async (q) => ({
    items:
      q.page_size === 50
        ? q.page === 1
          ? proxies.slice(0, 2)
          : proxies.slice(2)
        : proxies.slice(0, 1),
    total: 3,
    page: q.page,
    page_size: q.page_size
  }))
})

it.each([{ admin: false }, { admin: true, vendorId: 8 }])(
  'shows only scoped safe reference IDs and deletes the reviewed selection in %j',
  async (scope) => {
    api.proxies.mockResolvedValue({ items: proxies, total: 3 })
    api.proxy.mockImplementation(async (id) => ({
      ...proxies.find((p) => p.id === id),
      username: 'PRIVATE-USERNAME',
      password: 'PRIVATE-PASSWORD',
      references: {
        accounts: [101],
        backups: [202, 203],
        oauth: 2,
        inflight: 0,
        total: 5
      }
    }))
    const w = render(scope)
    await flushPromises()
    await w
      .findAll('button')
      .find((b) => b.text() === 'Proxy 1')!
      .trigger('click')
    await flushPromises()
    expect(w.find('.dialog').text()).toContain('#101')
    expect(w.find('.dialog').text()).toContain('#202, #203')
    expect(w.find('.dialog').html()).not.toContain('PRIVATE-')
    w.findComponent({ name: 'BaseDialog' }).vm.$emit('close')
    await flushPromises()
    await w.get('tbody input[aria-label="' + en.select + ' Proxy 1"]').setValue(true)
    await w.get('tbody input[aria-label="' + en.select + ' Proxy 2"]').setValue(true)
    await w
      .findAll('button')
      .find((b) => b.text() === en.delete)!
      .trigger('click')
    await flushPromises()
    const preview = w.get('[data-delete-references]')
    expect(preview.findAll('tbody tr')).toHaveLength(2)
    expect(preview.text()).toContain('#101')
    expect(preview.text()).toContain('#202, #203')
    expect(preview.html()).not.toContain('PRIVATE-')
    expect(api.deleteProxies).not.toHaveBeenCalled()
    expect(api.proxy.mock.calls.map((call) => call[0])).toEqual([1, 1, 2])
    expect(
      api.scope.mock.calls.every(
        ([value]) => value.admin === scope.admin && value.vendorId === scope.vendorId
      )
    ).toBe(true)
    // Selection changes cannot silently change the IDs reviewed in the confirmation.
    await w.get('tbody input[aria-label="' + en.select + ' Proxy 1"]').setValue(false)
    await w
      .findAll('button')
      .find((b) => b.text() === en.confirm)!
      .trigger('click')
    await flushPromises()
    expect(api.deleteProxies).toHaveBeenCalledWith([1, 2], expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(w.find('[data-delete-references]').exists()).toBe(false)
    expect(w.text()).toContain('PROXY_IN_USE')
    expect(w.text()).toContain(en.success)
    expect(w.text()).toContain(en.failed)
    w.unmount()
  }
)

it('blocks deletion when safe references are missing and allows a read-only retry', async () => {
  api.proxy.mockResolvedValueOnce(proxies[0])
  const w = render()
  await flushPromises()
  await w.get('tbody input[type=checkbox]').setValue(true)
  await w
    .findAll('button')
    .find((b) => b.text() === en.delete)!
    .trigger('click')
  await flushPromises()
  expect(w.get('[data-delete-references] [role=alert]').text()).toBe(en.referencesUnavailable)
  expect(
    w
      .findAll('button')
      .find((b) => b.text() === en.confirm)!
      .attributes('disabled')
  ).toBeDefined()
  expect(api.deleteProxies).not.toHaveBeenCalled()
  await w
    .findAll('button')
    .find((b) => b.text() === en.retry)!
    .trigger('click')
  await flushPromises()
  expect(
    w
      .findAll('button')
      .find((b) => b.text() === en.confirm)!
      .attributes('disabled')
  ).toBeUndefined()
  expect(w.get('[data-delete-references]').text()).toContain('#101')
  expect(api.proxy).toHaveBeenCalledTimes(2)
  expect(api.deleteProxies).not.toHaveBeenCalled()
  w.unmount()
})

it.each(['close', 'scope', 'readOnly'])(
  'cancels queued reference reads and ignores late results after %s',
  async (operation) => {
    api.proxies.mockResolvedValue({ items: proxies, total: 3 })
    const finish: ((value: unknown) => void)[] = []
    api.proxy.mockImplementation(() => new Promise((resolve) => finish.push(resolve)))
    const w = render({ admin: true, vendorId: 8 })
    await flushPromises()
    await w.get('thead input[type=checkbox]').setValue(true)
    await w
      .findAll('button')
      .find((b) => b.text() === en.delete)!
      .trigger('click')
    await flushPromises()
    expect(api.proxy.mock.calls.map((call) => call[0])).toEqual([1, 2])
    expect(
      w
        .findAll('button')
        .find((b) => b.text() === en.confirm)!
        .attributes('disabled')
    ).toBeDefined()
    if (operation === 'close') w.findComponent({ name: 'BaseDialog' }).vm.$emit('close')
    else await w.setProps({ scope: { admin: true, vendorId: operation === 'scope' ? 9 : 8, readOnly: operation === 'readOnly' } })
    await flushPromises()
    finish.forEach((resolve) =>
      resolve({
        ...proxies[0],
        references: {
          accounts: [999],
          backups: [],
          oauth: 0,
          inflight: 0,
          total: 1
        }
      })
    )
    await flushPromises()
    expect(api.proxy).toHaveBeenCalledTimes(2)
    expect(api.deleteProxies).not.toHaveBeenCalled()
    expect(w.find('[data-delete-references]').exists()).toBe(false)
    expect(w.text()).not.toContain('#999')
    w.unmount()
  }
)
it.each([false, true])('stops the queue without treating cancelled tests as skipped (quality=%s)', async (quality) => {
  const resolve: ((v: unknown) => void)[] = []
  api.testProxy.mockImplementation(() => new Promise((r) => resolve.push(r)))
  const w = render()
  await flushPromises()
  expect(api.testProxy).not.toHaveBeenCalled()
  await w
    .findAll('button')
    .find((b) => b.attributes('aria-label') === (quality ? en.qualityFiltered : en.testFiltered + ' (3)'))!
    .trigger('click')
  if (quality) {
    expect(api.testProxy).not.toHaveBeenCalled()
    await w.findAll('button').find((b) => b.text() === en.confirm)!.trigger('click')
  }
  await flushPromises()
  expect(api.proxies).toHaveBeenCalledWith(
    expect.objectContaining({ page: 2, page_size: 50 }),
    expect.any(AbortSignal)
  )
  expect(api.testProxy.mock.calls.map((c) => c[0])).toEqual([1, 2])
  expect(api.testProxy.mock.calls.every((c) => c[2] === quality)).toBe(true)
  if (!quality) {
    expect(w.get('p[role=status]').text()).toContain(en.running + ' 2')
    expect(w.get('p[role=status]').text()).toContain(en.queued + ' 1')
  }
  await w
    .findAll('button')
    .find((b) => b.text() === en.cancelQueue)!
    .trigger('click')
  resolve.forEach((r) => r({ status: 'success' }))
  await flushPromises()
  expect(api.testProxy).toHaveBeenCalledTimes(2)
  expect(w.text()).toContain(en.cancelled)
  if (!quality) {
    expect(w.get('p[role=status]').text()).toContain('0 valid, 0 invalid, 0 skipped')
    expect(w.get('p[role=status]').text()).toContain(en.cancelled + ' 3')
    expect(w.get('p[role=status]').text()).not.toContain(en.running)
    expect(notices.showSuccess).not.toHaveBeenCalled()
  }
  w.unmount()
})

it('starts row connection tests immediately, prevents duplicates and respects shared concurrency', async () => {
  api.proxies.mockResolvedValue({ items: proxies, total: 3 })
  const finish = new Map<number, (value: unknown) => void>()
  api.testProxy.mockImplementation((id) => new Promise((resolve) => finish.set(id, resolve)))
  const w = render()
  await flushPromises()
  const rowTest = (id: number) => w.get(`button[aria-label="${nativeEn.admin.proxies.testConnection} Proxy ${id}"]`)
  await rowTest(1).trigger('click')
  expect(api.testProxy).toHaveBeenCalledWith(1, 2, false, expect.any(AbortSignal))
  expect(w.find('.dialog').exists()).toBe(false)
  expect(rowTest(1).attributes('aria-busy')).toBe('true')
  expect(rowTest(1).find('[role=status]').exists()).toBe(true)
  await rowTest(1).trigger('click')
  expect(api.testProxy).toHaveBeenCalledTimes(1)
  expect(rowTest(2).attributes('disabled')).toBeUndefined()
  await rowTest(2).trigger('click')
  expect(api.testProxy).toHaveBeenCalledTimes(2)
  expect(rowTest(3).attributes('disabled')).toBeDefined()
  expect(w.get(`button[aria-label="${en.edit} Proxy 3"]`).attributes('disabled')).toBeUndefined()
  expect(w.get(`button[aria-label="${en.testFiltered} (3)"]`).attributes('disabled')).toBeDefined()
  finish.get(1)!({ status: 'success', test: { latency_ms: 0 } })
  await flushPromises()
  expect(notices.showSuccess).toHaveBeenCalledWith('Proxy 1: proxy available, latency 0 ms')
  expect(rowTest(1).attributes('aria-busy')).toBe('false')
  expect(rowTest(3).attributes('disabled')).toBeUndefined()
  finish.get(2)!({ status: 'failed', test: { message: 'dial timeout' } })
  await flushPromises()
  expect(notices.showError).toHaveBeenCalledWith('Proxy 2: connection failed · dial timeout')
  expect(w.find('.dialog').exists()).toBe(false)
  w.unmount()
})

it.each(['request failure', 'skipped result', 'inactive proxy'])(
  'reports a single connection %s without a dialog',
  async (scenario) => {
    if (scenario === 'request failure') api.testProxy.mockRejectedValueOnce(new Error('network unavailable'))
    if (scenario === 'skipped result') api.testProxy.mockResolvedValueOnce({ status: 'skipped' })
    if (scenario === 'inactive proxy') api.proxies.mockResolvedValue({ items: [{ ...proxies[0], status: 'inactive' }], total: 1 })
    const w = render()
    await flushPromises()
    await w.get(`button[aria-label="${nativeEn.admin.proxies.testConnection} Proxy 1"]`).trigger('click')
    await flushPromises()
    expect(w.find('.dialog').exists()).toBe(false)
    if (scenario === 'request failure')
      expect(notices.showError).toHaveBeenCalledWith('Proxy 1: connection failed · network unavailable')
    else expect(notices.showInfo).toHaveBeenCalledWith('Proxy 1: connection test skipped')
    if (scenario === 'inactive proxy') expect(api.testProxy).not.toHaveBeenCalled()
    expect(w.get(`button[aria-label="${nativeEn.admin.proxies.testConnection} Proxy 1"]`).attributes('disabled')).toBeUndefined()
    w.unmount()
  }
)

it('keeps rows and selection usable during the post-test refresh, including refresh failures', async () => {
  const w = render()
  await flushPromises()
  let failRefresh!: (reason: Error) => void
  api.proxies.mockImplementationOnce(() => new Promise((_, reject) => { failRefresh = reject }))
  await w.get(`button[aria-label="${nativeEn.admin.proxies.testConnection} Proxy 1"]`).trigger('click')
  await flushPromises()
  expect(w.find('tbody').text()).toContain('Proxy 1')
  await w.get('tbody input[type=checkbox]').setValue(true)
  expect(w.get('tbody input[type=checkbox]').element).toHaveProperty('checked', true)
  failRefresh(new Error('refresh failed'))
  await flushPromises()
  expect(w.find('tbody').text()).toContain('Proxy 1')
  expect(w.get('[role=alert]').text()).toContain('refresh failed')
  expect(w.get(`button[aria-label="${nativeEn.admin.proxies.testConnection} Proxy 1"]`).attributes('disabled')).toBeUndefined()
  w.unmount()
})

it.each(['scope', 'unmount', 'readOnly'])('aborts single tests and ignores late results after %s', async (operation) => {
  const finish: ((value: unknown) => void)[] = []
  api.testProxy.mockImplementation(() => new Promise((resolve) => finish.push(resolve)))
  const w = render({ admin: true, vendorId: 8 })
  await flushPromises()
  await w.get(`button[aria-label="${nativeEn.admin.proxies.testConnection} Proxy 1"]`).trigger('click')
  const signal = api.testProxy.mock.calls[0][3] as AbortSignal
  if (operation === 'unmount') w.unmount()
  else if (operation === 'readOnly') await w.setProps({ scope: { admin: true, vendorId: 8, readOnly: true } })
  else {
    await w.setProps({ scope: { admin: true, vendorId: 9 } })
    await flushPromises()
    await w.get(`button[aria-label="${nativeEn.admin.proxies.testConnection} Proxy 1"]`).trigger('click')
  }
  expect(signal.aborted).toBe(true)
  finish[0]({ status: 'success', test: { latency_ms: 88 } })
  await flushPromises()
  expect(notices.showSuccess).not.toHaveBeenCalled()
  expect(notices.showError).not.toHaveBeenCalled()
  if (operation === 'scope') {
    expect(w.get(`button[aria-label="${nativeEn.admin.proxies.testConnection} Proxy 1"]`).attributes('aria-busy')).toBe('true')
    finish[1]({ status: 'success', test: { latency_ms: 22 } })
    await flushPromises()
    expect(notices.showSuccess).toHaveBeenCalledWith('Proxy 1: proxy available, latency 22 ms')
    w.unmount()
  }
  if (operation === 'readOnly') w.unmount()
})

it('tests every filtered page immediately and reports valid, invalid and skipped counts', async () => {
  const finish = new Map<number, (value: unknown) => void>()
  api.testProxy.mockImplementation((id) => new Promise((resolve) => finish.set(id, resolve)))
  api.proxies.mockImplementation(async (q) => ({
    items: q.page_size === 50
      ? q.page === 1 ? [proxies[0], { ...proxies[1], status: 'inactive' }] : [proxies[2]]
      : [proxies[0]],
    total: q.page_size === 50 ? 3 : 4
  }))
  const w = render()
  await flushPromises()
  const bulk = w.get(`button[aria-label="${en.testFiltered} (4)"]`)
  expect(bulk.text()).toContain(en.testFiltered + ' (4)')
  await bulk.trigger('click')
  await flushPromises()
  expect(w.find('.dialog').exists()).toBe(false)
  expect(bulk.attributes('aria-busy')).toBe('true')
  expect(w.find('tbody').text()).toContain('Proxy 1')
  expect(api.proxies).toHaveBeenCalledWith(expect.objectContaining({ page: 2, page_size: 50 }), expect.any(AbortSignal))
  expect(api.testProxy.mock.calls.map((call) => call[0])).toEqual([1, 3])
  await bulk.trigger('click')
  expect(api.testProxy).toHaveBeenCalledTimes(2)
  await w.get('tbody input[type=checkbox]').setValue(true)
  expect(bulk.text()).toContain(en.testFiltered + ' (3)')
  finish.get(1)!({ status: 'success', test: { latency_ms: 32 } })
  finish.get(3)!({ status: 'failed', test: { message: 'timeout' } })
  await flushPromises()
  expect(notices.showWarning).toHaveBeenCalledWith('Test all filtered (3): 1 valid, 1 invalid, 1 skipped')
  expect(w.text()).toContain('Test all filtered (3): 1 valid, 1 invalid, 1 skipped')
  expect(w.find('.dialog').exists()).toBe(false)
  w.unmount()
})

it('freezes filtered pagination and queued proxy snapshots while the list filters change', async () => {
  let finishPage!: (value: unknown) => void
  const finishTests: ((value: unknown) => void)[] = []
  api.proxies.mockImplementation((q) => q.page_size === 50 && q.page === 1
    ? new Promise((resolve) => { finishPage = resolve })
    : Promise.resolve({ items: q.page_size === 50 ? proxies.slice(2) : proxies.slice(0, 1), total: 3 }))
  api.testProxy.mockImplementation(() => new Promise((resolve) => finishTests.push(resolve)))
  const w = render()
  await flushPromises()
  await w.get(`input[aria-label="${en.search}"]`).setValue('original')
  await w.get('form').trigger('submit')
  await flushPromises()
  await w.get(`button[aria-label="${en.testFiltered} (3)"]`).trigger('click')
  await w.get(`input[aria-label="${en.search}"]`).setValue('new filter')
  await w.get('form').trigger('submit')
  await flushPromises()
  finishPage({ items: proxies.slice(0, 2), total: 3 })
  await flushPromises()
  expect(api.proxies).toHaveBeenCalledWith(expect.objectContaining({ search: 'original', page: 2, page_size: 50 }), expect.any(AbortSignal))
  await w.get('form').trigger('submit')
  await flushPromises()
  finishTests[0]({ status: 'success' })
  await flushPromises()
  expect(api.testProxy.mock.calls.map((call) => call[0])).toEqual([1, 2, 3])
  finishTests[1]({ status: 'success' })
  finishTests[2]({ status: 'success' })
  await flushPromises()
  expect(notices.showSuccess).toHaveBeenCalledWith('Test all filtered (3): 3 valid, 0 invalid, 0 skipped')
  w.unmount()
})

it('tests the selected IDs across pages without expanding to the filtered set', async () => {
  api.proxies.mockImplementation(async (q) => ({ items: [proxies[q.page - 1]], total: 3 }))
  const w = render()
  await flushPromises()
  await w.get('tbody input[type=checkbox]').setValue(true)
  w.findComponent({ name: 'Pagination' }).vm.$emit('update:page', 2)
  await flushPromises()
  await w.get('tbody input[type=checkbox]').setValue(true)
  await w.get(`button[aria-label="${en.testSelected} (2)"]`).trigger('click')
  await flushPromises()
  expect(api.testProxy.mock.calls.map((call) => call[0])).toEqual([1, 2])
  expect(api.proxies.mock.calls.every(([q]) => q.page_size !== 50)).toBe(true)
  expect(notices.showSuccess).toHaveBeenCalledWith('Test selected (2): 2 valid, 0 invalid, 0 skipped')
  w.unmount()
})

it.each(['filtered limit', 'selected limit', 'incomplete pages'])(
  'does not start partial probes when reaching the %s',
  async (scenario) => {
    api.proxies.mockImplementation(async (q) => ({
      items: q.page_size === 50 ? [] : proxies,
      total: 3,
      capabilities: { max_scope_rows: scenario === 'filtered limit' ? 2 : 10000, max_batch_size: 1 }
    }))
    const w = render()
    await flushPromises()
    if (scenario === 'selected limit') await w.get('thead input[type=checkbox]').setValue(true)
    await w.get(`button[aria-label="${scenario === 'selected limit' ? en.testSelected : en.testFiltered} (3)"]`).trigger('click')
    await flushPromises()
    expect(api.testProxy).not.toHaveBeenCalled()
    expect(notices.showError).toHaveBeenCalledWith(en.scopeTooLarge)
    expect(w.find('tbody').exists()).toBe(true)
    w.unmount()
  }
)

it('keeps child proxy filters, pagination, detail and cached quality readable without mutation controls or credentials', async () => {
  const scope = { admin: false, vendorId: 8, readOnly: true }
  const child = {
    ...proxies[0],
    endpoint: 'http://secret-user:secret-password@proxy.invalid:8080/private?token=secret-token',
    username: 'secret-user',
    password: 'secret-password',
    has_username: true,
    has_password: true,
    quality: { ...qualityReport, score: 95 }
  }
  api.proxies.mockResolvedValue({ items: [child], total: 40 })
  api.proxy.mockResolvedValue(child)
  const w = render(scope)
  await flushPromises()
  expect(w.get('tbody').text()).toContain('http://proxy.invalid:8080')
  expect(w.find('input[type=checkbox]').exists()).toBe(false)
  expect(w.find('.proxy-action').exists()).toBe(false)
  expect(w.findComponent({ name: 'ActionMenu' }).exists()).toBe(false)
  for (const label of [en.addProxy, en.import, en.qualityFiltered, en.testFiltered + ' (40)', en.copy + ' Proxy 1'])
    expect(w.findAll('button').some((b) => b.text() === label || b.attributes('aria-label') === label)).toBe(false)
  await w.findAll('button').find((b) => b.text() === 'Proxy 1')!.trigger('click')
  await flushPromises()
  expect(api.proxy).toHaveBeenCalledWith(1, expect.objectContaining({ signal: expect.any(AbortSignal) }))
  expect(w.get('.dialog').text()).toContain('http://proxy.invalid:8080')
  expect(w.get('[data-proxy-quality-report]').text()).toContain('Grade A')
  expect(w.get('[data-proxy-quality-report]').text()).toContain('95')
  expect(w.get('.dialog').text()).toContain('OpenAI')
  expect(w.get('.dialog').text()).not.toContain(en.credentialAction)
  for (const secret of ['secret-user', 'secret-password', 'secret-token', '/private'])
    expect(w.html()).not.toContain(secret)
  w.getComponent({ name: 'BaseDialog' }).vm.$emit('close')
  await w.get('input[aria-label="' + en.search + '"]').setValue('child')
  await w.get('form').trigger('submit')
  await flushPromises()
  w.getComponent({ name: 'Pagination' }).vm.$emit('update:page', 2)
  await flushPromises()
  expect(api.proxies).toHaveBeenLastCalledWith(expect.objectContaining({ search: 'child', page: 2 }), expect.any(AbortSignal))
  for (const [value] of api.scope.mock.calls) expect(value).toEqual(scope)
  expect(api.testProxy).not.toHaveBeenCalled()
  expect(api.deleteProxies).not.toHaveBeenCalled()
  expect(api.proxyExport).not.toHaveBeenCalled()
  expect(download).not.toHaveBeenCalled()
  w.unmount()
})

it('guards every proxy mutation and credential-copy entry point when directly invoked in a child scope', async () => {
  const w = render({ admin: false, vendorId: 8, readOnly: true })
  await flushPromises()
  const vm = w.vm as any
  await vm.edit()
  await vm.edit(proxies[0])
  vm.openImport()
  vm.deleteProxy(1)
  vm.selected = new Set([1])
  vm.openDelete()
  await vm.loadDeleteReferences()
  vm.openQuality([1])
  await vm.tests(false, [1])
  await vm.tests(true, [1])
  await vm.testProxyRow(proxies[0])
  for (const format of ['csv', 'json']) await vm.exportRows(format)
  vm.copyEndpoint(proxies[0])
  expect(vm.showForm).toBe(false)
  expect(vm.importing).toBe(false)
  expect(vm.confirmDelete).toBe(false)
  expect(vm.confirmTest).toBeUndefined()
  vm.confirmDelete = true
  vm.deletePreview = [{ id: 1, proxy: { ...proxies[0], references: { total: 0 } } }]
  await vm.remove()
  expect(api.proxy).not.toHaveBeenCalled()
  expect(api.testProxy).not.toHaveBeenCalled()
  expect(api.deleteProxies).not.toHaveBeenCalled()
  expect(api.proxyExport).not.toHaveBeenCalled()
  expect(copyToClipboard).not.toHaveBeenCalled()
  expect(download).not.toHaveBeenCalled()
  w.unmount()
})

it.each([false, true])('clears proxy dialogs and ignores old details or edits when only readOnly changes from %s', async (readOnly) => {
  const w = render({ admin: false, vendorId: 8, readOnly })
  await flushPromises()
  const vm = w.vm as any
  await vm.showDetail(proxies[0])
  await vm.edit(proxies[0])
  vm.openImport()
  vm.openQuality([1])
  const finish: ((value: unknown) => void)[] = []
  api.proxy.mockImplementation(() => new Promise((resolve) => finish.push(resolve)))
  const pendingDetail = vm.showDetail(proxies[0])
  const pendingEdit = vm.edit(proxies[0])
  await w.setProps({ scope: { admin: false, vendorId: 8, readOnly: !readOnly } })
  finish.forEach((resolve) => resolve({ ...proxies[0], name: 'Previous child proxy' }))
  await Promise.all([pendingDetail, pendingEdit])
  await flushPromises()
  expect(w.find('.dialog').exists()).toBe(false)
  expect(w.findComponent({ name: 'ProxyForm' }).exists()).toBe(false)
  expect(w.findComponent({ name: 'ProxyImport' }).exists()).toBe(false)
  expect(vm.editing).toBeUndefined()
  expect(vm.selected.size).toBe(0)
  expect(w.text()).not.toContain('Previous child proxy')
  w.unmount()
})

it('reloads the applied proxy query on scope changes even with invalid draft dates and a pending old list', async () => {
  const w = render({ admin: false, vendorId: 8, readOnly: true })
  await flushPromises()
  const vm = w.vm as any
  let finishOld!: (value: unknown) => void
  let finishOwn!: (value: unknown) => void
  api.proxies.mockImplementationOnce(() => new Promise((resolve) => { finishOld = resolve }))
  const pendingOld = vm.load(false, true)
  vm.filters.start_date = '2026-10-17'
  vm.filters.end_date = '2026-10-01'
  api.proxies.mockImplementationOnce(() => new Promise((resolve) => { finishOwn = resolve }))
  await w.setProps({ scope: { admin: false } })
  expect(api.proxies).toHaveBeenCalledTimes(3)
  expect(w.find('tbody').exists()).toBe(false)
  expect(vm.total).toBe(0)
  finishOld({ items: [{ ...proxies[0], name: 'Previous child list' }], total: 99 })
  await pendingOld
  await flushPromises()
  expect(w.text()).not.toContain('Previous child list')
  expect(vm.total).toBe(0)
  finishOwn({ items: [{ ...proxies[0], name: 'Current own proxy' }], total: 1 })
  await flushPromises()
  expect(w.get('tbody').text()).toContain('Current own proxy')
  expect(api.proxies).toHaveBeenLastCalledWith(expect.objectContaining({ start_date: '', end_date: '', page: 1 }), expect.any(AbortSignal))
  w.unmount()
})

it('aborts queued proxy probes on a permission-only change without starting the remaining rows', async () => {
  api.proxies.mockResolvedValue({ items: proxies, total: 3 })
  const finish: ((value: unknown) => void)[] = []
  api.testProxy.mockImplementation(() => new Promise((resolve) => finish.push(resolve)))
  const w = render({ admin: false, vendorId: 8 })
  await flushPromises()
  const pending = (w.vm as any).tests(true, [1, 2, 3])
  await flushPromises()
  expect(api.testProxy).toHaveBeenCalledTimes(2)
  const signals = api.testProxy.mock.calls.map((call) => call[3] as AbortSignal)
  await w.setProps({ scope: { admin: false, vendorId: 8, readOnly: true } })
  expect(signals.every((signal) => signal.aborted)).toBe(true)
  finish.forEach((resolve) => resolve({ status: 'success' }))
  await pending
  expect(api.testProxy).toHaveBeenCalledTimes(2)
  expect(notices.showSuccess).not.toHaveBeenCalled()
  expect(w.find('.dialog').exists()).toBe(false)
  w.unmount()
})

it.each(['success', 'failure'])('ignores stale proxy export %s without downloading or unlocking the next scope', async (result) => {
  const w = render({ admin: false, vendorId: 8 })
  await flushPromises()
  const vm = w.vm as any
  let finish!: (value: Blob) => void
  let fail!: (reason: Error) => void
  api.proxyExport.mockImplementationOnce(() => new Promise((resolve, reject) => { finish = resolve; fail = reject }))
  const pendingExport = vm.exportRows('json')
  await w.setProps({ scope: { admin: false, vendorId: 8, readOnly: true } })
  let finishDetail!: (value: unknown) => void
  api.proxy.mockImplementationOnce(() => new Promise((resolve) => { finishDetail = resolve }))
  const pendingDetail = vm.showDetail(proxies[0])
  if (result === 'success') finish(new Blob(['secret-export']))
  else fail(new Error('previous scope failure'))
  await pendingExport
  expect(vm.busy).toBe(true)
  expect(vm.actionError).toBe('')
  expect(download).not.toHaveBeenCalled()
  finishDetail(proxies[0])
  await pendingDetail
  w.unmount()
})

it.each([{ admin: false }, { admin: true, vendorId: 8 }])('retains proxy forms, credential copy and exports in writable scope %j', async (scope) => {
  const data = new Blob(['owned proxies'])
  api.proxyExport.mockResolvedValue(data)
  const w = render(scope)
  await flushPromises()
  await w.get('button[aria-label="' + en.copy + ' Proxy 1"]').trigger('click')
  expect(copyToClipboard).toHaveBeenCalledWith('http://proxy.invalid:80')
  await w.get('button[aria-label="' + en.edit + ' Proxy 1"]').trigger('click')
  await flushPromises()
  expect(w.getComponent({ name: 'ProxyForm' }).props('proxy')).toMatchObject({ id: 1 })
  w.getComponent({ name: 'ProxyForm' }).vm.$emit('close')
  await w.findAll('button').find((b) => b.text() === en.import)!.trigger('click')
  expect(w.getComponent({ name: 'ProxyImport' }).props('scope')).toEqual(scope)
  w.getComponent({ name: 'ProxyImport' }).vm.$emit('close')
  for (const format of ['csv', 'json']) {
    await (w.vm as any).exportRows(format)
    expect(api.proxyExport).toHaveBeenLastCalledWith(expect.objectContaining({ format, filter: expect.any(Object) }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(download).toHaveBeenLastCalledWith(data, 'supplier-proxies.' + format)
  }
  w.unmount()
})

it('uses native uppercase protocol choices while querying and displaying lowercase URLs', async () => {
  const w = render()
  await flushPromises()
  expect(w.find('select').exists()).toBe(false)
  const select = w.findAllComponents(Select).find((c) => c.props('ariaLabel') === en.protocol)!
  expect(select.props('options')).toEqual([
    { value: '', label: en.allProtocols },
    { value: 'http', label: 'HTTP' },
    { value: 'https', label: 'HTTPS' },
    { value: 'socks5', label: 'SOCKS5' },
    { value: 'socks5h', label: 'SOCKS5H' }
  ])
  await select.find('button').trigger('click')
  await select
    .findAll('[role=option]')
    .find((o) => o.text() === 'SOCKS5H')!
    .trigger('click')
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(api.proxies).toHaveBeenLastCalledWith(
    expect.objectContaining({ protocol: 'socks5h' }),
    expect.any(AbortSignal)
  )
  expect(w.find('tbody').text()).toContain('HTTP')
  expect(w.find('tbody').text()).toContain('http://proxy.invalid:80')
  expect(w.find('tbody').text()).not.toContain('HTTP://')
  expect(api.testProxy).not.toHaveBeenCalled()
  w.unmount()
})

it('runs a row quality check immediately and displays a complete readable report', async () => {
  let complete!: (value: unknown) => void
  api.testProxy.mockImplementation(() => new Promise(resolve => { complete = resolve }))
  const w = render()
  await flushPromises()
  const button = w.get('button[aria-label="Quality Check Proxy 1"]')
  await button.trigger('click')
  await flushPromises()
  expect(api.testProxy).toHaveBeenCalledWith(1, 2, true, expect.any(AbortSignal))
  expect(button.attributes('aria-busy')).toBe('true')
  expect(w.findAll('button').some(b => b.text() === en.confirm)).toBe(false)
  expect(w.find('.dialog').exists()).toBe(false)
  expect(button.find('svg.animate-spin').exists()).toBe(true)
  await button.trigger('click')
  expect(api.testProxy).toHaveBeenCalledTimes(1)
  complete({ status: 'success', quality: qualityReport })
  await flushPromises()
  const report = w.get('[data-proxy-quality-report]')
  for (const text of ['3 passed', 'Grade A', '192.0.2.10', '64 ms', 'Base Connectivity', 'OpenAI', 'Anthropic', '401', '405', 'Pass'])
    expect(report.text()).toContain(text)
  expect(report.text()).not.toContain('Unknown')
  expect(w.find('[role=progressbar]').exists()).toBe(false)
  expect(button.attributes('aria-busy')).toBe('false')
  expect(notices.showSuccess).toHaveBeenCalledWith(expect.stringContaining('100'))
  expect(w.getComponent({ name: 'BaseDialog' }).props('width')).toBe('normal')
  w.getComponent({ name: 'BaseDialog' }).vm.$emit('close')
  await flushPromises()
  await w.get('[data-quality-report-trigger]').trigger('click')
  expect(w.get('[data-proxy-quality-report]').text()).toContain('3 passed')
  expect(api.testProxy).toHaveBeenCalledTimes(1)
  w.unmount()
})

it('retains completed reports while closing aborts unfinished quality checks', async () => {
  const completions = new Map<number, (value: unknown) => void>()
  api.testProxy.mockImplementation(id => new Promise(resolve => completions.set(id, resolve)))
  api.proxies.mockResolvedValue({ items: proxies, total: 3 })
  const w = render()
  await flushPromises()
  await w.get('button[aria-label="' + en.qualityFiltered + '"]').trigger('click')
  await w.findAll('button').find(b => b.text() === en.confirm)!.trigger('click')
  await flushPromises()
  completions.get(1)!({ status: 'success', quality: qualityReport })
  await flushPromises()
  expect(w.get('[data-quality-result="1"]').text()).toContain('Quality A/100')
  const dialog = w.getComponent({ name: 'BaseDialog' })
  dialog.vm.$emit('close')
  await flushPromises()
  expect(api.testProxy.mock.calls.every(call => call[3].aborted)).toBe(true)
  for (const [id, complete] of completions) if (id !== 1) complete({ status: 'success', quality: { ...qualityReport, score: 99 } })
  await flushPromises()
  await w.get('[data-quality-report-trigger]').trigger('click')
  expect(w.get('[data-proxy-quality-report]').text()).toContain('100')
  expect((w.vm as any).testResults.filter((row: any) => row.status === 'cancelled')).toHaveLength(2)
  w.unmount()
})

it('allows another row to run a quality check while the first row is pending', async () => {
  const completed = new Map<number, (value: unknown) => void>()
  api.proxies.mockResolvedValue({ items: proxies, total: 3 })
  api.testProxy.mockImplementation(id => new Promise(resolve => completed.set(id, resolve)))
  const w = render(); await flushPromises()
  await w.get('button[aria-label="Quality Check Proxy 1"]').trigger('click')
  expect(w.get('button[aria-label="Quality Check Proxy 2"]').attributes('disabled')).toBeUndefined()
  await w.get('button[aria-label="Quality Check Proxy 2"]').trigger('click')
  expect(api.testProxy).toHaveBeenCalledTimes(2)
  expect(w.find('.dialog').exists()).toBe(false)
  await w.setProps({ scope: { admin: false, vendorId: 8, readOnly: true } })
  expect(api.testProxy.mock.calls.every(call => call[3].aborted)).toBe(true)
  for (const complete of completed.values()) complete({ status: 'success', quality: qualityReport })
  await flushPromises()
  expect(w.find('.dialog').exists()).toBe(false)
  expect(notices.showSuccess).not.toHaveBeenCalled()
  w.unmount()
})
it('shows a toast and restores the row button when a quality check fails', async () => {
  api.testProxy.mockRejectedValue({ message: 'private proxy credential' })
  const w = render(); await flushPromises()
  const button = w.get('button[aria-label="Quality Check Proxy 1"]')
  await button.trigger('click'); await flushPromises()
  expect(button.attributes('disabled')).toBeUndefined()
  expect(button.attributes('aria-busy')).toBe('false')
  expect(w.find('.dialog').exists()).toBe(false)
  expect(notices.showError).toHaveBeenCalledWith(nativeEn.admin.proxies.qualityCheckFailed)
  w.unmount()
})

vi.mock('@/features/supply/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/supply/services')>('@/features/supply/services')
  const { createSupplyTestServices } = await import('@/features/supply/__tests__/services')
  const services = createSupplyTestServices()
  return { ...actual, useSupplyServices: () => services }
})
