import { afterEach, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import VendorLayout from '../components/VendorLayout.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import en from '@/i18n/locales/en'

const media = vi.hoisted(() => ({ desktop: true }))
vi.mock('@vueuse/core', async original => ({ ...await original<typeof import('@vueuse/core')>(), useMediaQuery: () => ref(media.desktop) }))
afterEach(() => { media.desktop = true })
async function render(role = 'super_admin') {
  const pinia = createPinia()
  const app = useAppStore(pinia)
  app.siteName = 'Supplier brand'
  useAuthStore(pinia).user = { id: 1, username: 'Manager', role, email: 'manager@example.invalid' } as any
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
  await router.push(role === 'super_admin' ? '/vendor/platform?tab=accounts' : '/vendor/accounts')
  const wrapper = mount(VendorLayout, { attachTo: document.body, global: {
    plugins: [pinia, router, createI18n({ legacy: false, locale: 'en', messages: { en } })],
    stubs: { VersionBadge: { props: ['portal'], template: '<span data-testid="version">vTest</span>' }, LocaleSwitcher: true, DisplayModeSwitcher: true, TotpStepUpDialog: true },
  }, slots: { default: '<div>Page content</div>' } })
  await flushPromises()
  return { wrapper, app, router }
}
it('uses the native sidebar header once and preserves tab selection when collapsed', async () => {
  const { wrapper, app, router } = await render()
  expect(wrapper.get('aside').classes()).toContain('w-64')
  expect(wrapper.get('aside .sidebar-header').text()).toContain('Supplier brand')
  expect(wrapper.findAll('[data-testid="version"]')).toHaveLength(1)
  expect(wrapper.find('header img').exists()).toBe(false)
  expect(wrapper.find('.sidebar-section-title').exists()).toBe(false)
  expect(wrapper.get('.sidebar-link-active').attributes('href')).toBe('/vendor/platform?tab=accounts')
  await wrapper.get('button[aria-label="Collapse"]').trigger('click')
  expect(app.sidebarCollapsed).toBe(true)
  expect(wrapper.get('aside').classes()).toContain('w-[72px]')
  expect(wrapper.get('.sidebar-link-active').classes()).toContain('sidebar-link-collapsed')
  await router.push('/vendor/platform?tab=settlements')
  await flushPromises()
  expect(wrapper.get('.sidebar-link-active').attributes('href')).toBe('/vendor/platform?tab=settlements')
  wrapper.unmount()
})
it.each(['vendor_level1', 'vendor_level2'])('keeps %s navigation scoped while showing the shared brand', async role => {
  const { wrapper } = await render(role)
  expect(wrapper.get('.sidebar-header').text()).toContain('Supplier brand')
  const hrefs = wrapper.findAll('nav a').map(link => link.attributes('href'))
  expect(hrefs).not.toContain('/vendor/platform?tab=suppliers')
  expect(hrefs.includes('/vendor/children')).toBe(role === 'vendor_level1')
  expect(hrefs).toContain('/vendor/settlements')
  wrapper.unmount()
})
it('keeps mobile drawer isolation and restores focus after Escape', async () => {
  media.desktop = false
  const { wrapper, app } = await render()
  app.sidebarCollapsed = true
  await flushPromises()
  const trigger = wrapper.get('button[aria-controls="vendor-navigation"]')
  expect(wrapper.get('aside').attributes()).toHaveProperty('inert')
  await trigger.trigger('click')
  await flushPromises()
  expect(wrapper.get('aside').attributes('role')).toBe('dialog')
  expect(wrapper.get('aside').classes()).toContain('w-64')
  expect(wrapper.get('main').element.parentElement?.hasAttribute('inert')).toBe(true)
  await wrapper.get('aside').trigger('keydown', { key: 'Escape' })
  await flushPromises()
  expect(trigger.attributes('aria-expanded')).toBe('false')
  expect(document.activeElement).toBe(trigger.element)
  wrapper.unmount()
})
