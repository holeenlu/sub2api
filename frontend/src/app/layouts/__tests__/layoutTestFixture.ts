import { computed, reactive, ref } from 'vue'
import { createMemoryHistory, createRouter, RouterView, type RouteRecordRaw } from 'vue-router'
import { vi } from 'vitest'
import { uiFeedbackKey, type UiFeedback } from '@/shared/ui/feedback'
import { siteActorKey, siteSettingsKey, type SiteActor, type SiteSettings, type SitePublicSettings } from '@/features/site/ports'
import { publicKeyUsageServicesKey, type PublicKeyUsageServices } from '@/features/usage/publicKeyServices'
import type { CustomMenuItem, PublicSettings } from '@/features/site/api/publicSettingsTypes'

function unexpected(action: string): never { throw new Error('Unexpected layout fixture action: ' + action) }
export function createLayoutFixture() {
  const deniedPages = reactive(new Set<string>())
  const app = reactive({
    sidebarCollapsed: false, mobileOpen: false, sidebarScrollTop: 0,
    siteName: 'Fixture site', siteLogo: '/logo.svg', siteVersion: '1.2.3',
    publicSettingsLoaded: true, backendModeEnabled: false, docUrl: '', contactInfo: '',
    currentVersion: '1.2.3', upstreamVersion: '', buildCommit: '', versionLoading: false,
    cachedPublicSettings: { subscription_enabled: true, payment_enabled: true, payment_balance_disabled: false,
      channel_monitor_enabled: true, available_channels_enabled: true, risk_control_enabled: true, custom_menu_items: [] } as Partial<PublicSettings>,
    fetchVersion: vi.fn().mockResolvedValue({ version: '1.2.3' }),
    toggleSidebar: vi.fn(() => { app.sidebarCollapsed = !app.sidebarCollapsed }),
    setMobileOpen: vi.fn((open: boolean) => { app.mobileOpen = open }),
    fetchPublicSettings: vi.fn(() => unexpected('app settings load')),
  })
  const auth = reactive({ isAdmin: true, isSuperAdmin: false, isSimpleMode: false, isAuthenticated: true,
    adminLandingPath: '/admin/dashboard', user: null,
    canAccessAdminPage: (path: string) => !deniedPages.has(path),
    logout: vi.fn(() => unexpected('logout')),
  })
  const admin = reactive({ customMenuItems: [] as CustomMenuItem[], opsMonitoringEnabled: true, paymentEnabled: true, fetch: vi.fn().mockResolvedValue(undefined) })
  const onboarding = { isCurrentStep: () => false, isCompleted: true, nextStep: vi.fn(() => unexpected('onboarding advance')) }
  const batchImage = { canUseBatchImage: ref(false), refreshBatchImageAccess: vi.fn().mockResolvedValue(undefined) }
  const feedback: UiFeedback = { success: () => unexpected('success toast'), error: () => unexpected('error toast') }
  return { app, auth, admin, onboarding, batchImage, deniedPages, provide: { [uiFeedbackKey as symbol]: feedback } }
}

export async function layoutRouter(path = '/dashboard') {
  // Read the real assembled contribution metadata, while keeping page loaders
  // out of layout tests. No independent menu/path declaration lives here.
  const { default: applicationRouter } = await import('../../router')
  const withoutPage = (record: RouteRecordRaw): RouteRecordRaw => {
    if (record.redirect) return record
    return {
      path: record.path, name: record.name, meta: record.meta,
      ...(record.alias !== undefined ? { alias: record.alias } : {}),
      component: RouterView,
      children: record.children?.map(withoutPage),
    }
  }
  const router = createRouter({ history: createMemoryHistory(), routes: applicationRouter.options.routes.map(withoutPage) })
  await router.push(path)
  await router.isReady()
  return router
}

export function createBrandPorts(logo: string, docUrl = '') {
  const current = ref<SitePublicSettings>({
    site_name: 'Fixture site', site_logo: logo, doc_url: docUrl, compact_home_enabled: false,
    site_subtitle: '', api_base_url: '', contact_info: '', home_content: '',
    model_plaza_enabled: false, model_plaza_require_auth: false,
  })
  const settings: SiteSettings = { get current() { return current.value }, loaded: true, load: async () => unexpected('site settings load') }
  const actor: SiteActor = { isAuthenticated: false, adminLandingPath: '/dashboard', user: null }
  const usage: PublicKeyUsageServices = {
    appearance: computed(() => ({ siteName: 'Fixture site', siteLogo: current.value.site_logo || '', docUrl: current.value.doc_url || '', subscriptionEnabled: true })),
    ensureSettings: vi.fn().mockResolvedValue(undefined),
    notifyInfo: () => unexpected('public key info'), notifySuccess: () => unexpected('public key success'), notifyError: () => unexpected('public key error'),
  }
  return { current, usage, provide: { [siteSettingsKey as symbol]: settings, [siteActorKey as symbol]: actor, [publicKeyUsageServicesKey as symbol]: usage } }
}
