import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, nextTick, reactive, ref, type Component } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'

type PanelModule = { default: Component }

const harness = vi.hoisted(() => ({
  loadLayout: vi.fn<() => Promise<PanelModule>>(),
  loadPopup: vi.fn<() => Promise<PanelModule>>(),
  loadCompliance: vi.fn<() => Promise<PanelModule>>(),
  fetchCompliance: vi.fn<() => Promise<void>>(),
}))

const auth = reactive({
  isAuthenticated: false, identityScopeKey: 'anonymous', isAdmin: false, isSuperAdmin: false,
})
const announcements = reactive({
  currentPopup: null as { id: number } | null,
  setScope: vi.fn(), fetchAnnouncements: vi.fn().mockResolvedValue(undefined),
  reset: () => { announcements.currentPopup = null },
})
const compliance = reactive({
  shouldShow: false,
  fetchStatus: () => harness.fetchCompliance(),
  reset: () => { compliance.shouldShow = false },
})
const settings = {
  siteName: 'sub2api', publicSettingsLoaded: true, cachedPublicSettings: {},
  fetchPublicSettings: vi.fn().mockResolvedValue(undefined),
}

vi.mock('@/features/identity', () => ({
  useAuthStore: () => auth, useAdminComplianceStore: () => compliance,
  globalAdminStepUp: { onCancel: vi.fn() },
}))
vi.mock('@/features/identity/panelLoaders', () => ({ loadAdminComplianceDialog: () => harness.loadCompliance() }))
vi.mock('@/features/site/announcementPanels', () => ({ loadAnnouncementPopup: () => harness.loadPopup() }))
vi.mock('@/features/site/announcements', () => ({ useAnnouncementStore: () => announcements }))
vi.mock('@/features/subscriptions', () => ({
  useSubscriptionStore: () => ({ setScope: vi.fn(), stopPolling: vi.fn(), clear: vi.fn() }),
}))
vi.mock('@/features/supply', () => ({ isVendorOrigin: () => false }))
vi.mock('@/app/navigation/featureFlags', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/app/navigation/featureFlags')>()),
  isFeatureFlagEnabled: () => false,
}))
vi.mock('@/app/model/app', () => ({ useAppStore: () => settings }))
vi.mock('@/app/model/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [], setScope: vi.fn() }) }))
vi.mock('@/features/system/api', () => ({ getSetupStatus: async () => ({ needs_setup: false }) }))
vi.mock('@/app/router/title', () => ({ applyRouteMetaDescription: vi.fn(), resolveRouteDocumentTitle: () => 'sub2api' }))
vi.mock('@/app/components/Toast.vue', () => ({ default: { render: () => null } }))
vi.mock('@/app/components/NavigationProgress.vue', () => ({ default: { render: () => null } }))
vi.mock('@/shared/ui/LoadingSpinner.vue', () => ({ default: { render: () => h('p', { role: 'status' }, 'Loading') } }))
vi.mock('../layouts/AppLayout.vue', async () => ({ __esModule: true, ...(await harness.loadLayout()) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: ref('en'), t: (key: string) => key }) }))

function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve: (value: T) => void = () => { throw new Error('Deferred module is not initialized') }
  const promise = new Promise<T>(finish => { resolve = finish })
  return { promise, resolve }
}

function panel(name: string): PanelModule {
  // Vue unwraps actual ES module namespaces; mirror their module marker.
  return Object.defineProperty({ default: defineComponent({ render: () => h('section', { 'data-test': name }, name) }) }, Symbol.toStringTag, { value: 'Module' })
}

let wrapper: VueWrapper | undefined

async function mountApp(path = '/home') {
  // Each application instance owns fresh async wrappers, so prior resolved
  // modules cannot conceal eager loading in a later scenario.
  const { default: App } = await import('../App.vue')
  const page = defineComponent({ render: () => h('p', { 'data-test': 'page' }, 'Page content') })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/home', component: page, meta: { requiresAuth: false } },
      { path: '/dashboard', component: page, meta: { requiresAuth: true, shell: 'user' } },
      { path: '/vendor', component: page, meta: { requiresAuth: true } },
    ],
  })
  await router.push(path)
  await router.isReady()
  wrapper = mount(App, { global: { plugins: [router] } })
  await flushPromises()
  return { wrapper, router }
}

beforeEach(() => {
  vi.resetModules()
  vi.clearAllMocks()
  vi.stubEnv('VITE_VENDOR_PORTAL', 'false')
  Object.assign(auth, { isAuthenticated: false, identityScopeKey: 'anonymous', isAdmin: false, isSuperAdmin: false })
  announcements.currentPopup = null
  compliance.shouldShow = false
  harness.fetchCompliance.mockResolvedValue(undefined)
  harness.loadPopup.mockResolvedValue(panel('announcement'))
  harness.loadCompliance.mockResolvedValue(panel('compliance'))
  harness.loadLayout.mockResolvedValue({
    default: defineComponent({ setup: (_, { slots }) => () => h('main', { 'data-test': 'shell' }, slots.default?.()) }),
  })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.unstubAllEnvs()
})

describe('application lazy surfaces', () => {
  it('renders a public page without loading the authenticated shell or private dialogs', async () => {
    const { wrapper } = await mountApp()
    expect(wrapper.get('[data-test="page"]').text()).toBe('Page content')
    expect(harness.loadLayout).not.toHaveBeenCalled()
    expect(harness.loadPopup).not.toHaveBeenCalled()
    expect(harness.loadCompliance).not.toHaveBeenCalled()
  })

  it('loads the shell when navigation enters a shell route and keeps the route content', async () => {
    auth.isAuthenticated = true
    auth.identityScopeKey = 'user-a'
    const { wrapper, router } = await mountApp()
    expect(harness.loadLayout).not.toHaveBeenCalled()
    await router.push('/dashboard')
    await flushPromises()
    expect(harness.loadLayout).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-test="shell"] [data-test="page"]').text()).toBe('Page content')
    await router.push('/home')
    await flushPromises()
    expect(wrapper.find('[data-test="shell"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="page"]').text()).toBe('Page content')
  })

  it('loads an announcement only when the authenticated public page receives a popup', async () => {
    auth.isAuthenticated = true
    auth.identityScopeKey = 'user-a'
    const { wrapper } = await mountApp()
    expect(harness.loadPopup).not.toHaveBeenCalled()
    announcements.currentPopup = { id: 8 }
    await flushPromises()
    expect(harness.loadPopup).toHaveBeenCalledOnce()
    expect(wrapper.find('[data-test="announcement"]').exists()).toBe(true)
    announcements.currentPopup = null
    await nextTick()
    expect(wrapper.find('[data-test="announcement"]').exists()).toBe(false)
    expect(harness.loadLayout).not.toHaveBeenCalled()
  })

  it('ignores private popup state while anonymous and compliance state for a non-admin', async () => {
    const { wrapper } = await mountApp()
    announcements.currentPopup = { id: 8 }
    compliance.shouldShow = true
    await flushPromises()
    expect(harness.loadPopup).not.toHaveBeenCalled()
    expect(harness.loadCompliance).not.toHaveBeenCalled()
    announcements.currentPopup = null
    auth.isAuthenticated = true
    auth.identityScopeKey = 'user-a'
    compliance.shouldShow = true
    await flushPromises()
    expect(harness.loadCompliance).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="compliance"]').exists()).toBe(false)
  })

  it('loads mandatory compliance after an administrator status response requires it', async () => {
    Object.assign(auth, { isAuthenticated: true, identityScopeKey: 'admin-a', isAdmin: true })
    const status = deferred<void>()
    harness.fetchCompliance.mockImplementation(async () => { await status.promise; compliance.shouldShow = true })
    const { wrapper } = await mountApp()
    expect(harness.fetchCompliance).toHaveBeenCalledOnce()
    expect(harness.loadCompliance).not.toHaveBeenCalled()
    status.resolve(undefined)
    await flushPromises()
    expect(harness.loadCompliance).toHaveBeenCalledOnce()
    expect(wrapper.find('[data-test="compliance"]').exists()).toBe(true)
    compliance.shouldShow = false
    await nextTick()
    expect(wrapper.find('[data-test="compliance"]').exists()).toBe(false)
  })

  it('does not mount late private imports after logout', async () => {
    Object.assign(auth, { isAuthenticated: true, identityScopeKey: 'admin-a', isAdmin: true })
    const popup = deferred<PanelModule>()
    const dialog = deferred<PanelModule>()
    harness.loadPopup.mockReturnValue(popup.promise)
    harness.loadCompliance.mockReturnValue(dialog.promise)
    const { wrapper } = await mountApp()
    announcements.currentPopup = { id: 8 }
    compliance.shouldShow = true
    await nextTick()
    expect(harness.loadPopup).toHaveBeenCalledOnce()
    expect(harness.loadCompliance).toHaveBeenCalledOnce()
    auth.isAuthenticated = false
    auth.identityScopeKey = 'anonymous'
    await nextTick()
    popup.resolve(panel('announcement'))
    dialog.resolve(panel('compliance'))
    await flushPromises()
    expect(wrapper.find('[data-test="announcement"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="compliance"]').exists()).toBe(false)
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('keeps supplier announcements unloaded and permits compliance only for a super administrator', async () => {
    Object.assign(auth, { isAuthenticated: true, identityScopeKey: 'supplier-a', isAdmin: true })
    const { wrapper } = await mountApp('/vendor')
    announcements.currentPopup = { id: 8 }
    compliance.shouldShow = true
    await flushPromises()
    expect(harness.loadPopup).not.toHaveBeenCalled()
    expect(harness.loadCompliance).not.toHaveBeenCalled()
    auth.isSuperAdmin = true
    await flushPromises()
    expect(harness.loadCompliance).toHaveBeenCalledOnce()
    expect(wrapper.find('[data-test="compliance"]').exists()).toBe(true)
    expect(harness.loadPopup).not.toHaveBeenCalled()
  })
})
