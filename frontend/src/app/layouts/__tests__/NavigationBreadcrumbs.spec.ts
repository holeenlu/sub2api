import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { RouterView } from 'vue-router'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import AppHeader from '../AppHeader.vue'
import AppSidebar from '../AppSidebar.vue'
import { createLayoutFixture, layoutRouter } from './layoutTestFixture'

let state: ReturnType<typeof createLayoutFixture>
vi.mock('@/app/model/app', () => ({ useAppStore: () => state.app }))
vi.mock('@/features/identity', async original => ({ ...await original<typeof import('@/features/identity')>(), useAuthStore: () => state.auth }))
vi.mock('@/app/model/adminSettings', () => ({ useAdminSettingsStore: () => state.admin }))
vi.mock('@/app/onboarding/store', () => ({ useOnboardingStore: () => state.onboarding }))
vi.mock('@/features/jobs', () => ({ useBatchImageAccess: () => state.batchImage }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { state = createLayoutFixture() })
afterEach(() => { document.body.innerHTML = '' })

async function shell(path: string) {
  const router = await layoutRouter(path)
  const wrapper = shallowMount(defineComponent({
    components: { AppHeader, AppSidebar }, template: '<div><AppSidebar /><AppHeader /></div>',
  }), { attachTo: document.body, global: { plugins: [router], provide: state.provide,
    stubs: { AppHeader: false, AppSidebar: false, NavigationBreadcrumbs: false, RouterLink: false },
  } })
  await flushPromises()
  const breadcrumb = () => wrapper.get('nav[aria-label="ui.breadcrumb"]')
  const current = () => breadcrumb().get('[aria-current="page"]')
  return { wrapper, router, breadcrumb, current }
}

describe('production console menu, title and breadcrumb consumers', () => {
  it('renders contributed admin groups as text and follows real redirects and aliases', async () => {
    const { wrapper, router, breadcrumb, current } = await shell('/admin/channels')
    expect(router.currentRoute.value.path).toBe('/admin/channels/pricing')
    expect(breadcrumb().findAll('li').map(item => item.text())).toEqual(['nav.channelManagement', '/admin.channels.title'])
    expect(current().text()).toBe(wrapper.get('h1').text())
    expect(breadcrumb().findAll('a')).toHaveLength(0)
    expect(wrapper.get('aside a[href="/admin/channels/pricing"]').text()).toBe('nav.channelPricing')
    await router.push('/docs/batch-image')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('BatchImageGuide')
    expect(current().text()).toBe('batchImageGuide.title')
    expect(current().text()).toBe(wrapper.get('h1').text())
    expect(breadcrumb().findAll('[aria-current]')).toHaveLength(1)
    expect(current().element.tagName).toBe('SPAN')
  })

  it('updates purchase labels in the existing menu, heading and breadcrumb together', async () => {
    state.auth.isAdmin = false
    const { wrapper, current } = await shell('/purchase')
    for (const [subscription, balanceDisabled, label] of [
      [true, false, 'nav.buySubscription'], [false, false, 'nav.recharge'], [true, true, 'nav.subscribe'],
    ] as const) {
      state.app.cachedPublicSettings.subscription_enabled = subscription
      state.app.cachedPublicSettings.payment_balance_disabled = balanceDisabled
      await flushPromises()
      expect(wrapper.get('aside a[href="/purchase"]').text()).toBe(label)
      expect(wrapper.get('h1').text()).toBe(label)
      expect(current().text()).toBe(label)
    }
    expect(state.batchImage.refreshBatchImageAccess).toHaveBeenCalledTimes(1)
  })

  it('keeps valid user deep links visible even when simple mode hides their menu', async () => {
    state.auth.isAdmin = false
    state.auth.isSimpleMode = true
    const { wrapper, current, router } = await shell('/usage?model=retained')
    expect(wrapper.find('aside a[href="/usage"]').exists()).toBe(false)
    expect(current().text()).toBe(wrapper.get('h1').text())
    expect(current().text()).toBe('usage.title')
    expect(router.currentRoute.value.fullPath).toBe('/usage?model=retained')
  })

  it('uses the existing payment visibility and admin permissions for group ancestry', async () => {
    const { wrapper, router, breadcrumb, current } = await shell('/admin/orders/plans')
    expect(breadcrumb().text()).toContain('nav.orderManagement')
    state.admin.paymentEnabled = false
    await flushPromises()
    expect(breadcrumb().text()).not.toContain('nav.orderManagement')
    expect(current().text()).toBe(wrapper.get('h1').text())
    await router.push('/admin/channels/pricing')
    state.deniedPages.add('/admin/channels/pricing')
    await flushPromises()
    expect(breadcrumb().text()).not.toContain('nav.channelManagement')
    expect(wrapper.find('aside a[href="/admin/channels/pricing"]').exists()).toBe(false)
    expect(current().text()).toBe('admin.channels.title')
  })

  it('reacts to user/admin custom labels without rendering label HTML', async () => {
    const item = (id: string, visibility: 'admin' | 'user', label: string) => ({
      id, visibility, label, sort_order: 0, icon_svg: '', url: 'https://example.test/',
    })
    state.app.cachedPublicSettings.custom_menu_items = [item('status', 'user', 'Service status')]
    state.admin.customMenuItems = [item('private', 'admin', 'Admin report')]
    const { wrapper, router, current } = await shell('/custom/status')
    expect(current().text()).toBe('Service status')
    state.app.cachedPublicSettings.custom_menu_items[0]!.label = '<img src=x onerror=alert(1)>LongLabel'.repeat(8)
    await flushPromises()
    expect(current().text()).toBe(wrapper.get('h1').text())
    expect(current().find('img').exists()).toBe(false)
    expect(current().classes()).toContain('[overflow-wrap:anywhere]')
    expect(current().element.closest('ol')?.classList.contains('flex-wrap')).toBe(true)
    await router.push('/custom/private')
    await flushPromises()
    expect(current().text()).toBe('Admin report')
    state.admin.customMenuItems[0]!.label = 'Renamed report'
    await flushPromises()
    expect(wrapper.get('aside a[href="/custom/private"]').text()).toBe('Renamed report')
    expect(current().text()).toBe('Renamed report')
    state.auth.isAdmin = false
    await flushPromises()
    expect(current().text()).toBe('customPage.title')
  })

  it('resolves real parameterized parents and removes denied or disabled ancestor links', async () => {
    const { wrapper, router, breadcrumb, current } = await shell('/admin/dashboard')
    router.addRoute({ path: '/admin/reports/:id', name: 'ReportParent', component: RouterView,
      meta: { shell: 'admin', titleKey: 'report.parent',
        navigation: { order: 1, icon: 'chart', feature: 'opsMonitoring', group: 'reports' },
        navigationGroup: { id: 'reports', labelKey: 'report.group', icon: 'chart', order: 1, surface: 'admin', feature: 'adminPayment' },
      },
      children: [{ path: 'detail', name: 'ReportDetail', component: RouterView, meta: { titleKey: 'report.detail' } }],
    })
    await router.push('/admin/reports/report-7/detail')
    await flushPromises()
    const parent = breadcrumb().get('a')
    expect(parent.attributes('href')).toBe('/admin/reports/report-7')
    expect(parent.attributes('tabindex')).toBeUndefined()
    expect(parent.classes()).toContain('focus-visible:ring-2')
    ;(parent.element as HTMLAnchorElement).focus()
    expect(document.activeElement).toBe(parent.element)
    expect(current().text()).toBe('report.detail')
    expect(wrapper.get('h1').text()).toBe('report.detail')
    await parent.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/reports/report-7')
    await router.push('/admin/reports/report-7/detail')
    state.deniedPages.add('/admin/reports/:id')
    await flushPromises()
    expect(breadcrumb().findAll('a')).toHaveLength(0)
    expect(current().text()).toBe('report.detail')
    state.deniedPages.clear()
    state.admin.opsMonitoringEnabled = false
    await flushPromises()
    expect(breadcrumb().findAll('a')).toHaveLength(0)
    state.admin.opsMonitoringEnabled = true
    await flushPromises()
    expect(breadcrumb().get('a').attributes('href')).toBe('/admin/reports/report-7')
    state.admin.paymentEnabled = false
    await flushPromises()
    expect(breadcrumb().findAll('a')).toHaveLength(0)
    expect(breadcrumb().text()).not.toContain('report.group')
    expect(current().text()).toBe('report.detail')
  })
})
