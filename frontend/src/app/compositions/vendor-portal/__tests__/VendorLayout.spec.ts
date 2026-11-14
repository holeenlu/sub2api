import { afterEach, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { layoutRouter } from '@/app/layouts/__tests__/layoutTestFixture'
import VendorLayout from '../components/VendorLayout.vue'
import { useAppStore } from '@/app/model/app'
import { useAuthStore } from '@/features/identity'
import { loadApplicationLocale } from '@/app/i18n'
import imageReferences from '@/shared/lib/__tests__/fixtures/imageReferences.json'
const assembledEnMessages = await loadApplicationLocale('en')
const en = assembledEnMessages

const media = vi.hoisted(() => ({ desktop: true }))
vi.mock('@vueuse/core', async original => ({ ...await original<typeof import('@vueuse/core')>(), useMediaQuery: () => ref(media.desktop) }))
afterEach(() => { media.desktop = true })
async function render(role = 'super_admin', options: { publicSettingsLoaded?: boolean; beforeMount?: (app: ReturnType<typeof useAppStore>) => void } = {}) {
  const pinia = createPinia()
  const app = useAppStore(pinia)
  app.siteName = 'Supplier brand'
  if (options.publicSettingsLoaded !== undefined) app.publicSettingsLoaded = options.publicSettingsLoaded
  options.beforeMount?.(app)
  useAuthStore(pinia).user = { id: 1, username: 'Manager', role, email: 'manager@example.invalid' } as any
  const router = await layoutRouter(role === 'super_admin' ? '/vendor/platform?tab=accounts' : '/vendor/accounts')
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })
  const wrapper = mount(VendorLayout, {
    attachTo: document.body, global: {
      plugins: [pinia, router, i18n],
      stubs: { VersionBadge: { props: ['portal'], template: '<span data-testid="version">vTest</span>' }, LocaleSwitcher: true, DisplayModeSwitcher: true, TotpStepUpDialog: true },
    }, slots: { default: '<div>Page content</div>' }
  })
  await flushPromises()
  return { wrapper, app, router, i18n, auth: useAuthStore(pinia) }
}
it('loads the backend public brand before showing the supplier logo', async () => {
  let fetchCount = 0
  const { wrapper, app } = await render('vendor_level1', {
    publicSettingsLoaded: false,
    beforeMount: store => {
      vi.spyOn(store, 'fetchPublicSettings').mockImplementation(async () => {
        fetchCount += 1
        store.siteName = 'Backend Supplier Brand'
        store.siteLogo = '/backend-supplier-logo.svg'
        store.publicSettingsLoaded = true
        return null
      })
    }
  })
  try {
    expect(fetchCount).toBe(1)
    expect(wrapper.get('.sidebar-logo img').attributes('src')).toBe('/backend-supplier-logo.svg')
    expect(wrapper.get('.sidebar-brand-title').text()).toBe('Backend Supplier Brand')
    expect(app.siteLogo).toBe('/backend-supplier-logo.svg')
  } finally { wrapper.unmount() }
})
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
  expect(wrapper.find('[data-testid="version"]').exists()).toBe(false)
  const hrefs = wrapper.findAll('nav a').map(link => link.attributes('href'))
  expect(hrefs).not.toContain('/vendor/platform?tab=suppliers')
  expect(hrefs.includes('/vendor/children')).toBe(role === 'vendor_level1')
  expect(hrefs).toContain('/vendor/settlements')
  wrapper.unmount()
})
it('applies the shared image reference contract when the supplier logo changes', async () => {
  const { wrapper, app } = await render('vendor_level1')
  try {
    for (const { name, input, expected } of imageReferences) {
      app.siteLogo = input
      await flushPromises()
      expect(wrapper.get('.sidebar-logo img').attributes('src'), name).toBe(expected || '/logo.svg')
    }
  } finally { wrapper.unmount() }
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

it('keeps platform query tabs, menu selection, heading and breadcrumb on one contribution', async () => {
  const { wrapper, router, i18n } = await render()
  try {
    for (const tab of ['suppliers', 'accounts', 'proxies', 'usage', 'settlements', 'supply', 'audit']) {
      await router.push({ path: '/vendor/platform', query: { tab, vendor_id: '12' } })
      await flushPromises()
      const breadcrumb = wrapper.get('nav[aria-label="Breadcrumb"]')
      const label = i18n.global.t('vendor.' + tab)
      expect(wrapper.get('h1').text()).toBe(label)
      expect(breadcrumb.get('[aria-current="page"]').text()).toBe(label)
      expect(wrapper.get('.sidebar-link-active').text()).toBe(label)
      expect(wrapper.findAll('aside nav [aria-current="page"]')).toHaveLength(1)
      expect(breadcrumb.findAll('a')).toHaveLength(0)
      expect(breadcrumb.findAll('li')[0]!.text()).toBe(i18n.global.t('vendor.supplierManagement'))
      expect(router.currentRoute.value.query.vendor_id).toBe('12')
    }
    await router.push('/vendor/platform')
    await flushPromises()
    expect(wrapper.get('nav[aria-label="Breadcrumb"] [aria-current="page"]').text()).toBe(i18n.global.t('vendor.suppliers'))
  } finally { wrapper.unmount() }
})

it.each(['vendor_level1', 'vendor_level2'])('shares %s section and page labels without creating group destinations', async role => {
  const { wrapper, router, i18n } = await render(role)
  try {
    for (const path of ['/vendor/accounts', '/vendor/proxies', '/vendor/usage', '/vendor/settlements']) {
      await router.push(path)
      await flushPromises()
      const breadcrumb = wrapper.get('nav[aria-label="Breadcrumb"]')
      expect(breadcrumb.get('[aria-current="page"]').text()).toBe(wrapper.get('h1').text())
      expect(wrapper.get('.sidebar-link-active').text()).toBe(wrapper.get('h1').text())
      expect(breadcrumb.findAll('a')).toHaveLength(0)
    }
    await router.push('/vendor/security')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/vendor/profile')
    expect(wrapper.get('h1').text()).toBe(i18n.global.t('vendor.accountSettings'))
    expect(wrapper.get('nav[aria-label="Breadcrumb"] [aria-current="page"]').text()).toBe(wrapper.get('h1').text())
    expect(wrapper.findAll('nav[aria-label="Breadcrumb"] li')).toHaveLength(1)
  } finally { wrapper.unmount() }
})

it('drops level-one ancestry when the role changes and preserves existing child deep links', async () => {
  const { wrapper, router, auth, i18n } = await render('vendor_level1')
  try {
    await router.push('/vendor/child-accounts?vendor_id=19')
    await flushPromises()
    expect(wrapper.get('nav[aria-label="Breadcrumb"]').text()).toContain(i18n.global.t('vendor.downstream'))
    expect(wrapper.get('nav[aria-label="Breadcrumb"] [aria-current="page"]').text()).toBe(i18n.global.t('vendor.childAccounts'))
    auth.user!.role = 'vendor_level2'
    await flushPromises()
    expect(wrapper.find('aside a[href="/vendor/child-accounts"]').exists()).toBe(false)
    expect(wrapper.get('nav[aria-label="Breadcrumb"]').text()).not.toContain(i18n.global.t('vendor.downstream'))
    expect(wrapper.get('h1').text()).toBe(i18n.global.t('vendor.portal'))
    expect(router.currentRoute.value.fullPath).toBe('/vendor/child-accounts?vendor_id=19')
    auth.user!.role = 'vendor_level1'
    await router.push('/vendor/child-usage?vendor_id=19')
    await flushPromises()
    expect(wrapper.get('nav[aria-label="Breadcrumb"] [aria-current="page"]').text()).toBe(wrapper.get('h1').text())
    expect(router.currentRoute.value.params.tab).toBe('child-usage')
  } finally { wrapper.unmount() }
})

vi.mock('@/features/supply/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/supply/services')>('@/features/supply/services')
  const { createSupplyTestServices } = await import('@/features/supply/__tests__/services')
  const services = createSupplyTestServices()
  return { ...actual, useSupplyServices: () => services }
})
