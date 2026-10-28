import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, disposePinia } from 'pinia'
import { defineComponent, h, nextTick } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { AxiosError, type AxiosAdapter, type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import App from '../App.vue'
import LinuxDoCallbackView from '@/features/identity/pages/LinuxDoCallbackView.vue'
import { identityRoutes } from '@/features/identity/routes'
import { useAuthStore } from '@/features/identity/model/auth'
import { identityAuthShellKey, identityNotificationsKey } from '@/features/identity/ports'
import { apiClient, configureHttpClient } from '@/shared/http'

const harness = vi.hoisted(() => ({
  notifications: { showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn(), showInfo: vi.fn() },
  settings: {
    siteName: 'OAuth continuation test', publicSettingsLoaded: true,
    cachedPublicSettings: { subscription_enabled: false, custom_menu_items: [] },
    fetchPublicSettings: vi.fn().mockResolvedValue(undefined)
  },
  subscriptions: { setScope: vi.fn(), clear: vi.fn(), stopPolling: vi.fn(), startPolling: vi.fn(), fetchActiveSubscriptions: vi.fn().mockResolvedValue([]) },
  announcements: { setScope: vi.fn(), reset: vi.fn(), fetchAnnouncements: vi.fn().mockResolvedValue(undefined) },
  compliance: { reset: vi.fn(), requireAcknowledgement: vi.fn(), fetchStatus: vi.fn().mockResolvedValue(undefined) }
}))

// Keep App, RouterView, the contributed callback route, callback/composable, auth
// store and auth HTTP wrappers real. Only unrelated chrome and background jobs
// are stubbed; the Axios adapter below is the only authentication test double.
vi.mock('@/features/subscriptions', () => ({ useSubscriptionStore: () => harness.subscriptions }))
vi.mock('@/features/site/announcements', () => ({ useAnnouncementStore: () => harness.announcements }))
vi.mock('@/app/model/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [], setScope: vi.fn() }) }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => harness.settings }))
vi.mock('@/features/identity', async () => {
  const { globalAdminStepUp } = await import('@/features/identity/composables/useStepUp')
  const { useAuthStore } = await import('@/features/identity/model/auth')
  return { globalAdminStepUp, useAuthStore, useAdminComplianceStore: () => harness.compliance }
})
vi.mock('@/features/identity/panelLoaders', () => ({ loadAdminComplianceDialog: async () => ({ __esModule: true, default: { render: () => null } }) }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }
})
vi.mock('@/features/system/api', () => ({ getSetupStatus: async () => ({ needs_setup: false }) }))
vi.mock('@/app/router/title', () => ({
  applyRouteMetaDescription: vi.fn(), resolveRouteDocumentTitle: () => 'OAuth continuation test'
}))
vi.mock('../layouts/AppLayout.vue', () => ({ __esModule: true, default: { template: '<main><slot /></main>' } }))
vi.mock('@/app/components/Toast.vue', () => ({ default: { render: () => null } }))
vi.mock('@/app/components/NavigationProgress.vue', () => ({ default: { render: () => null } }))
vi.mock('@/features/site/announcementPanels', () => ({ loadAnnouncementPopup: async () => ({ __esModule: true, default: { render: () => null } }) }))
vi.mock('@/features/identity/components/PendingOAuthCreateAccountForm.vue', () => ({ default: { render: () => null } }))

function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Deferred response is not initialized') }
  const promise = new Promise<T>((finish) => { resolve = finish })
  return { promise, resolve }
}

function response(config: InternalAxiosRequestConfig, data: unknown, status = 200): AxiosResponse<unknown> {
  return { config, data, status, statusText: '', headers: {} }
}

let wrapper: VueWrapper | undefined
let pinia: ReturnType<typeof createPinia> | undefined
let originalAdapter: typeof apiClient.defaults.adapter

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] })
  vi.stubEnv('VITE_VENDOR_PORTAL', 'false')
  localStorage.clear()
  sessionStorage.clear()
  window.history.replaceState({}, '', '/auth/linuxdo/callback')
  originalAdapter = apiClient.defaults.adapter
  configureHttpClient({})
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  if (pinia) disposePinia(pinia)
  pinia = undefined
  apiClient.defaults.adapter = originalAdapter
  configureHttpClient({})
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.unstubAllEnvs()
  localStorage.clear()
  sessionStorage.clear()
})

describe('OAuth continuation through the application router', () => {
  it('redeems the LinuxDo session once while /auth/me is pending, then completes the intended redirect', async () => {
    const callbackRoute = identityRoutes.find(route => route.name === 'LinuxDoOAuthCallback')
    if (!callbackRoute) throw new Error('The LinuxDo callback route is missing')
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [callbackRoute, {
        path: '/dashboard', name: 'OAuthTestDestination', meta: { requiresAuth: true },
        component: defineComponent({ render: () => h('p', { 'data-test': 'oauth-destination' }, 'Signed in') })
      }]
    })
    const me = deferred<void>()
    const token = 'header.' + btoa(JSON.stringify({ user_id: 7, role: 'user', sid: 'oauth-session', token_version: 0 })) + '.signature'
    const user = { id: 7, role: 'user', email: 'oauth@example.test', username: 'OAuth user', policy_version: 1 }
    let consumed = false
    const adapter = vi.fn<AxiosAdapter>(async config => {
      if (config.method === 'post' && config.url === '/auth/oauth/pending/exchange') {
        // Match the server's one-time pending-cookie completion: a remounted
        // callback must receive a real HTTP failure, not another valid token.
        if (consumed) {
          throw new AxiosError('pending auth session not found', 'ERR_BAD_REQUEST', config, undefined,
            response(config, { code: 'PENDING_AUTH_SESSION_NOT_FOUND', message: 'pending auth session not found' }, 404))
        }
        consumed = true
        return response(config, { code: 0, data: {
          access_token: token, refresh_token: 'oauth-refresh', expires_in: 3600,
          token_type: 'Bearer', redirect: '/dashboard?from=linuxdo'
        } })
      }
      if (config.method === 'get' && config.url === '/auth/me') {
        await me.promise
        return response(config, { code: 0, data: user })
      }
      throw new Error('Unexpected HTTP request: ' + config.method + ' ' + config.url)
    })
    apiClient.defaults.adapter = adapter
    pinia = createPinia()
    const auth = useAuthStore(pinia)
    const anonymousScope = auth.identityScopeKey
    await router.push('/auth/linuxdo/callback')
    await router.isReady()
    wrapper = mount(App, {
      global: {
        plugins: [pinia, router],
        provide: {
          [identityAuthShellKey as symbol]: defineComponent({ setup: (_, { slots }) => () => h('section', slots.default?.()) }),
          [identityNotificationsKey as symbol]: harness.notifications
        }
      }
    })
    expect(wrapper.findComponent(LinuxDoCallbackView).exists()).toBe(true)
    await flushPromises()
    await nextTick()

    // The epoch has advanced, but the identity is not complete. This was the
    // interval in which keying every RouterView remounted the public callback.
    expect(auth.identityScopeKey).not.toBe(anonymousScope)
    expect(auth.token).toBe(token)
    expect(auth.user).toBeNull()
    expect(auth.isAuthenticated).toBe(false)
    expect(router.currentRoute.value.path).toBe('/auth/linuxdo/callback')
    expect(adapter.mock.calls.map(([config]) => config.url)).toEqual(['/auth/oauth/pending/exchange', '/auth/me'])
    expect(harness.notifications.showError).not.toHaveBeenCalled()
    expect(harness.notifications.showSuccess).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('pending auth session not found')

    me.resolve(undefined)
    await flushPromises()
    await nextTick()

    expect(router.currentRoute.value.fullPath).toBe('/dashboard?from=linuxdo')
    expect(wrapper.get('[data-test="oauth-destination"]').text()).toBe('Signed in')
    expect(wrapper.findComponent(LinuxDoCallbackView).exists()).toBe(false)
    expect(auth.isAuthenticated).toBe(true)
    expect(auth.user?.id).toBe(user.id)
    expect(localStorage.getItem('auth_token')).toBe(token)
    expect(JSON.parse(localStorage.getItem('auth_user') || 'null')).toMatchObject(user)
    expect(adapter.mock.calls.map(([config]) => config.url)).toEqual(['/auth/oauth/pending/exchange', '/auth/me'])
    expect(harness.notifications.showSuccess).toHaveBeenCalledOnce()
    expect(harness.notifications.showSuccess).toHaveBeenCalledWith('auth.loginSuccess')
    expect(harness.notifications.showError).not.toHaveBeenCalled()
  })
})
