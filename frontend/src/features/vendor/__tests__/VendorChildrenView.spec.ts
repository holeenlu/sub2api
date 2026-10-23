import { beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import VendorChildrenView from '../components/VendorChildrenView.vue'
import AccountWorkbench from '../components/AccountWorkbench.vue'
import ProxyWorkbench from '../components/ProxyWorkbench.vue'
import Select from '@/components/common/Select.vue'
import nativeEn from '@/i18n/locales/en'
import type { Vendor } from '../types'

const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn() }))
vi.mock('../api', () => ({ vendorManagementAPI: () => api }))
const parent = { id: 1, name: 'Parent', groups: [{ id: 4, name: 'Allowed group' }] } as Vendor
beforeEach(() => {
  vi.resetAllMocks()
  api.list.mockResolvedValue({ items: [{ id: 2, name: 'Child two' }, { id: 3, name: 'Child three' }], total: 2 })
  api.detail.mockImplementation(async id => ({ id, name: 'Child ' + id, groups: [] }))
})
async function render(kind: 'accounts' | 'proxies') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/children', component: VendorChildrenView }] })
  await router.push('/children')
  await router.isReady()
  const wrapper = mount(VendorChildrenView, {
    props: { parent, kind },
    global: {
      plugins: [router, createI18n({ legacy: false, locale: 'en', messages: { en: nativeEn } })],
      stubs: { AccountWorkbench: true, ProxyWorkbench: true, UsageWorkbench: true, Pagination: true },
    },
  })
  await flushPromises()
  return { wrapper, router }
}
it.each(['accounts', 'proxies'] as const)('opens all children %s immediately, with optional owner filtering', async kind => {
  const { wrapper, router } = await render(kind)
  const component = kind === 'accounts' ? AccountWorkbench : ProxyWorkbench
  expect(wrapper.getComponent(component).props('scope')).toEqual({ admin: false, readOnly: true, children: true })
  expect(api.detail).not.toHaveBeenCalled()
  expect(wrapper.text()).not.toContain(nativeEn.vendor.selectVendor)
  wrapper.getComponent(component).vm.$emit('selectVendor', 3)
  await flushPromises()
  expect(router.currentRoute.value.query.vendor_id).toBe('3')
  expect(wrapper.getComponent(component).props('scope')).toEqual({ admin: false, readOnly: true, vendorId: 3 })
  wrapper.getComponent(Select).vm.$emit('update:modelValue', null)
  await flushPromises()
  expect(wrapper.getComponent(component).props('scope')).toEqual({ admin: false, readOnly: true, children: true })
  wrapper.unmount()
})
it('never falls back to the parent or all resources while a selected child is unavailable', async () => {
  const { wrapper, router } = await render('accounts')
  let finish!: (value: unknown) => void
  api.detail.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  await router.replace({ query: { vendor_id: '2' } })
  await flushPromises()
  expect(wrapper.findComponent(AccountWorkbench).exists()).toBe(false)
  await router.replace({ query: {} })
  await flushPromises()
  finish({ id: 2, name: 'Late child', groups: [] })
  await flushPromises()
  expect(wrapper.getComponent(AccountWorkbench).props('scope')).toEqual({ admin: false, readOnly: true, children: true })
  expect(wrapper.getComponent(AccountWorkbench).props('vendor')).toBeUndefined()
  api.detail.mockRejectedValueOnce(new Error('No access to supplier'))
  await router.replace({ query: { vendor_id: '99' } })
  await flushPromises()
  expect(wrapper.findComponent(AccountWorkbench).exists()).toBe(false)
  expect(wrapper.get('[role=alert]').text()).toContain('No access to supplier')
  wrapper.unmount()
})
