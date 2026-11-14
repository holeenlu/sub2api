import Select from '@/shared/ui/Select.vue'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ProxyForm from '../components/ProxyForm.vue'
import type { ResourceScope } from '@/features/supply/types'
import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const en = assembledEnMessages.vendor
const nativeEn = assembledEnMessages

const selector = (w: VueWrapper, label: string) =>
  w.findAllComponents(Select).find((s) => s.props('ariaLabel') === label)!
async function choose(w: VueWrapper, label: string, value: string | number) {
  const control = selector(w, label)
  expect(control).toBeDefined()
  await control.find('button').trigger('click')
  const option = (
    control.props('options') as { value: string | number; label: string }[]
  ).find((o) => o.value === value)!
  expect(option).toBeDefined()
  await control
    .findAll('[role=option]')
    .find((o) => o.text() === option.label)!
    .trigger('click')
  await flushPromises()
}
const mocks = vi.hoisted(() => ({ updateProxy: vi.fn(), createProxy: vi.fn(), proxyImport: vi.fn(), proxy: vi.fn(), proxies: vi.fn(), resourceAPI: vi.fn() }))
vi.mock('@/features/supply/api', () => ({ resourceAPI: (scope: ResourceScope) => { mocks.resourceAPI(scope); return mocks } }))
const proxy = {
  id: 4,
  vendor_id: 7,
  name: 'EU egress',
  protocol: 'socks5h',
  host: 'gateway.example',
  port: 443,
  status: 'active',
  has_username: true,
  has_password: true,
  config_version: 9,
}
function render(editing = true, settings = {}, scope: ResourceScope = { admin: false }) {
  return mount(ProxyForm, {
    props: { scope, ...(editing ? { proxy: { ...proxy, ...settings } } : {}) },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          messages: { en: nativeEn },
        }),
      ],
      stubs: {
        teleport: true,
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
      },
    },
  })
}
beforeEach(() => {
  vi.resetAllMocks()
  mocks.updateProxy.mockResolvedValue(proxy)
  mocks.createProxy.mockResolvedValue(proxy)
  mocks.proxy.mockResolvedValue(proxy)
  mocks.proxies.mockResolvedValue({ items: [], total: 0 })
})
describe('proxy credential edits', () => {
  it('sends a versioned metadata patch without replacing existing authentication', async () => {
    const w = render()
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(mocks.updateProxy).toHaveBeenCalledWith(
      4,
      expect.objectContaining({
        credential_action: 'preserve',
        config_version: 9,
        protocol: 'socks5h',
      }),
      expect.any(String), expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    const body = mocks.updateProxy.mock.calls[0][1]
    expect(body).not.toHaveProperty('username')
    expect(body).not.toHaveProperty('password')
    w.unmount()
  })
  it('keeps a blank password omitted during a username-only change', async () => {
    const w = render()
    await choose(w, en.credentialAction, 'update')
    const username = w.get('input[id$="-username"]')
    await username.setValue('updated-user')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(mocks.updateProxy.mock.calls[0][1]).toMatchObject({
      credential_action: 'update',
      username: 'updated-user',
    })
    expect(mocks.updateProxy.mock.calls[0][1]).not.toHaveProperty('password')
    w.unmount()
  })
  it('clears credentials only through an explicit action', async () => {
    const w = render()
    await choose(w, en.credentialAction, 'clear')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(mocks.updateProxy.mock.calls[0][1]).toMatchObject({
      credential_action: 'clear',
      config_version: 9,
    })
    w.unmount()
  })
})

it('reuses an unchanged proxy write key after an ambiguous network failure', async () => {
  mocks.updateProxy.mockRejectedValueOnce(new Error('Network interrupted'))
    .mockRejectedValueOnce(new Error('Still interrupted'))
  const w = render()
  await w.find('form').trigger('submit')
  await flushPromises()
  const key = mocks.updateProxy.mock.calls[0][2]
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(mocks.updateProxy.mock.calls[1][2]).toBe(key)
  await w.find('input').setValue('Changed name')
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(mocks.updateProxy.mock.calls[2][2]).not.toBe(key)
  w.unmount()
})

it.each(['http', 'https', 'socks5', 'socks5h'])(
  'displays %s in uppercase while retaining the lowercase protocol and credentials in writes',
  async (protocol) => {
    const w = render()
    expect(w.find('select').exists()).toBe(false)
    expect(selector(w, en.protocol).props('options')).toEqual([
      { value: 'http', label: 'HTTP' },
      { value: 'https', label: 'HTTPS' },
      { value: 'socks5', label: 'SOCKS5' },
      { value: 'socks5h', label: nativeEn.admin.proxies.protocols.socks5h },
    ])
    await choose(w, en.protocol, protocol)
    expect(selector(w, en.protocol).find('button').text()).toBe(
      nativeEn.admin.proxies.protocols[protocol as keyof typeof nativeEn.admin.proxies.protocols],
    )
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(mocks.updateProxy).toHaveBeenLastCalledWith(
      4,
      expect.objectContaining({
        protocol,
        credential_action: 'preserve',
        config_version: 9,
      }),
      expect.any(String), expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    const body = mocks.updateProxy.mock.calls.at(-1)![1]
    expect(body).not.toHaveProperty('username')
    expect(body).not.toHaveProperty('password')
    w.unmount()
  },
)

const native = nativeEn.admin.proxies
async function fillCreate(w: VueWrapper) {
  await w.get('input[id$="-name"]').setValue(' New proxy ')
  await w.get('input[id$="-host"]').setValue(' 2001:db8::1 ')
}
it('uses the native field order, date presets and project password icon without supplier-only create fields', async () => {
  const w = render(false)
  expect(w.findAll('label').map(label => label.text())).toEqual([
    native.name, native.protocol, native.host, native.port, native.username,
    native.password, native.expiresAt, native.fallbackMode
  ])
  expect(w.find('textarea').exists()).toBe(false)
  expect(w.find('input[type="datetime-local"]').exists()).toBe(false)
  const presets = w.findAll('button').filter(button => /^\d+d$/.test(button.text()))
  expect(presets.map(button => button.text())).toEqual(['7d', '30d', '90d', '180d'])
  await presets[1].trigger('click')
  expect((w.get('input[type="date"]').element as HTMLInputElement).value).toMatch(/^\d{4}-\d{2}-\d{2}$/)
  await w.get('input[aria-label="' + native.expiryDaysPlaceholder + '"]').setValue(0)
  expect((w.get('input[type="date"]').element as HTMLInputElement).value).toBe('')
  const toggle = w.get('button[aria-label="' + native.password + '"]')
  expect(w.get('input[id$="-password"]').attributes('type')).toBe('password')
  await toggle.trigger('click')
  expect(toggle.attributes('aria-pressed')).toBe('true')
  expect(w.get('input[id$="-password"]').attributes('type')).toBe('text')
  expect(selector(w, native.fallbackMode).props('disabled')).toBe(false)
  expect(selector(w, native.fallbackMode).props('options')).toContainEqual(expect.objectContaining({ value: 'direct', disabled: true }))
  w.unmount()
})
it('creates with native fields while preserving credentials and omitting edit-only metadata', async () => {
  const w = render(false)
  await fillCreate(w)
  await w.get('input[id$="-username"]').setValue(' user ')
  await w.get('input[id$="-password"]').setValue(' p@ss: word ')
  await w.get('input[type="date"]').setValue('2030-01-20')
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(mocks.createProxy).toHaveBeenCalledWith({
    name: 'New proxy', protocol: 'http', host: '2001:db8::1', port: 8080,
    fallback_mode: 'none', backup_proxy_id: null, expiry_warn_days: 7,
    username: ' user ', password: ' p@ss: word ', expires_at: '2030-01-20T00:00:00.000Z'
  }, expect.any(String), expect.objectContaining({ signal: expect.any(AbortSignal) }))
  expect((w.get('input[id$="-password"]').element as HTMLInputElement).value).toBe('')
  w.unmount()
})
it('blocks overlapping creates and another submit after success', async () => {
  let resolve!: (value: typeof proxy) => void
  mocks.createProxy.mockImplementation(() => new Promise(r => { resolve = r }))
  const w = render(false)
  await fillCreate(w)
  await w.get('form').trigger('submit')
  await w.get('form').trigger('submit')
  expect(mocks.createProxy).toHaveBeenCalledTimes(1)
  expect(w.get('input[id$="-password"]').attributes('disabled')).toBeDefined()
  resolve(proxy)
  await flushPromises()
  await w.get('form').trigger('submit')
  expect(mocks.createProxy).toHaveBeenCalledTimes(1)
  w.unmount()
})
it('keeps the create idempotency key on retry and never renders a credential-bearing error', async () => {
  mocks.createProxy.mockRejectedValueOnce(new Error('http://user:DO-NOT-ECHO@example.test:8080'))
  const w = render(false)
  await fillCreate(w)
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(w.get('[role="alert"]').text()).toBe(native.failedToCreate)
  expect(w.text()).not.toContain('DO-NOT-ECHO')
  const key = mocks.createProxy.mock.calls[0][1]
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(mocks.createProxy.mock.calls[1][1]).toBe(key)
  w.unmount()
})
it('uses the native quick input and completes the existing server preview/confirm flow', async () => {
  mocks.proxyImport.mockResolvedValueOnce({ input_digest: 'ipv6-digest', valid_count: 1, rows: [] })
    .mockResolvedValueOnce({ valid_count: 1, rows: [{ proxy_id: 4, status: 'created' }] })
  const w = render(false)
  await w.findAll('button').find(button => button.text() === native.batchAdd)!.trigger('click')
  const input = 'socks5h://user:p%40ss%3Aword@[2606:4700:4700::1111]:1080'
  await w.get('textarea').setValue(input)
  expect(w.findComponent(Select).exists()).toBe(false)
  expect(w.text()).not.toContain(en.preview)
  expect(w.findAll('button').find(button => button.text() === 'Import 1 proxies')!.attributes('form')).toBe('vendor-proxy-import-form')
  await w.get('#vendor-proxy-import-form').trigger('submit')
  await flushPromises()
  expect(mocks.proxyImport.mock.calls[0]).toEqual([{ format: 'quick', content: input }, true, undefined, expect.objectContaining({ signal: expect.any(AbortSignal) })])
  expect(mocks.proxyImport.mock.calls[1]).toEqual([{ format: 'quick', content: input, input_digest: 'ipv6-digest' }, false, expect.any(String), expect.objectContaining({ signal: expect.any(AbortSignal) })])
  expect(w.emitted('saved')).toEqual([[proxy]])
  expect((w.get('textarea').element as HTMLTextAreaElement).value).toBe('')
  w.unmount()
})

const backup = { ...proxy, id: 6, host: '2606:4700:4700::1111', name: 'Backup' }
it('loads backup options in the direct scope and excludes self, platform and other owners', async () => {
  mocks.proxies.mockResolvedValue({
    total: 5, items: [proxy, backup,
      { ...backup, id: 8, vendor_id: 8 }, { ...backup, id: 9, vendor_id: 9 }, { ...backup, id: 10, vendor_id: null }]
  })
  const w = render(true, { fallback_mode: 'proxy', backup_proxy_id: 6, expiry_warn_days: 0 }, { admin: true, vendorId: 7 })
  await flushPromises()
  expect(mocks.resourceAPI).toHaveBeenCalledWith({ admin: true, vendorId: 7 })
  expect(mocks.proxies).toHaveBeenCalledWith({ page: 1, page_size: 50, sort_by: 'created_at', sort_order: 'asc' }, expect.any(AbortSignal))
  expect(selector(w, native.backupProxy).props('options')).toEqual([{ value: 6, label: 'Backup ([2606:4700:4700::1111]:443)' }])
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(mocks.updateProxy.mock.calls[0][1]).toMatchObject({ fallback_mode: 'proxy', backup_proxy_id: 6, expiry_warn_days: 0, credential_action: 'preserve' })
  expect(mocks.updateProxy.mock.calls[0][1]).not.toHaveProperty('password')
  w.unmount()
})
it('creates with a backup and explicitly clears it when fallback is disabled', async () => {
  mocks.proxies.mockResolvedValue({ total: 1, items: [backup] })
  const w = render(false, {}, { admin: false, vendorId: 7 })
  await fillCreate(w)
  await choose(w, native.fallbackMode, 'proxy')
  await choose(w, native.backupProxy, 6)
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(mocks.createProxy.mock.calls[0][0]).toMatchObject({ fallback_mode: 'proxy', backup_proxy_id: 6 })
  w.unmount()
  const edit = render(true, { fallback_mode: 'proxy', backup_proxy_id: 6 })
  await flushPromises()
  await choose(edit, native.fallbackMode, 'none')
  await edit.get('form').trigger('submit')
  await flushPromises()
  expect(mocks.updateProxy.mock.calls[0][1]).toMatchObject({ fallback_mode: 'none', backup_proxy_id: null })
  edit.unmount()
})
it('loads every option page and preserves disabled backups for native chained resolution', async () => {
  const items = Array.from({ length: 50 }, (_, i) => ({ ...backup, id: i + 10 }))
  mocks.proxies.mockResolvedValueOnce({ total: 51, items })
    .mockResolvedValueOnce({ total: 51, items: [{ ...backup, id: 60, status: 'inactive' }] })
  const w = render(true, { fallback_mode: 'proxy', backup_proxy_id: 60 })
  await flushPromises()
  expect(mocks.proxies).toHaveBeenCalledTimes(2)
  expect(selector(w, native.backupProxy).props('options')).toHaveLength(51)
  await w.get('form').trigger('submit')
  await flushPromises()
  expect(mocks.updateProxy.mock.calls[0][1].backup_proxy_id).toBe(60)
  w.unmount()
})
it.each(['failure', 'partial', 'changed', 'duplicate', 'oversized'])('fails closed on %s backup pages without leaking API errors', async (kind) => {
  const items = Array.from({ length: 50 }, (_, i) => ({ ...backup, id: i + 10 }))
  if (kind === 'failure') mocks.proxies.mockRejectedValue(new Error('SECRET-password'))
  if (kind === 'partial') mocks.proxies.mockResolvedValue({ total: 51, items: [backup] })
  if (kind === 'changed') mocks.proxies.mockResolvedValueOnce({ total: 51, items }).mockResolvedValueOnce({ total: 52, items: [backup] })
  if (kind === 'duplicate') mocks.proxies.mockResolvedValueOnce({ total: 51, items }).mockResolvedValueOnce({ total: 51, items: [items[0]] })
  if (kind === 'oversized') mocks.proxies.mockResolvedValue({ total: 10001, items })
  const w = render(true, { fallback_mode: 'proxy', backup_proxy_id: 6 })
  await flushPromises()
  expect(selector(w, native.backupProxy).props('options')).toEqual([])
  expect(w.text()).toContain(native.failedToLoad)
  expect(w.text()).not.toContain('SECRET-password')
  await w.get('form').trigger('submit')
  expect(mocks.updateProxy).not.toHaveBeenCalled()
  w.unmount()
})
it('aborts stale scope requests and cannot publish their backup options', async () => {
  let resolve!: (value: unknown) => void
  mocks.proxies.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    .mockResolvedValueOnce({ total: 1, items: [{ ...backup, id: 8, vendor_id: 8 }] })
  const w = render(true, { fallback_mode: 'proxy', backup_proxy_id: 6 }, { admin: true, vendorId: 7 })
  const signal = mocks.proxies.mock.calls[0][1] as AbortSignal
  await w.setProps({ scope: { admin: true, vendorId: 8 } })
  await flushPromises()
  expect(signal.aborted).toBe(true)
  resolve({ total: 1, items: [backup] })
  await flushPromises()
  expect(selector(w, native.backupProxy).props('options')).toEqual([expect.objectContaining({ value: 8 })])
  await w.get('form').trigger('submit')
  expect(mocks.updateProxy).not.toHaveBeenCalled()
  w.unmount()
})
it('does not load or submit in read-only descendant scope', async () => {
  const w = render(true, { fallback_mode: 'proxy', backup_proxy_id: 6 }, { admin: false, vendorId: 7, readOnly: true })
  await flushPromises()
  await w.get('form').trigger('submit')
  expect(mocks.proxies).not.toHaveBeenCalled()
  expect(mocks.updateProxy).not.toHaveBeenCalled()
  w.unmount()
})

it('narrows an aggregate administrator edit to the proxy direct owner', async () => {
  mocks.proxies.mockResolvedValue({ total: 1, items: [backup] })
  const w = render(true, { fallback_mode: 'proxy', backup_proxy_id: 6 }, { admin: true, vendorId: 0 })
  await flushPromises()
  expect(mocks.resourceAPI).toHaveBeenCalledWith({ admin: true, vendorId: 7 })
  expect(selector(w, native.backupProxy).props('options')).toEqual([expect.objectContaining({ value: 6 })])
  w.unmount()
})
it('never queries aggregate backup candidates for a create without a direct vendor', async () => {
  const w = render(false, {}, { admin: true, vendorId: 0 })
  await choose(w, native.fallbackMode, 'proxy')
  expect(mocks.proxies).not.toHaveBeenCalled()
  expect(w.text()).toContain(native.failedToLoad)
  w.unmount()
})

vi.mock('@/features/supply/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/supply/services')>('@/features/supply/services')
  const { createSupplyTestServices } = await import('@/features/supply/__tests__/services')
  const services = createSupplyTestServices()
  return { ...actual, useSupplyServices: () => services }
})
