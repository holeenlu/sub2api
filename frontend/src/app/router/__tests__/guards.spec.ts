import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Router } from 'vue-router'

const state = vi.hoisted(() => ({
  auth: {
    isAuthenticated: false,
    isAdmin: false,
    isSimpleMode: false,
    hasPendingAuthSession: false,
    sessionEpoch: 1,
    identityScopeKey: 'fixture-session',
    adminLandingPath: '/dashboard',
    user: null as { id: number; role: 'user' | 'admin'; policy_version: number } | null,
    checkAuth: vi.fn(),
    refreshUser: vi.fn(),
    canAccessAdminPage: vi.fn<(path: string) => boolean>()
  },
  app: {
    siteName: 'sub2api',
    backendModeEnabled: false,
    publicSettingsLoaded: true,
    cachedPublicSettings: {
      vendor_portal_origin: '',
      vendor_console_enabled: false,
      subscription_enabled: true,
      payment_enabled: true
    },
    fetchPublicSettings: vi.fn()
  },
  setupStatus: vi.fn(),
  supply: { refresh: vi.fn(), clear: vi.fn() },
  compliance: { initialized: true, fetchStatus: vi.fn(), requireAcknowledgement: vi.fn() },
  loading: { startNavigation: vi.fn(), endNavigation: vi.fn() }
}))

// Use the real route registration, metadata, guards and redirect pipeline.
// Only browser history, store/API I/O and destination page rendering are isolated.
vi.mock('vue-router', async original => {
  const actual = await original<typeof import('vue-router')>()
  return { ...actual, createWebHistory: () => actual.createMemoryHistory() }
})
vi.mock('@/features/identity', () => ({
  useAuthStore: () => state.auth,
  useAdminComplianceStore: () => state.compliance
}))
vi.mock('@/features/supply', async () => ({
  ...(await import('@/features/supply/helpers')),
  useSupplyStore: () => state.supply
}))
vi.mock('@/app/model/app', () => ({ useAppStore: () => state.app }))
vi.mock('@/app/model/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))
vi.mock('@/features/system/api', () => ({ getSetupStatus: state.setupStatus }))
vi.mock('@/app/router/loading', () => ({ useNavigationLoadingState: () => state.loading }))

let router: Router

function signIn(role: 'user' | 'admin'): void {
  state.auth.isAuthenticated = true
  state.auth.isAdmin = role === 'admin'
  state.auth.user = { id: 9, role, policy_version: 1 }
  state.auth.adminLandingPath = role === 'admin' ? '/admin/dashboard' : '/dashboard'
}

async function navigate(path: string): Promise<void> {
  const resolved = router.resolve(path)
  expect(resolved.matched.length).toBeGreaterThan(0)
  expect(resolved.name).not.toBe('NotFound')
  await router.push(path)
}

beforeEach(async () => {
  vi.resetModules()
  vi.resetAllMocks()
  vi.stubEnv('VITE_VENDOR_PORTAL', 'false')
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  Object.assign(state.auth, {
    isAuthenticated: false, isAdmin: false, isSimpleMode: false, hasPendingAuthSession: false,
    sessionEpoch: 1, identityScopeKey: 'fixture-session', adminLandingPath: '/dashboard', user: null
  })
  state.auth.canAccessAdminPage.mockReturnValue(true)
  state.auth.refreshUser.mockResolvedValue(undefined)
  state.app.backendModeEnabled = false
  state.app.publicSettingsLoaded = true
  state.app.cachedPublicSettings = {
    vendor_portal_origin: '', vendor_console_enabled: false, subscription_enabled: true, payment_enabled: true
  }
  state.app.fetchPublicSettings.mockResolvedValue(state.app.cachedPublicSettings)
  state.compliance.initialized = true
  state.setupStatus.mockResolvedValue({ needs_setup: true })
  const { i18n } = await import('@/i18n')
  const { loadApplicationLocale } = await import('@/app/i18n')
  i18n.global.setLocaleMessage('en', await loadApplicationLocale('en'))
  i18n.global.locale.value = 'en'
  router = (await import('../index')).default
  for (const record of router.getRoutes()) {
    if (record.components) record.components.default = { render: () => null }
  }
})

afterEach(() => {
  router?.options.history.destroy()
  vi.restoreAllMocks()
  vi.unstubAllEnvs()
})

describe('生产路由守卫：身份', () => {
  it.each(['/dashboard', '/admin/dashboard'])('未认证访问 %s 跳转登录并保留原目的地', async path => {
    await navigate(path)
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe(path)
  })

  it('登录返回地址保留真实 query 和 fragment', async () => {
    const destination = '/subscriptions?tab=active#details'
    await navigate(destination)
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe(destination)
  })

  it.each(['/login', '/home'])('未认证访问公开页 %s 允许通过', async path => {
    await navigate(path)
    expect(router.currentRoute.value.path).toBe(path)
    expect(router.currentRoute.value.meta.requiresAuth).toBe(false)
  })

  it.each(['/login', '/register'])('已认证普通用户访问 %s 返回用户仪表盘', async path => {
    signIn('user')
    await navigate(path)
    expect(router.currentRoute.value.path).toBe('/dashboard')
  })

  it('已认证普通用户可以访问用户仪表盘', async () => {
    signIn('user')
    await navigate('/dashboard')
    expect(router.currentRoute.value.path).toBe('/dashboard')
  })

  it.each(['/admin/dashboard', '/admin/users'])('普通用户不能进入 %s', async path => {
    signIn('user')
    expect(router.resolve(path).meta.requiresAdmin).toBe(true)
    await navigate(path)
    expect(router.currentRoute.value.path).toBe('/dashboard')
  })

  it.each(['/login', '/register'])('管理员访问 %s 返回获准管理落点', async path => {
    signIn('admin')
    await navigate(path)
    expect(router.currentRoute.value.path).toBe('/admin/dashboard')
  })

  it.each(['/admin/dashboard', '/dashboard'])('管理员可以访问 %s', async path => {
    signIn('admin')
    await navigate(path)
    expect(router.currentRoute.value.path).toBe(path)
  })

  it('首次导航恢复身份，后续导航不重复初始化认证', async () => {
    state.auth.checkAuth.mockImplementationOnce(() => signIn('user'))
    await navigate('/dashboard')
    expect(router.currentRoute.value.path).toBe('/dashboard')
    await navigate('/keys')
    expect(router.currentRoute.value.path).toBe('/keys')
    expect(state.auth.checkAuth).toHaveBeenCalledTimes(1)
    expect(state.loading.startNavigation).toHaveBeenCalledTimes(2)
    expect(state.loading.endNavigation).toHaveBeenCalledTimes(2)
  })
})

describe('生产路由守卫：简易模式', () => {
  it.each([
    ['user', '/subscriptions', '/dashboard'],
    ['admin', '/subscriptions', '/admin/dashboard'],
    ['admin', '/admin/subscriptions', '/admin/dashboard']
  ] as const)('%s 在简易模式访问实际订阅入口 %s 返回 %s', async (role, path, destination) => {
    signIn(role)
    state.auth.isSimpleMode = true
    await navigate(path)
    expect(router.currentRoute.value.path).toBe(destination)
  })

  it.each([
    ['user', '/subscriptions'],
    ['admin', '/admin/subscriptions']
  ] as const)('%s 在标准模式仍可访问 %s', async (role, path) => {
    signIn(role)
    await navigate(path)
    expect(router.currentRoute.value.path).toBe(path)
  })

  it('简易模式仍允许管理员管理分组', async () => {
    signIn('admin')
    state.auth.isSimpleMode = true
    await navigate('/admin/groups')
    expect(router.currentRoute.value.path).toBe('/admin/groups')
  })

  it.each(['/dashboard', '/keys'])('简易模式仍允许普通用户进入 %s', async path => {
    signIn('user')
    state.auth.isSimpleMode = true
    await navigate(path)
    expect(router.currentRoute.value.path).toBe(path)
  })
})

describe('生产路由守卫：backend mode', () => {
  beforeEach(() => { state.app.backendModeEnabled = true })

  it('匿名访问首页转到登录', async () => {
    await navigate('/home')
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it.each(['/login', '/key-usage', '/setup', '/payment/result'])('匿名可以访问 %s', async path => {
    await navigate(path)
    expect(router.currentRoute.value.path).toBe(path)
  })

  it('管理员可正常进入管理仪表盘', async () => {
    signIn('admin')
    await navigate('/admin/dashboard')
    expect(router.currentRoute.value.path).toBe('/admin/dashboard')
  })

  it('管理员访问登录页转到管理仪表盘', async () => {
    signIn('admin')
    await navigate('/login')
    expect(router.currentRoute.value.path).toBe('/admin/dashboard')
  })

  it('已登录非管理员从受保护页返回登录，且没有重定向循环', async () => {
    signIn('user')
    await navigate('/dashboard')
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it.each(['/login', '/key-usage'])('已登录非管理员仍可停留于 %s', async path => {
    signIn('user')
    await navigate(path)
    expect(router.currentRoute.value.path).toBe(path)
  })

  it.each(['/auth/wechat/callback', '/auth/wechat/payment/callback'])('匿名允许实际回调 %s', async path => {
    await navigate(path)
    expect(router.currentRoute.value.path).toBe(path)
  })

  it.each(['/register', '/email-verify'])('存在 pending auth 时允许 %s', async path => {
    state.auth.hasPendingAuthSession = true
    await navigate(path)
    expect(router.currentRoute.value.path).toBe(path)
  })

  it.each(['/register', '/email-verify'])('没有 pending auth 时阻止 %s', async path => {
    await navigate(path)
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('初始化已完成时匿名进入 setup 返回登录', async () => {
    state.setupStatus.mockResolvedValue({ needs_setup: false })
    await navigate('/setup')
    expect(state.setupStatus).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('初始化已完成时管理员进入 setup 返回管理仪表盘', async () => {
    signIn('admin')
    state.setupStatus.mockResolvedValue({ needs_setup: false })
    await navigate('/setup')
    expect(state.setupStatus).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.path).toBe('/admin/dashboard')
  })

  it('setup 状态请求失败仍可到达实际 setup 页面', async () => {
    state.setupStatus.mockRejectedValue(new Error('setup status unavailable'))
    await navigate('/setup')
    expect(state.setupStatus).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.path).toBe('/setup')
  })
})
