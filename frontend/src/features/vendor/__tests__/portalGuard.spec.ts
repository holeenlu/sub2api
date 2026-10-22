import {
  beforeAll,
  beforeEach,
  afterEach,
  describe,
  it,
  expect,
  vi,
} from 'vitest'
const harness = vi.hoisted(() => ({
  guard: null as null | ((to: any, from: any, next: any) => Promise<void>),
  routes: [] as Array<{ path: string; redirect?: string; component?: unknown }>,
}))
const auth = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  refreshUser: vi.fn(),
  isAuthenticated: true,
  isAdmin: false,
  isSuperAdmin: false,
  user: { role: 'vendor_level1' },
  isSimpleMode: false,
  hasPendingAuthSession: false,
  adminLandingPath: '/dashboard',
  canAccessAdminPage: () => true,
}))
const app = vi.hoisted(() => ({
  siteName: 'Test',
  publicSettingsLoaded: true,
  backendModeEnabled: false,
  fetchPublicSettings: vi.fn(),
  cachedPublicSettings: {
    vendor_console_enabled: true,
    vendor_portal_origin: '',
    custom_menu_items: [],
  },
}))
const vendor = vi.hoisted(() => ({ refresh: vi.fn(), clear: vi.fn() }))
vi.mock('vue-router', () => ({
  createWebHistory: () => ({}),
  createRouter: (options: { routes: typeof harness.routes }) => {
    harness.routes = options.routes
    return {
      beforeEach: (guard: any) => (harness.guard = guard),
      afterEach: vi.fn(),
      onError: vi.fn(),
    }
  },
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
vi.mock('@/features/vendor/store', () => ({ useVendorStore: () => vendor }))
vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))
vi.mock('@/stores/adminCompliance', () => ({
  useAdminComplianceStore: () => ({ initialized: true }),
}))
vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
  }),
}))
vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))
vi.mock('@/router/title', () => ({
  applyRouteMetaDescription: vi.fn(),
  resolveRouteDocumentTitle: () => '',
}))
async function navigate(path: string, meta: Record<string, unknown> = {}) {
  const next = vi.fn()
  await harness.guard!(
    { path, fullPath: path, query: {}, params: {}, meta },
    {},
    next,
  )
  return next
}
beforeAll(async () => {
  await import('@/router')
})
beforeEach(() => {
  vi.clearAllMocks()
  app.cachedPublicSettings.vendor_portal_origin = window.location.origin
  app.cachedPublicSettings.vendor_console_enabled = true
  auth.isAuthenticated = true
  auth.isAdmin = false
  auth.isSuperAdmin = false
  auth.user.role = 'vendor_level1'
  auth.refreshUser.mockResolvedValue({ role: 'vendor_level1' })
  vendor.refresh.mockResolvedValue({ id: 8 })
})
afterEach(() => vi.unstubAllEnvs())
describe('supplier route guard', () => {
  it('loads dedicated supplier identity after fresh authentication', async () => {
    const next = await navigate('/vendor/accounts')
    expect(auth.refreshUser).toHaveBeenCalled()
    expect(vendor.refresh).toHaveBeenCalled()
    expect(next).toHaveBeenCalledWith()
    expect(auth.user.role).toBe('vendor_level1')
  })
  it('allows a superadministrator into the unified platform without a supplier identity', async () => {
    auth.user.role = 'super_admin'
    auth.isSuperAdmin = true
    expect(await navigate('/vendor/platform')).toHaveBeenCalledWith()
    expect(vendor.refresh).not.toHaveBeenCalled()
    expect(vendor.clear).toHaveBeenCalled()
    expect(await navigate('/admin/vendors')).toHaveBeenCalledWith({ path: '/vendor/platform', query: {} })
  })
  it('rejects customer roles and prevents tier-2 access to child management', async () => {
    for (const role of ['user', 'admin']) {
      auth.user.role = role
      expect(await navigate('/vendor/accounts')).toHaveBeenCalledWith('/vendor-unavailable')
    }
    auth.user.role = 'vendor_level2'
    for (const path of ['/vendor/children', '/vendor/child-accounts', '/vendor/payables', '/vendor/platform']) {
      expect(await navigate(path)).toHaveBeenCalledWith('/vendor/accounts')
    }
  })
  it('does not expose registration or admin routes on the supplier origin', async () => {
    for (const path of ['/register', '/admin/vendors', '/dashboard']) {
      const next = await navigate(path)
      expect(next).toHaveBeenCalledWith('/vendor/accounts')
    }
  })
  it('gates the configured origin when disabled', async () => {
    app.cachedPublicSettings.vendor_console_enabled = false
    const next = await navigate('/login', { requiresAuth: false })
    expect(next).toHaveBeenCalledWith('/vendor-unavailable')
    expect(vendor.refresh).not.toHaveBeenCalled()
  })
  it('rejects a vendor path on the ordinary origin', async () => {
    app.cachedPublicSettings.vendor_portal_origin = 'https://different.example'
    expect(await navigate('/vendor/accounts')).toHaveBeenCalledWith(
      '/vendor-unavailable',
    )
  })
  it('fails closed for a supplier build with an origin mismatch', async () => {
    vi.stubEnv('VITE_VENDOR_PORTAL', 'true')
    app.cachedPublicSettings.vendor_portal_origin = 'https://different.example'
    expect(await navigate('/')).toHaveBeenCalledWith('/vendor-unavailable')
  })
  it('uses native login for unauthenticated suppliers and denies revoked capability', async () => {
    auth.isAuthenticated = false
    expect(await navigate('/vendor/usage')).toHaveBeenCalledWith({
      path: '/login',
      query: { redirect: '/vendor/usage' },
    })
    auth.isAuthenticated = true
    vendor.refresh.mockRejectedValueOnce({ status: 403 })
    expect(await navigate('/vendor/usage')).toHaveBeenCalledWith(
      '/vendor-unavailable',
    )
  })
  it('retires the customer-admin supplier page while retaining old bookmarks', () => {
    const legacy = harness.routes.find(route => route.path === '/admin/vendors')
    expect(legacy?.redirect).toBe('/admin/dashboard')
    expect(legacy?.component).toBeUndefined()
  })
  it('keeps the dedicated supplier workspace available after a legacy redirect', async () => {
    auth.user.role = 'super_admin'
    auth.isSuperAdmin = true
    expect(await navigate('/admin/dashboard')).toHaveBeenCalledWith({ path: '/vendor/platform', query: {} })
  })
})
