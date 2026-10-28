import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Router } from 'vue-router'

const state = vi.hoisted(() => ({
  auth: {
    loggedIn: true,
    sessionEpoch: 1,
    user: { id: 7, role: 'vendor_level1', policy_version: 1 },
    get isAuthenticated() { return this.loggedIn },
    get identityScopeKey() { return JSON.stringify([this.sessionEpoch, this.user.id, this.user.role, this.user.policy_version]) },
    get isAdmin() { return ['admin', 'super_admin'].includes(this.user.role) },
    get adminLandingPath() { return this.isAdmin ? '/admin/dashboard' : '/dashboard' },
    isSimpleMode: false, hasPendingAuthSession: false,
    checkAuth: vi.fn(), refreshUser: vi.fn(), canAccessAdminPage: () => true
  },
  settings: {
    siteName: 'sub2api', backendModeEnabled: false, publicSettingsLoaded: true,
    cachedPublicSettings: { vendor_portal_origin: '', vendor_console_enabled: true },
    fetchPublicSettings: vi.fn()
  },
  supply: { refresh: vi.fn(), clear: vi.fn() }
}))

// Execute the production route table, installed guards and redirect pipeline.
// Only history, store I/O and page rendering are isolated from external services.
vi.mock('vue-router', async original => {
  const actual = await original<typeof import('vue-router')>()
  return { ...actual, createWebHistory: () => actual.createMemoryHistory() }
})
vi.mock('@/features/identity', () => ({
  useAuthStore: () => state.auth,
  useAdminComplianceStore: () => ({ initialized: true, fetchStatus: vi.fn(), requireAcknowledgement: vi.fn() })
}))
vi.mock('@/features/supply', async () => ({
  ...(await import('@/features/supply/helpers')), useSupplyStore: () => state.supply
}))
vi.mock('@/app/model/app', () => ({ useAppStore: () => state.settings }))
vi.mock('@/app/model/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))
vi.mock('@/app/router/loading', () => ({ useNavigationLoadingState: () => ({ startNavigation: vi.fn(), endNavigation: vi.fn() }) }))

let router: Router
beforeEach(async () => {
  vi.resetModules()
  vi.stubEnv('VITE_VENDOR_PORTAL', 'false')
  state.auth.loggedIn = true; state.auth.sessionEpoch = 1
  state.auth.user = { id: 7, role: 'vendor_level1', policy_version: 1 }
  state.auth.checkAuth.mockReset()
  state.auth.refreshUser.mockReset().mockResolvedValue(undefined)
  state.supply.refresh.mockReset().mockResolvedValue({ id: 3 })
  state.supply.clear.mockReset()
  state.settings.publicSettingsLoaded = true
  state.settings.cachedPublicSettings = { vendor_portal_origin: window.location.origin, vendor_console_enabled: true }
  state.settings.fetchPublicSettings.mockReset().mockResolvedValue(state.settings.cachedPublicSettings)
  router = (await import('../index')).default
  for (const record of router.getRoutes()) {
    if (record.components) record.components.default = { render: () => null }
  }
})
afterEach(() => { router?.options.history.destroy(); vi.unstubAllEnvs() })

function deferred() {
  let resolve!: () => void
  const promise = new Promise<void>(done => { resolve = done })
  return { promise, resolve }
}

describe('production supplier navigation', () => {
  it.each([
    ['super_admin', 'platform'], ['super_admin', 'profile'],
    ...['accounts', 'proxies', 'usage', 'settlements', 'profile', 'children', 'child-accounts', 'child-proxies', 'child-usage', 'payables'].map(tab => ['vendor_level1', tab]),
    ...['accounts', 'proxies', 'usage', 'settlements', 'profile'].map(tab => ['vendor_level2', tab])
  ])('allows %s into its actual %s workspace', async (role, tab) => {
    state.auth.user.role = role
    await router.push('/vendor/' + tab)
    expect(router.currentRoute.value.path).toBe('/vendor/' + tab)
    expect(state.auth.refreshUser).toHaveBeenCalled()
    if (role === 'super_admin') {
      expect(state.supply.clear).toHaveBeenCalled()
      expect(state.supply.refresh).not.toHaveBeenCalled()
    } else expect(state.supply.refresh).toHaveBeenCalled()
  })

  it.each(['platform', 'children', 'child-accounts', 'child-proxies', 'child-usage', 'payables'])('redirects tier two away from %s without admitting its page', async tab => {
    state.auth.user.role = 'vendor_level2'
    await router.push('/vendor/' + tab)
    expect(router.currentRoute.value.path).toBe('/vendor/accounts')
  })

  it.each(['accounts', 'proxies', 'usage', 'settlements'])('routes superadministrators to platform %s', async tab => {
    state.auth.user.role = 'super_admin'
    await router.push('/vendor/' + tab)
    expect(router.currentRoute.value.fullPath).toBe('/vendor/platform?tab=' + tab)
    expect(state.supply.refresh).not.toHaveBeenCalled()
  })

  it.each(['user', 'admin'])('rejects an ordinary %s on the supplier origin', async role => {
    state.auth.user.role = role
    await router.push('/vendor/accounts')
    expect(router.currentRoute.value.path).toBe('/vendor-unavailable')
  })

  it('requires the exact origin and enabled portal even in a supplier build', async () => {
    vi.stubEnv('VITE_VENDOR_PORTAL', 'true')
    state.settings.cachedPublicSettings.vendor_portal_origin = 'https://other.invalid'
    await router.push('/vendor/accounts')
    expect(router.currentRoute.value.path).toBe('/vendor-unavailable')
    state.settings.cachedPublicSettings.vendor_portal_origin = window.location.origin
    state.settings.cachedPublicSettings.vendor_console_enabled = false
    await router.push('/login')
    expect(router.currentRoute.value.path).toBe('/vendor-unavailable')
    expect(state.auth.refreshUser).not.toHaveBeenCalled()
  })

  it('allows anonymous recovery pages but keeps a safe login return for private pages', async () => {
    state.auth.loggedIn = false
    for (const path of ['/forgot-password', '/reset-password', '/legal/admin-compliance']) {
      await router.push(path)
      expect(router.currentRoute.value.path).toBe(path)
    }
    await router.push('/vendor/usage')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/vendor/usage')
    expect(state.supply.refresh).not.toHaveBeenCalled()
  })

  it('rejects an old-session navigation after the actual identity refresh awaits', async () => {
    const pending = deferred()
    state.auth.refreshUser.mockReturnValueOnce(pending.promise)
    const navigation = router.push('/vendor/child-accounts?vendor_id=41')
    await vi.waitFor(() => expect(state.auth.refreshUser).toHaveBeenCalledOnce())
    state.auth.sessionEpoch++
    pending.resolve()
    await navigation
    expect(router.currentRoute.value.path).toBe('/vendor-unavailable')
    expect(state.supply.refresh).not.toHaveBeenCalled()
  })

  it.each(['logout', 'session', 'policy'])('rejects %s changes during the supplier scope refresh', async change => {
    const pending = deferred()
    state.supply.refresh.mockReturnValueOnce(pending.promise)
    const navigation = router.push('/vendor/payables?vendor_id=41')
    await vi.waitFor(() => expect(state.supply.refresh).toHaveBeenCalledOnce())
    if (change === 'logout') state.auth.loggedIn = false
    else if (change === 'session') state.auth.sessionEpoch++
    else state.auth.user.policy_version++
    pending.resolve()
    await navigation
    expect(router.currentRoute.value.path).toBe('/vendor-unavailable')
  })

  it('uses a freshly loaded policy in the same logical session', async () => {
    state.auth.refreshUser.mockImplementationOnce(async () => { state.auth.user.policy_version++ })
    await router.push('/vendor/accounts')
    expect(router.currentRoute.value.path).toBe('/vendor/accounts')
  })

  it('rejects a session replaced while initial public settings are loading', async () => {
    const pending = deferred()
    state.settings.publicSettingsLoaded = false
    state.settings.fetchPublicSettings.mockImplementationOnce(async () => {
      await pending.promise
      state.settings.publicSettingsLoaded = true
      return state.settings.cachedPublicSettings
    })
    const navigation = router.push('/vendor/accounts')
    await vi.waitFor(() => expect(state.settings.fetchPublicSettings).toHaveBeenCalledOnce())
    state.auth.sessionEpoch++
    pending.resolve()
    await navigation
    expect(router.currentRoute.value.path).toBe('/vendor-unavailable')
    expect(state.auth.refreshUser).not.toHaveBeenCalled()
  })
})
