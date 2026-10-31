import type { Ref } from 'vue'
import { beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import VendorPlatformWorkspace from '../VendorPlatformWorkspace.vue'
import Select from '@/shared/ui/Select.vue'
import UsageWorkbench from '../components/UsageWorkbench.vue'
import VendorManagement from '@/features/supply/components/VendorManagement.vue'
import AccountWorkbench from '../components/AccountWorkbench.vue'
import ProxyWorkbench from '../components/ProxyWorkbench.vue'
import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const nativeEn = assembledEnMessages

const api = vi.hoisted(() => ({
  list: vi.fn(),
  detail: vi.fn(),
  adminUsage: vi.fn(),
}))
const client = vi.hoisted(() => ({ get: vi.fn() }))
const auth = vi.hoisted(() => ({ isSuperAdmin: true }))
const lifecycle = vi.hoisted(() => ({ identity: undefined as { isSuperAdmin: boolean } | undefined, scope: undefined as Ref<string | null> | undefined }))
vi.mock('@/features/supply/api', () => ({ vendorAPI: api }))
vi.mock('@/shared/http', () => ({ default: client }))
vi.mock('@/features/identity', async () => {
  const { reactive } = await import('vue')
  lifecycle.identity = reactive(auth)
  return { useAuthStore: () => lifecycle.identity }
})


const catalog = [
  { id: 3, name: 'Shared group', platform: 'openai', is_default: false },
]
const owned = [
  { id: 9, name: 'Supplier group', platform: 'anthropic', is_default: false },
]
const en = nativeEn.vendor
async function render(tab = 'usage') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/vendor/platform', component: VendorPlatformWorkspace }],
  })
  await router.push({ path: '/vendor/platform', query: { tab } })
  await router.isReady()
  const w = mount(VendorPlatformWorkspace, {
    global: {
      plugins: [
        router,
        createI18n({ legacy: false, locale: 'en', messages: { en: nativeEn } }),
      ],
      stubs: {
        VendorManagement: true,
        AccountWorkbench: true,
        ProxyWorkbench: true,
        SettlementWorkbench: true,
        GroupSupply: true,
        VendorAudit: true,
        teleport: true,
        Pagination: true,
        UsageTrend: true,
        UsageAccountPicker: true,
      },
    },
  })
  await flushPromises()
  return { w, router }
}
beforeEach(() => {
  vi.resetAllMocks()
  auth.isSuperAdmin = true
  if (lifecycle.identity) lifecycle.identity.isSuperAdmin = true
  if (lifecycle.scope) lifecycle.scope.value = 'session-a:admin'
  client.get.mockResolvedValue({ data: catalog })
  api.list.mockResolvedValue({
    items: [{ id: 8, name: 'Supplier eight', status: 'active' }],
    total: 1,
  })
  api.detail.mockResolvedValue({
    id: 8,
    name: 'Supplier eight',
    status: 'active',
    groups: owned,
  })
  api.adminUsage.mockResolvedValue({ items: [], total: 0, summary: {} })
})
it.each(['suppliers', 'accounts', 'proxies', 'usage', 'settlements', 'supply', 'audit'])(
  'does not repeat the platform introduction above the embedded %s workspace', async tab => {
    const { w } = await render(tab)
    expect(w.find('header').exists()).toBe(false)
    expect(w.text()).not.toContain(en.platformDescription)
    expect(w.text()).not.toContain(en.permissionBoundary)
    expect(w.text()).not.toContain(en.settlementScopeValue)
    expect(w.text()).not.toContain(en.dataBoundaryValue)
    w.unmount()
  },
)
it('retries the failed supplier catalog and keeps errors visible until recovery', async () => {
  api.list.mockRejectedValue({ status: 404, message: 'Request failed with status code 404' })
  const { w } = await render('proxies')
  expect(w.get('[role=alert]').text()).toContain('404')
  expect(w.findComponent(ProxyWorkbench).exists()).toBe(false)
  await w.get('[role=alert] button').trigger('click')
  await flushPromises()
  expect(api.list).toHaveBeenCalledTimes(2)
  expect(w.get('[role=alert]').text()).toContain('404')
  expect(w.findComponent(ProxyWorkbench).exists()).toBe(false)
  api.list.mockResolvedValueOnce({ items: [], total: 0 })
  await w.get('[role=alert] button').trigger('click')
  await flushPromises()
  expect(api.list).toHaveBeenCalledTimes(3)
  expect(w.find('[role=alert]').exists()).toBe(false)
  expect(w.findComponent(ProxyWorkbench).exists()).toBe(true)
  expect(api.detail).not.toHaveBeenCalled()
  w.unmount()
})
it('retries both failed catalogs without losing a selected supplier detail error', async () => {
  api.list.mockRejectedValueOnce(new Error('Supplier catalog unavailable'))
  client.get.mockRejectedValueOnce(new Error('Group catalog unavailable'))
  const { w } = await render('accounts')
  await w.get('[role=alert] button').trigger('click')
  await flushPromises()
  expect(api.list).toHaveBeenCalledTimes(2)
  expect(client.get).toHaveBeenCalledTimes(2)
  expect(w.findComponent(AccountWorkbench).exists()).toBe(true)
  api.detail.mockRejectedValueOnce(new Error('Supplier detail unavailable'))
  w.findAllComponents(Select).find(c => c.props('ariaLabel') === en.vendor)!.vm.$emit('update:modelValue', 8)
  await flushPromises()
  expect(w.get('[role=alert]').text()).toContain('Supplier detail unavailable')
  await w.get('[role=alert] button').trigger('click')
  await flushPromises()
  expect(api.detail).toHaveBeenCalledTimes(2)
  expect(w.getComponent(AccountWorkbench).props('scope')).toEqual({ admin: true, vendorId: 8 })
  w.unmount()
})
it('loads all groups only for root all-supplier usage and applies the cross-supplier filter', async () => {
  const { w, router } = await render('proxies')
  expect(client.get).not.toHaveBeenCalled()
  await router.replace({ query: { tab: 'usage' } })
  await flushPromises()
  expect(client.get).toHaveBeenCalledWith('/admin/groups/all', {
    signal: expect.any(AbortSignal),
  })
  const report = w.getComponent(UsageWorkbench)
  const group = report
    .findAllComponents(Select)
    .find((c) => c.props('ariaLabel') === en.group)!
  expect(group.props('options').map((o) => o.label)).toEqual([
    en.all,
    'Shared group',
  ])
  group.vm.$emit('update:modelValue', '3')
  await report.find('form').trigger('submit')
  await flushPromises()
  expect(api.adminUsage).toHaveBeenLastCalledWith(
    expect.objectContaining({ group_id: '3' }),
    expect.any(AbortSignal),
  )
  expect(api.adminUsage.mock.calls.at(-1)![0]).not.toHaveProperty('vendor_id')
  expect(
    w
      .findAllComponents(Select)
      .find((c) => c.props('ariaLabel') === en.vendor)!
      .props('options'),
  ).toContainEqual({ value: 8, label: 'Supplier eight · ' + en.active })
  w.findAllComponents(Select)
    .find((c) => c.props('ariaLabel') === en.vendor)!
    .vm.$emit('update:modelValue', 8)
  await flushPromises()
  expect(client.get).toHaveBeenCalledTimes(1)
  expect(w.getComponent(UsageWorkbench).props('groups')).toEqual(owned)
  expect(api.adminUsage).toHaveBeenLastCalledWith(
    expect.objectContaining({ vendor_id: 8, group_id: '' }),
    expect.any(AbortSignal),
  )
  w.findAllComponents(Select)
    .find((c) => c.props('ariaLabel') === en.vendor)!
    .vm.$emit('update:modelValue', null)
  await flushPromises()
  expect(w.getComponent(UsageWorkbench).props('groups')).toEqual(catalog)
  expect(api.adminUsage.mock.calls.at(-1)![0]).not.toHaveProperty('vendor_id')
  expect(api.adminUsage.mock.calls.at(-1)![0].group_id).toBe('')
  w.unmount()
})
it('offers retry when the group catalog fails instead of silently showing only all groups', async () => {
  client.get.mockRejectedValueOnce(new Error('Group catalog unavailable'))
  const { w } = await render()
  expect(w.find('[role=alert]').text()).toContain('Group catalog unavailable')
  expect(w.findComponent(UsageWorkbench).exists()).toBe(false)
  await w.find('[role=alert] button').trigger('click')
  await flushPromises()
  expect(w.getComponent(UsageWorkbench).props('groups')).toEqual(catalog)
  w.unmount()
})
it('cancels late global catalogs when a supplier is selected', async () => {
  let resolve!: (value: unknown) => void
  client.get.mockImplementationOnce(
    () =>
      new Promise((done) => {
        resolve = done
      }),
  )
  const { w } = await render()
  const signal = client.get.mock.calls[0][1].signal
  w.findAllComponents(Select)
    .find((c) => c.props('ariaLabel') === en.vendor)!
    .vm.$emit('update:modelValue', 8)
  await flushPromises()
  expect(signal.aborted).toBe(true)
  resolve({ data: catalog })
  await flushPromises()
  expect(w.getComponent(UsageWorkbench).props('groups')).toEqual(owned)
  expect(w.text()).not.toContain('Shared group')
  w.unmount()
})
it('does not request the admin group catalog for a non-superadmin', async () => {
  auth.isSuperAdmin = false
  const { w } = await render()
  expect(client.get).not.toHaveBeenCalled()
  expect(api.list).not.toHaveBeenCalled()
  w.unmount()
})
it('opens reports directly from supplier management with that supplier scope', async () => {
  const { w, router } = await render('suppliers')
  w.getComponent(VendorManagement).vm.$emit('navigate', 'usage', 8)
  await flushPromises()
  expect(router.currentRoute.value.query.tab).toBe('usage')
  expect(w.getComponent(UsageWorkbench).props('vendorId')).toBe(8)
  expect(api.adminUsage).toHaveBeenCalledTimes(1)
  expect(api.adminUsage.mock.lastCall![0]).toEqual(
    expect.objectContaining({ vendor_id: 8 }),
  )
  expect(client.get).not.toHaveBeenCalled()
  w.unmount()
})

it.each(['accounts', 'proxies'])('defaults root %s to the complete workbench and scopes management by owner', async (kind) => {
  const { w } = await render(kind)
  const component = kind === 'accounts' ? AccountWorkbench : ProxyWorkbench
  expect(w.getComponent(component).props('scope')).toEqual({ admin: true, readOnly: true, vendorId: 0 })
  if (kind === 'accounts') expect(w.getComponent(AccountWorkbench).props('groups')).toEqual(catalog)
  w.getComponent(component).vm.$emit('selectVendor', 8)
  await flushPromises()
  expect(w.getComponent(component).props('scope')).toEqual({ admin: true, vendorId: 8 })
  expect(api.detail).toHaveBeenCalledWith(8, expect.objectContaining({ signal: expect.any(AbortSignal) }))
  w.findAllComponents(Select).find(c => c.props('ariaLabel') === en.vendor)!.vm.$emit('update:modelValue', null)
  await flushPromises()
  expect(w.getComponent(component).props('scope')).toEqual({ admin: true, readOnly: true, vendorId: 0 })
  w.unmount()
})

vi.mock('@/features/supply/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/supply/services')>('@/features/supply/services')
  const { createSupplyTestServices } = await import('@/features/supply/__tests__/services')
  const { ref } = await import('vue')
  lifecycle.scope = ref<string | null>('session-a:admin')
  const services = createSupplyTestServices({ scopeKey: lifecycle.scope, getAssignableGroups: async ({ signal }) => (await client.get('/admin/groups/all', { signal })).data })
  return { ...actual, useSupplyServices: () => services }
})


it('revokes a pending platform catalog synchronously when super-admin permission disappears', async () => {
  let finish: (value: unknown) => void = () => { throw new Error('No pending catalog') }
  api.list.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const { w } = await render('proxies')
  const signal = api.list.mock.calls[0][1] as AbortSignal
  if (!lifecycle.identity) throw new Error('No identity fixture')
  lifecycle.identity.isSuperAdmin = false
  expect(signal.aborted).toBe(true)
  finish({ items: [{ id: 91, name: 'Private old supplier', status: 'active' }], total: 1 })
  await flushPromises()
  expect(w.findComponent(ProxyWorkbench).exists()).toBe(false)
  expect(w.text()).not.toContain('Private old supplier')
  expect(api.list).toHaveBeenCalledTimes(1)
  w.unmount()
})

it('accepts only the current session catalog after a same-role identity switch', async () => {
  let finish: (value: unknown) => void = () => { throw new Error('No pending catalog') }
  api.list.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    .mockResolvedValue({ items: [{ id: 92, name: 'Current supplier', status: 'active' }], total: 1 })
  const { w } = await render('proxies')
  const oldSignal = api.list.mock.calls[0][1] as AbortSignal
  if (!lifecycle.scope) throw new Error('No session fixture')
  lifecycle.scope.value = 'session-b:admin'
  expect(oldSignal.aborted).toBe(true)
  await flushPromises()
  finish({ items: [{ id: 91, name: 'Private old supplier', status: 'active' }], total: 1 })
  await flushPromises()
  const chooser = w.findAllComponents(Select).find(item => item.props('ariaLabel') === en.vendor)
  expect(chooser?.props('options')).toContainEqual({ value: 92, label: 'Current supplier · ' + en.active })
  expect(chooser?.props('options')).not.toContainEqual(expect.objectContaining({ value: 91 }))
  w.unmount()
  expect((api.list.mock.lastCall?.[1] as AbortSignal).aborted).toBe(true)
})
