import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import postcss from 'postcss'
import tailwindcss from 'tailwindcss'
import autoprefixer from 'autoprefixer'
import tailwindConfig from '../../../../tailwind.config.js'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import { parse, compileStyle } from 'vue/compiler-sfc'
import AppSidebar from '../AppSidebar.vue'
import sidebarSfc from '../AppSidebar.vue?raw'
import versionBadgeSfc from '@/app/components/VersionBadge.vue?raw'
import { createLayoutFixture, layoutRouter } from './layoutTestFixture'

let state: ReturnType<typeof createLayoutFixture>
vi.mock('@/app/model/app', () => ({ useAppStore: () => state.app }))
vi.mock('@/features/identity', async original => ({ ...await original<typeof import('@/features/identity')>(), useAuthStore: () => state.auth }))
vi.mock('@/app/model/adminSettings', () => ({ useAdminSettingsStore: () => state.admin }))
vi.mock('@/app/onboarding/store', () => ({ useOnboardingStore: () => state.onboarding }))
vi.mock('@/features/jobs', () => ({ useBatchImageAccess: () => state.batchImage }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const sheets: HTMLStyleElement[] = []
let appStyles = ''
beforeAll(async () => {
  const path = resolve(process.cwd(), 'src/style.css')
  const config = { ...tailwindConfig, content: [{ raw: sidebarSfc, extension: 'vue' }, { raw: versionBadgeSfc, extension: 'vue' }] }
  const result = await postcss([tailwindcss(config), autoprefixer()]).process(readFileSync(path, 'utf8'), { from: path })
  appStyles = result.css
})
function installStyles() {
  // Use the actual compiled global CSS and SFC stylesheet, then inspect rendered CSS behavior.
  const scopeId = Reflect.get(AppSidebar, '__scopeId')
  if (typeof scopeId !== 'string' || !appStyles.trim()) throw new Error('Real sidebar styles were not available: scope=' + String(scopeId) + ', CSS bytes=' + appStyles.length)
  const styles = parse(sidebarSfc).descriptor.styles.map(style => {
    const result = compileStyle({ source: style.content, filename: 'AppSidebar.vue', id: scopeId, scoped: style.scoped })
    if (result.errors.length) throw result.errors[0]
    return result.code
  })
  for (const content of [appStyles, ...styles]) {
    const sheet = document.createElement('style'); sheet.textContent = content; document.head.appendChild(sheet); sheets.push(sheet)
  }
}
enableAutoUnmount(afterEach)
beforeEach(() => {
  localStorage.clear()
  state = createLayoutFixture()
  installStyles()
})
afterEach(() => {
  for (const sheet of sheets) sheet.remove()
  sheets.length = 0
  document.documentElement.classList.remove('dark')
  document.body.innerHTML = ''
})
async function sidebar(path = '/admin/dashboard', realVersion = false) {
  const router = await layoutRouter(path)
  const wrapper = shallowMount(AppSidebar, { attachTo: document.body, global: { plugins: [router], provide: state.provide,
    stubs: { RouterLink: false, VersionBadge: !realVersion } } })
  await flushPromises()
  return { wrapper, router }
}

describe('AppSidebar navigation and presentation behavior', () => {
  it.each([false, true])('shows version details only to super admins, admin=%s', async isAdmin => {
    state.auth.isAdmin = isAdmin
    const { wrapper } = await sidebar(isAdmin ? '/admin/dashboard' : '/dashboard')
    expect(wrapper.findComponent({ name: 'VersionBadge' }).exists()).toBe(false)
    state.auth.isSuperAdmin = true
    await flushPromises()
    expect(wrapper.findComponent({ name: 'VersionBadge' }).exists()).toBe(true)
    state.auth.isSuperAdmin = false
    await flushPromises()
    expect(wrapper.findComponent({ name: 'VersionBadge' }).exists()).toBe(false)
  })

  it('renders the contributed user order and reacts to the existing batch access fact', async () => {
    state.auth.isAdmin = false
    const { wrapper } = await sidebar('/dashboard')
    const paths = () => wrapper.findAll('nav a').map(link => link.attributes('href'))
    expect(paths()).toEqual(['/dashboard', '/keys', '/usage', '/available-channels', '/monitor', '/subscriptions', '/purchase', '/orders', '/profile'])
    state.batchImage.canUseBatchImage.value = true
    await flushPromises()
    expect(paths()).toEqual(['/dashboard', '/keys', '/batch-image', '/usage', '/available-channels', '/monitor', '/subscriptions', '/purchase', '/orders', '/profile'])
    expect(wrapper.get('a[href="/keys"]').attributes('data-tour')).toBe('sidebar-my-keys')
  })

  it('preserves simple admin placement, hides its personal section and keeps tour selectors', async () => {
    state.auth.isSimpleMode = true
    const { wrapper } = await sidebar()
    const paths = wrapper.findAll('nav a').map(link => link.attributes('href'))
    expect(paths.slice(-2)).toEqual(['/keys', '/admin/settings'])
    expect(paths).not.toContain('/admin/users')
    expect(paths).not.toContain('/subscriptions')
    expect(paths.filter(path => path === '/keys')).toHaveLength(1)
    expect(wrapper.find('.sidebar-section-title').exists()).toBe(false)
    expect(wrapper.get('a[href="/admin/groups"]').attributes('id')).toBe('sidebar-group-manage')
    expect(wrapper.get('a[href="/admin/accounts"]').attributes('id')).toBe('sidebar-channel-manage')
  })

  it('keeps custom item ownership, visibility and sort order after built-in contributions', async () => {
    const item = (id: string, visibility: 'user' | 'admin', sort_order: number) => ({
      id, label: id, visibility, sort_order, icon_svg: '', url: 'https://example.test/' + id,
    })
    state.app.cachedPublicSettings.custom_menu_items = [item('user-later', 'user', 20), item('hidden-admin', 'admin', 0), item('user-first', 'user', 10)]
    state.admin.customMenuItems = [item('admin-later', 'admin', 20), item('hidden-user', 'user', 0), item('admin-first', 'admin', 10)]
    const { wrapper } = await sidebar()
    const customPaths = () => wrapper.findAll('nav a').map(link => link.attributes('href')).filter(path => path?.startsWith('/custom/'))
    expect(customPaths()).toEqual(['/custom/admin-first', '/custom/admin-later', '/custom/user-first', '/custom/user-later'])
    state.auth.isAdmin = false
    await flushPromises()
    expect(customPaths()).toEqual(['/custom/user-first', '/custom/user-later'])
    state.app.backendModeEnabled = true
    await flushPromises()
    expect(wrapper.findAll('nav a')).toHaveLength(0)
  })

  it('keeps supplier administration out while honoring individual admin page permissions', async () => {
    const { wrapper } = await sidebar()
    expect(wrapper.find('a[href="/admin/vendors"]').exists()).toBe(false)
    expect(wrapper.find('a[href="/admin/users"]').exists()).toBe(true)
    expect(wrapper.find('a[href="/admin/accounts"]').exists()).toBe(true)
    state.deniedPages.add('/admin/users')
    await flushPromises()
    expect(wrapper.find('a[href="/admin/users"]').exists()).toBe(false)
    expect(wrapper.find('a[href="/admin/accounts"]').exists()).toBe(true)
  })

  it('preserves uploaded SVG colors and rejects active SVG content', async () => {
    state.auth.isAdmin = false
    state.app.cachedPublicSettings.custom_menu_items = [{ id: 'colored', label: 'Colored', url: 'https://docs.example.test/custom', visibility: 'user', sort_order: 0,
      icon_svg: '<svg viewBox="0 0 16 16" fill="#ff3300" stroke="#2255cc" onload="window.__sidebarXss=1"><path d="M0 0h16v16H0z"/><script>window.__sidebarXss=1</script></svg>' }]
    const { wrapper } = await sidebar('/dashboard')
    const svg = wrapper.get('a[href="/custom/colored"] svg')
    expect(svg.attributes('fill')).toBe('#ff3300'); expect(svg.attributes('stroke')).toBe('#2255cc')
    expect(svg.attributes('onload')).toBeUndefined(); expect(svg.find('script').exists()).toBe(false)
    const reference = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
    reference.setAttribute('fill', '#ff3300'); reference.setAttribute('stroke', '#2255cc'); document.body.appendChild(reference)
    expect(getComputedStyle(svg.element).fill).toBe(getComputedStyle(reference).fill)
    expect(getComputedStyle(svg.element).stroke).toBe(getComputedStyle(reference).stroke)
    expect(getComputedStyle(svg.element).display).toBe('block')
    reference.remove()
  })

  it('saves the actual navigation scroll offset on unmount and restores it on remount', async () => {
    const first = await sidebar()
    const navigation = first.wrapper.get('nav.sidebar-nav').element
    navigation.scrollTop = 147
    first.wrapper.unmount()
    expect(state.app.sidebarScrollTop).toBe(147)
    const second = await sidebar('/admin/accounts')
    expect(second.wrapper.get('nav.sidebar-nav').element.scrollTop).toBe(147)
  })

  it('allows an active navigation group to collapse and reopen without leaving the route', async () => {
    const { wrapper, router } = await sidebar('/admin/channels/pricing')
    expect(wrapper.find('a[href="/admin/channels/pricing"]').exists()).toBe(true)
    const group = wrapper.findAll('button').find(button => button.text().includes('nav.channelManagement'))!
    await group.trigger('click')
    expect(wrapper.find('a[href="/admin/channels/pricing"]').exists()).toBe(false)
    expect(router.currentRoute.value.path).toBe('/admin/channels/pricing')
    await group.trigger('click')
    expect(wrapper.find('a[href="/admin/channels/pricing"]').exists()).toBe(true)
  })

  it('opens the real version details without a clipping ancestor in the sidebar header', async () => {
    state.auth.isSuperAdmin = true
    const { wrapper } = await sidebar('/admin/dashboard', true)
    await wrapper.get('[aria-label="version.details"]').trigger('click')
    expect(wrapper.get('[data-testid="current-version"]').text()).toBe('v1.2.3')
    expect(getComputedStyle(wrapper.get('.sidebar-header').element).display).toBe('flex')
    for (const selector of ['.sidebar-header', '.sidebar-brand']) {
      const style = getComputedStyle(wrapper.get(selector).element)
      expect([style.overflow, style.overflowX, style.overflowY]).not.toContain('hidden')
      expect([style.overflow, style.overflowX, style.overflowY]).not.toContain('clip')
    }
  })

  it.each([false, true])('reactively gates personal and admin subscriptions for admin=%s', async isAdmin => {
    state.auth.isAdmin = isAdmin
    const { wrapper } = await sidebar(isAdmin ? '/admin/dashboard' : '/dashboard')
    expect(wrapper.find('a[href="/subscriptions"]').exists()).toBe(true)
    if (isAdmin) expect(wrapper.find('a[href="/admin/subscriptions"]').exists()).toBe(true)
    state.app.cachedPublicSettings.subscription_enabled = false
    await flushPromises()
    expect(wrapper.find('a[href="/subscriptions"]').exists()).toBe(false)
    expect(wrapper.find('a[href="/admin/subscriptions"]').exists()).toBe(false)
    state.app.cachedPublicSettings.subscription_enabled = true
    await flushPromises()
    expect(wrapper.find('a[href="/subscriptions"]').exists()).toBe(true)
  })

  it.each([
    [false, false, 'nav.recharge'], [true, true, 'nav.subscribe'], [true, false, 'nav.buySubscription'],
  ] as const)('renders the billing-mode purchase label for subscription=%s balance-disabled=%s', async (subscription, disabledBalance, label) => {
    state.auth.isAdmin = false
    state.app.cachedPublicSettings.subscription_enabled = subscription
    state.app.cachedPublicSettings.payment_balance_disabled = disabledBalance
    const { wrapper } = await sidebar('/dashboard')
    expect(wrapper.get('a[href="/purchase"]').text()).toBe(label)
  })
})
