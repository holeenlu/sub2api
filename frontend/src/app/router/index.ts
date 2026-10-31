import { accessPolicyRoutes } from '@/features/access-policy/routes'
import { systemRoutes } from '@/features/system/routes'
import { siteRoutes } from '@/features/site/routes'
import { jobsRoutes } from '@/features/jobs/routes'
import { operationsRoutes } from '@/features/operations/routes'
import { usageRoutes } from '@/features/usage/routes'
import { paymentsRoutes } from '@/features/payments/routes'
import { subscriptionsRoutes } from '@/features/subscriptions/routes'
import { pricingRoutes } from '@/features/pricing/routes'
import { catalogRoutes } from '@/features/catalog/routes'
import { identityRoutes } from '@/features/identity/routes'
import { keysRoutes } from '@/features/keys/routes'
/**
 * Vue Router configuration for Sub2API frontend
 * Defines all application routes with lazy loading and navigation guards
 */

import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/features/identity'
import { createVendorPortalGuard } from '@/app/compositions/vendor-portal/guard'
import { useSupplyStore } from '@/features/supply'
import { useAppStore } from '@/app/model/app'
import { useAdminSettingsStore } from '@/app/model/adminSettings'
import { useAdminComplianceStore } from '@/features/identity'
import { useNavigationLoadingState } from '@/app/router/loading'
import { getSetupStatus } from '@/features/system/api'
import { resolveCompletedSetupRedirectPath } from './setupRedirect'
import { applyRouteMetaDescription, resolveRouteDocumentTitle } from './title'

/**
 * Route definitions with lazy loading
 */
const routes: RouteRecordRaw[] = [
  ...identityRoutes,
  ...accessPolicyRoutes,
  ...keysRoutes,
  ...catalogRoutes,
  {
    path: '/admin/models',
    name: 'AdminModels',
    component: () => import('@/app/compositions/model-management/ModelManagementPage.vue'),
    meta: {
      domain: 'catalog', shell: 'admin', requiresAuth: true, requiresAdmin: true,
      title: 'Unified model management', titleKey: 'admin.models.title', descriptionKey: 'admin.models.description',
      navigation: { order: 45, icon: 'channel', labelKey: 'admin.models.title' }
    }
  },
  ...pricingRoutes,
  ...subscriptionsRoutes,
  ...paymentsRoutes,
  ...usageRoutes,
  ...operationsRoutes,
  ...jobsRoutes,
  ...siteRoutes,
  ...systemRoutes,
  { meta: { domain: 'supply' },  path: '/vendor', redirect: '/vendor/accounts' },
  { path: '/vendor/:tab(accounts|proxies|usage|settlements|profile|platform|children|child-accounts|child-proxies|child-usage|payables)', name: 'VendorPortal', component: () => import('@/app/compositions/vendor-portal/VendorPortalPage.vue'), meta: { domain: 'supply',  requiresAuth: true, titleKey: 'vendor.portal' } },
  { meta: { domain: 'supply' },  path: '/vendor/security', redirect: '/vendor/profile' },
  { meta: { domain: 'supply' },  path: '/vendor/audit', redirect: '/vendor/accounts' },
  { path: '/vendor-unavailable', name: 'VendorUnavailable', component: () => import('@/app/compositions/vendor-portal/VendorUnavailablePage.vue'), meta: { domain: 'supply',  requiresAuth: false, titleKey: 'vendor.portal' } },
  { meta: { domain: 'app' },  path: '/admin/vendors', redirect: '/admin/dashboard' },
  { meta: { domain: 'app' }, 
    path: '/',
    redirect: '/home'
  },
  {
    path: '/dashboard',
    name: 'Dashboard',
    component: () => import('@/app/compositions/dashboard/pages/UserDashboardPage.vue'),
    meta: { domain: 'app', navigation: { order: 10, icon: 'dashboard', labelKey: 'nav.dashboard', personal: false },  shell: 'user',
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Dashboard',
      titleKey: 'dashboard.title',
      descriptionKey: 'dashboard.welcomeMessage'
    }
  },
  {
    path: '/custom/:id',
    name: 'CustomPage',
    component: () => import('@/app/compositions/custom-page/CustomPage.vue'),
    meta: { domain: 'site', navigationSource: 'custom',  shell: 'user',
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Custom Page',
      titleKey: 'customPage.title',
    }
  },
  { meta: { domain: 'app' }, 
    path: '/admin',
    redirect: '/admin/dashboard'
  },
  {
    path: '/admin/dashboard',
    name: 'AdminDashboard',
    component: () => import('@/app/compositions/dashboard/pages/AdminDashboardPage.vue'),
    meta: { domain: 'app', navigation: { order: 10, icon: 'dashboard', labelKey: 'nav.dashboard' },  shell: 'admin',
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Admin Dashboard',
      titleKey: 'admin.dashboard.title',
      descriptionKey: 'admin.dashboard.description'
    }
  },
  {
    path: '/admin/groups',
    name: 'AdminGroups',
    component: () => import('@/app/compositions/group-editor/GroupsPage.vue'),
    meta: { domain: 'access-policy', navigation: { order: 40, icon: 'folder', labelKey: 'nav.groups', domId: 'sidebar-group-manage' },  shell: 'admin',
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Group Management',
      titleKey: 'admin.groups.title',
      descriptionKey: 'admin.groups.description'
    }
  },
  {
    path: '/admin/accounts',
    name: 'AdminAccounts',
    component: () => import('@/app/compositions/resource-workspace/AccountsPage.vue'),
    meta: { domain: 'resources', navigation: { order: 70, icon: 'globe', labelKey: 'nav.accounts', domId: 'sidebar-channel-manage' },  shell: 'admin',
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Account Management',
      titleKey: 'admin.accounts.title',
      descriptionKey: 'admin.accounts.description'
    }
  },
  {
    path: '/admin/proxies',
    name: 'AdminProxies',
    component: () => import('@/app/compositions/resource-workspace/ProxiesPage.vue'),
    meta: { domain: 'resources', navigation: { order: 100, icon: 'server', labelKey: 'nav.proxies' },  shell: 'admin',
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Proxy Management',
      titleKey: 'admin.proxies.title',
      descriptionKey: 'admin.proxies.description'
    }
  },
  {
    path: '/admin/settings',
    name: 'AdminSettings',
    component: () => import('@/app/compositions/settings/SettingsWorkspace.vue'),
    meta: { domain: 'system', navigation: { order: 160, icon: 'cog', labelKey: 'nav.settings' },  shell: 'admin',
      requiresAuth: true,
      requiresAdmin: true,
      title: 'System Settings',
      titleKey: 'admin.settings.title',
      descriptionKey: 'admin.settings.description'
    }
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: () => import('@/app/pages/NotFoundPage.vue'),
    meta: { domain: 'app', 
      title: '404 Not Found',
      titleKey: 'common.pageNotFound'
    }
  }
]

/**
 * Create router instance
 */
const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
  scrollBehavior(_to, _from, savedPosition) {
    // Scroll to saved position when using browser back/forward
    if (savedPosition) {
      return savedPosition
    }
    // Scroll to top for new routes
    return { top: 0 }
  }
})

/**
 * Navigation guard: Authentication check
 */
let authInitialized = false

// 初始化导航加载状态
const navigationLoading = useNavigationLoadingState()
const BACKEND_MODE_ALLOWED_PATHS = ['/login', '/key-usage', '/setup', '/payment/result', '/payment/airwallex', '/legal']
const BACKEND_MODE_CALLBACK_PATHS = [
  '/auth/callback',
  '/auth/linuxdo/callback',
  '/auth/dingtalk/callback',
  '/auth/dingtalk/email-completion',
  '/auth/oidc/callback',
  '/auth/wechat/callback',
  '/auth/wechat/payment/callback',
]
const BACKEND_MODE_PENDING_AUTH_PATHS = ['/register', '/email-verify']

function isBackendModePublicRouteAllowed(path: string, hasPendingAuthSession: boolean): boolean {
  if (BACKEND_MODE_ALLOWED_PATHS.some((allowedPath) => path === allowedPath || path.startsWith(allowedPath))) {
    return true
  }

  if (BACKEND_MODE_CALLBACK_PATHS.some((callbackPath) => path === callbackPath)) {
    return true
  }

  if (hasPendingAuthSession && BACKEND_MODE_PENDING_AUTH_PATHS.some((allowedPath) => path === allowedPath)) {
    return true
  }

  return false
}

router.beforeEach(async (to, _from, next) => {
  // 开始导航加载状态
  navigationLoading.startNavigation()

  const authStore = useAuthStore()

  // Restore auth state from localStorage on first navigation (page refresh)
  if (!authInitialized) {
    authStore.checkAuth()
    authInitialized = true
  }

  // Set page title
  const appStore = useAppStore()
  const supplierDecision = await createVendorPortalGuard({
    settings: {
      loaded: () => appStore.publicSettingsLoaded,
      load: () => appStore.fetchPublicSettings(),
      read: () => appStore.cachedPublicSettings
    },
    identity: {
      authenticated: () => authStore.isAuthenticated,
      sessionKey: () => authStore.isAuthenticated ? String(authStore.sessionEpoch) : null,
      scopeKey: () => authStore.isAuthenticated ? authStore.identityScopeKey : null,
      role: () => authStore.user?.role,
      refresh: () => authStore.refreshUser()
    },
    supply: {
      refresh: () => useSupplyStore().refresh(),
      clear: () => useSupplyStore().clear()
    },
    supplierBuild: () => import.meta.env.VITE_VENDOR_PORTAL === 'true',
    origin: () => window.location.origin
  })(to)
  if (supplierDecision.handled) {
    if (supplierDecision.redirect) next(supplierDecision.redirect)
    else next()
    return
  }
  const adminSettingsStore = useAdminSettingsStore()
  const customMenuItems = [
    ...(appStore.cachedPublicSettings?.custom_menu_items ?? []),
    ...(authStore.isAdmin ? adminSettingsStore.customMenuItems : []),
  ]
  document.title = resolveRouteDocumentTitle(to, appStore.siteName, customMenuItems)
  applyRouteMetaDescription(to)

  // Check if route requires authentication
  const requiresAuth = to.meta.requiresAuth !== false // Default to true
  const requiresAdmin = to.meta.requiresAdmin === true

  if (to.path === '/setup') {
    try {
      const status = await getSetupStatus()
      if (!status.needs_setup) {
        next(resolveCompletedSetupRedirectPath(authStore.isAuthenticated, authStore.isAdmin))
        return
      }
    } catch {
      // If setup status cannot be determined, keep the setup page reachable.
    }
  }

  // If route doesn't require auth, allow access
  if (!requiresAuth) {
    // If already authenticated and trying to access login/register, redirect to appropriate dashboard
    if (authStore.isAuthenticated && (to.path === '/login' || to.path === '/register')) {
      // In backend mode, non-admin users should NOT be redirected away from login
      // (they are blocked from all protected routes, so redirecting would cause a loop)
      if (appStore.backendModeEnabled && !authStore.isAdmin) {
        next()
        return
      }
      // Admin users go to admin dashboard, regular users go to user dashboard
      next(authStore.adminLandingPath)
      return
    }
    // Model Plaza:公开路由但受「启用开关 + 可选强制登录」双重控制(后端同口径 fail-closed)
    if (to.path === '/model-plaza') {
      if (!appStore.publicSettingsLoaded) {
        try {
          await appStore.fetchPublicSettings()
        } catch (error) {
          console.warn('Failed to load public settings in route guard', error)
        }
      }
      const plazaSettings = appStore.cachedPublicSettings
      // 仅在设置成功加载且明确为 false 时拦截(瞬时加载失败视为未知,由后端 404 兜底)
      if (appStore.publicSettingsLoaded && plazaSettings?.model_plaza_enabled === false) {
        next(
          authStore.isAuthenticated
            ? authStore.isAdmin
              ? '/admin/dashboard'
              : '/dashboard'
            : '/home'
        )
        return
      }
      if (plazaSettings?.model_plaza_require_auth === true && !authStore.isAuthenticated) {
        next({ path: '/login', query: { redirect: to.fullPath } })
        return
      }
      // Backend mode:登录的非管理员也不可见(匿名由下方公共拦截处理,广场不在白名单)
      if (appStore.backendModeEnabled && authStore.isAuthenticated && !authStore.isAdmin) {
        next('/login')
        return
      }
    }
    // Backend mode: block public pages for unauthenticated users (except login, key-usage, setup)
    if (appStore.backendModeEnabled && !authStore.isAuthenticated) {
      const isAllowed = isBackendModePublicRouteAllowed(to.path, authStore.hasPendingAuthSession)
      if (!isAllowed) {
        next('/login')
        return
      }
    }
    next()
    return
  }

  // Route requires authentication
  if (!authStore.isAuthenticated) {
    // Not authenticated, redirect to login
    next({
      path: '/login',
      query: { redirect: to.fullPath } // Save intended destination
    })
    return
  }

  // A limited admin must load the server-owned role snapshot before navigation.
  if (requiresAdmin && authStore.user?.role === 'admin' && authStore.user.policy_version === undefined) {
    try { await authStore.refreshUser() } catch { next('/login'); return }
  }
  if (requiresAdmin && authStore.isAdmin && !authStore.canAccessAdminPage(to.path)) {
    next(authStore.adminLandingPath)
    return
  }

  const accessibleDashboard = authStore.isAdmin && authStore.canAccessAdminPage('/admin/dashboard') ? '/admin/dashboard' : '/dashboard'

  // Check admin requirement
  if (requiresAdmin && !authStore.isAdmin) {
    // User is authenticated but not admin, redirect to user dashboard
    next('/dashboard')
    return
  }

  if (requiresAdmin && authStore.isAdmin) {
    const adminComplianceStore = useAdminComplianceStore()
    if (!adminComplianceStore.initialized) {
      try {
        await adminComplianceStore.fetchStatus()
      } catch (error) {
        const err = error as { status?: number; code?: string; metadata?: Record<string, string> }
        if (err.status === 423 && err.code === 'ADMIN_COMPLIANCE_ACK_REQUIRED') {
          adminComplianceStore.requireAcknowledgement(err.metadata)
        }
      }
    }
  }


  // 公共设置可能尚未加载（App.vue 的 onMounted 异步拉取晚于首次导航，且纯静态部署
  // 无 __APP_CONFIG__ 注入）。此时 cachedPublicSettings 为空会把 payment/risk_control
  // 误判为“未启用”而错误拦截，故这里先确保设置加载完成。
  if ((to.meta.requiresPayment || to.meta.requiresRiskControl || to.meta.requiresSubscription) && !appStore.publicSettingsLoaded) {
    try {
      await appStore.fetchPublicSettings()
    } catch (error) {
      console.warn('Failed to load public settings in route guard', error)
    }
  }

  // Only an explicit value from successfully loaded settings can disable a route.
  // A transient settings failure is unknown state, not a confirmed feature toggle.
  if (
    to.meta.requiresPayment &&
    appStore.publicSettingsLoaded &&
    appStore.cachedPublicSettings?.payment_enabled === false
  ) {
    next(accessibleDashboard)
    return
  }

  if (
    to.meta.requiresRiskControl &&
    appStore.publicSettingsLoaded &&
    appStore.cachedPublicSettings?.risk_control_enabled === false
  ) {
    next(authStore.isAdmin && authStore.canAccessAdminPage('/admin/settings') ? '/admin/settings' : accessibleDashboard)
    return
  }

  // 订阅功能是 opt-out 开关：只有显式 false 才拦截「我的订阅」页直达。
  if (
    to.meta.requiresSubscription &&
    appStore.publicSettingsLoaded &&
    appStore.cachedPublicSettings?.subscription_enabled === false
  ) {
    next(accessibleDashboard)
    return
  }

  // 简易模式下限制访问某些页面
  if (authStore.isSimpleMode) {
    const restrictedPaths = [
      '/admin/subscriptions',
      '/subscriptions',
    ]

    if (restrictedPaths.some((path) => to.path.startsWith(path))) {
      // 简易模式下访问受限页面,重定向到仪表板
      next(accessibleDashboard)
      return
    }
  }

  // Backend mode: admin gets full access, non-admin blocked
  if (appStore.backendModeEnabled) {
    if (authStore.isAuthenticated && authStore.isAdmin) {
      next()
      return
    }
    const isAllowed = isBackendModePublicRouteAllowed(to.path, authStore.hasPendingAuthSession)
    if (!isAllowed) {
      next('/login')
      return
    }
  }

  // All checks passed, allow navigation
  next()
})

/**
 * Navigation guard: End loading
 */
router.afterEach(() => {
  // 结束导航加载状态
  navigationLoading.endNavigation()

})

/**
 * Navigation guard: Error handling
 * Handles dynamic import failures caused by deployment updates
 */
router.onError((error) => {
  console.error('Router error:', error)

  // Check if this is a dynamic import failure (chunk loading error)
  const isChunkLoadError =
    error.message?.includes('Failed to fetch dynamically imported module') ||
    error.message?.includes('Loading chunk') ||
    error.message?.includes('Loading CSS chunk') ||
    error.name === 'ChunkLoadError'

  if (isChunkLoadError) {
    // Avoid infinite reload loop by checking sessionStorage
    const reloadKey = 'chunk_reload_attempted'
    const lastReload = sessionStorage.getItem(reloadKey)
    const now = Date.now()

    // Allow reload if never attempted or more than 10 seconds ago
    if (!lastReload || now - parseInt(lastReload) > 10000) {
      sessionStorage.setItem(reloadKey, now.toString())
      console.warn('Chunk load error detected, reloading page to fetch latest version...')
      window.location.reload()
    } else {
      console.error('Chunk load error persists after reload. Please clear browser cache.')
    }
  }
})

export default router
