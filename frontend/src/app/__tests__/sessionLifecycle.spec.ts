import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { nextTick, reactive, ref } from 'vue'
import App from '../App.vue'
import { authSessionStamp } from '@/shared/session'

const locale = ref('en')

const harness = vi.hoisted(() => ({
  resolveTitle: vi.fn(),
  cancelStepUp: vi.fn(),
  fetchPublicSettings: vi.fn<() => Promise<void>>(),
  getSetupStatus: vi.fn<() => Promise<{ needs_setup: boolean }>>(),
  routeHooks: new Set<() => void>(),
  removeAfterEach: vi.fn<() => void>(),
  router: {
    afterEach: vi.fn<(hook: () => void) => () => void>(),
    replace: vi.fn()
  },
  subscriptions: {
    setScope: vi.fn(),
    fetchActiveSubscriptions: vi.fn<() => Promise<unknown[]>>(),
    startPolling: vi.fn(),
    stopPolling: vi.fn(),
    clear: vi.fn()
  },
  announcements: {
    setScope: vi.fn(),    fetchAnnouncements: vi.fn<(force?: boolean) => Promise<void>>(),
    reset: vi.fn()
  },
  compliance: {
    fetchStatus: vi.fn<() => Promise<void>>(),
    requireAcknowledgement: vi.fn(),
    reset: vi.fn()
  }
}))

vi.mock('vue-router', () => ({
  RouterView: { name: 'RouterView', render: () => null },
  useRouter: () => harness.router,
  useRoute: () => route
}))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ locale })
}))
vi.mock('@/features/identity', () => ({ globalAdminStepUp: { onCancel: harness.cancelStepUp }, useAuthStore: () => auth, useAdminComplianceStore: () => harness.compliance }))
vi.mock('@/features/identity/panelLoaders', () => ({ loadAdminComplianceDialog: async () => ({ __esModule: true, default: { render: () => null } }) }))
vi.mock('../layouts/AppLayout.vue', () => ({ __esModule: true, default: { template: '<slot />' } }))
vi.mock('@/features/subscriptions', () => ({ useSubscriptionStore: () => harness.subscriptions }))
vi.mock('@/features/site/announcements', () => ({ useAnnouncementStore: () => harness.announcements }))
vi.mock('@/app/model/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [], setScope: vi.fn() }) }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => app }))
vi.mock('@/features/system/api', () => ({ getSetupStatus: () => harness.getSetupStatus() }))
vi.mock('@/app/router/title', () => ({
  applyRouteMetaDescription: vi.fn(),
  resolveRouteDocumentTitle: () => harness.resolveTitle()
}))
vi.mock('@/app/components/Toast.vue', () => ({ default: { render: () => null } }))
vi.mock('@/app/components/NavigationProgress.vue', () => ({ default: { render: () => null } }))
vi.mock('@/features/site/announcementPanels', () => ({ loadAnnouncementPopup: async () => ({ __esModule: true, default: { render: () => null } }) }))

interface AuthState {
  readonly identityScopeKey: string
  isAuthenticated: boolean
  token: string | null
  user: { id: number; role: string; policy_version: number } | null
  isAdmin: boolean
  isSuperAdmin: boolean
}

function createAppState() {
  return reactive({
    siteName: 'sub2api',
    publicSettingsLoaded: true,
    cachedPublicSettings: { subscription_enabled: true, custom_menu_items: [] },
    fetchPublicSettings: harness.fetchPublicSettings
  })
}

let auth: AuthState
let app: ReturnType<typeof createAppState>
let route: { path: string; fullPath: string; meta: Record<string, unknown> }
let wrapper: VueWrapper | undefined

function signIn(session: string, rotation = 0): void {
  const claims = { user_id: 7, role: 'user', sid: session, token_version: 0, iat: rotation }
  auth.token = 'header.' + btoa(JSON.stringify(claims)) + '.signature'
  auth.user = { id: 7, role: 'user', policy_version: 1 }
  auth.isAuthenticated = true
}

function deferredSettings(): { promise: Promise<void>; resolve: () => void } {
  let resolve = (): void => { throw new Error('Settings promise is not initialized') }
  const promise = new Promise<void>((finish) => { resolve = finish })
  return { promise, resolve }
}

async function mountApp(): Promise<void> {
  wrapper = mount(App)
  await flushPromises()
}

function unmountApp(): void {
  wrapper?.unmount()
  wrapper = undefined
}

describe('application session lifecycle', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    locale.value = 'en'
    harness.resolveTitle.mockImplementation(() => 'Session lifecycle ' + locale.value)
    vi.useFakeTimers()
    vi.stubEnv('VITE_VENDOR_PORTAL', 'false')
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    auth = reactive<AuthState>({
      get identityScopeKey() { return JSON.stringify([authSessionStamp(this.token), this.user?.id, this.user?.role, this.user?.policy_version]) },
      isAuthenticated: false,
      token: null,
      user: null,
      isAdmin: false,
      isSuperAdmin: false
    })
    app = createAppState()
    route = reactive({ path: '/dashboard', fullPath: '/dashboard', meta: {} })
    harness.routeHooks.clear()
    harness.router.afterEach.mockImplementation((hook) => {
      harness.routeHooks.add(hook)
      harness.removeAfterEach.mockImplementation(() => { harness.routeHooks.delete(hook) })
      return harness.removeAfterEach
    })
    harness.getSetupStatus.mockResolvedValue({ needs_setup: false })
    harness.fetchPublicSettings.mockResolvedValue(undefined)
    harness.subscriptions.fetchActiveSubscriptions.mockResolvedValue([])
    harness.announcements.fetchAnnouncements.mockResolvedValue(undefined)
    harness.compliance.fetchStatus.mockResolvedValue(undefined)
  })

  afterEach(() => {
    unmountApp()
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllEnvs()
  })

  it('cancels the delayed login announcement when the user signs out', async () => {
    await mountApp()
    signIn('login-a')
    await nextTick()
    expect(harness.subscriptions.startPolling).toHaveBeenCalledOnce()
    expect(harness.announcements.fetchAnnouncements).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(1)

    await vi.advanceTimersByTimeAsync(1_000)
    auth.isAuthenticated = false
    auth.token = null
    auth.user = null
    await nextTick()

    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(3_000)
    expect(harness.announcements.fetchAnnouncements).not.toHaveBeenCalled()
    expect(harness.subscriptions.stopPolling).toHaveBeenCalled()
  })

  it('preserves a rotating token session and clears private state for a new login of the same user', async () => {
    signIn('login-a')
    await mountApp()
    expect(harness.subscriptions.startPolling).toHaveBeenCalledOnce()
    expect(harness.announcements.fetchAnnouncements).toHaveBeenCalledOnce()

    signIn('login-a', 1)
    await nextTick()
    expect(harness.subscriptions.startPolling).toHaveBeenCalledOnce()
    expect(harness.subscriptions.stopPolling).not.toHaveBeenCalled()
    expect(harness.subscriptions.clear).not.toHaveBeenCalled()
    expect(harness.announcements.reset).not.toHaveBeenCalled()
    expect(harness.compliance.reset).not.toHaveBeenCalled()

    signIn('login-b', 2)
    await nextTick()
    expect(harness.subscriptions.stopPolling).toHaveBeenCalled()
    expect(harness.subscriptions.setScope).toHaveBeenLastCalledWith(auth.identityScopeKey)
    expect(harness.subscriptions.setScope).toHaveBeenCalledTimes(2)
    expect(harness.announcements.reset).toHaveBeenCalledOnce()
    expect(harness.compliance.reset).toHaveBeenCalledOnce()
    expect(harness.subscriptions.startPolling).toHaveBeenCalledTimes(2)
    expect(harness.announcements.fetchAnnouncements).toHaveBeenCalledTimes(2)
  })

  it('does not start an old session after its pending public settings request resolves', async () => {
    const oldSettings = deferredSettings()
    const currentSettings = deferredSettings()
    app.publicSettingsLoaded = false
    harness.fetchPublicSettings
      .mockReturnValueOnce(oldSettings.promise) // Application startup.
      .mockReturnValueOnce(oldSettings.promise) // First authenticated session.
      .mockReturnValueOnce(currentSettings.promise)
    await mountApp()
    signIn('login-a')
    await nextTick()
    signIn('login-b')
    await nextTick()
    expect(harness.fetchPublicSettings).toHaveBeenCalledTimes(3)

    oldSettings.resolve()
    await flushPromises()
    document.dispatchEvent(new Event('visibilitychange'))
    expect(harness.subscriptions.startPolling).not.toHaveBeenCalled()
    expect(harness.subscriptions.fetchActiveSubscriptions).not.toHaveBeenCalled()
    expect(harness.announcements.fetchAnnouncements).not.toHaveBeenCalled()

    app.publicSettingsLoaded = true
    currentSettings.resolve()
    await flushPromises()
    expect(harness.subscriptions.startPolling).toHaveBeenCalledOnce()
    expect(harness.subscriptions.fetchActiveSubscriptions).toHaveBeenCalledOnce()
    expect(harness.announcements.fetchAnnouncements).toHaveBeenCalledOnce()
  })

  it('unregisters navigation and visibility callbacks and stops polling on unmount', async () => {
    signIn('login-a')
    await mountApp()
    expect(harness.routeHooks.size).toBe(1)
    document.dispatchEvent(new Event('visibilitychange'))
    for (const hook of harness.routeHooks) hook()
    expect(harness.announcements.fetchAnnouncements).toHaveBeenCalledTimes(3)

    unmountApp()
    expect(harness.removeAfterEach).toHaveBeenCalledOnce()
    expect(harness.routeHooks.size).toBe(0)
    expect(harness.subscriptions.stopPolling).toHaveBeenCalled()
    document.dispatchEvent(new Event('visibilitychange'))
    for (const hook of harness.routeHooks) hook()
    await vi.advanceTimersByTimeAsync(3_000)
    expect(harness.announcements.fetchAnnouncements).toHaveBeenCalledTimes(3)
    expect(harness.subscriptions.fetchActiveSubscriptions).toHaveBeenCalledOnce()
  })
  it('owns title updates for committed language changes and stops them on unmount', async () => {
    await mountApp()
    expect(document.title).toBe('Session lifecycle en')
    locale.value = 'ja'
    await nextTick()
    expect(document.title).toBe('Session lifecycle ja')
    unmountApp()
    const calls = harness.resolveTitle.mock.calls.length
    locale.value = 'zh'
    await nextTick()
    expect(harness.resolveTitle).toHaveBeenCalledTimes(calls)
  })

})
