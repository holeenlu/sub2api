import { catalogNotificationsKey, catalogUserGroupRatesKey, platformLabel, GROUP_PLATFORM_OPTIONS } from '@/features/catalog'
import { userGroupsAPI } from '@/features/access-policy'
import { peakRatePresentation } from '@/features/pricing'
import { computed, readonly, type App } from 'vue'
import { useAuthStore, usersAPI } from '@/features/identity'
import { catalogScopeKey, catalogPresentationKey, listPlatformIds, supportsPricingModelSync } from '@/features/catalog'
import { pricingNotificationsKey, pricingPermissionsKey, pricingScopeKey, pricingDirectoryKey, pricingCatalogKey, pricingWebSearchKey, BILLING_MODE_TOKEN, BILLING_MODE_PER_REQUEST, BILLING_MODE_IMAGE, BILLING_MODE_VIDEO, REASONING_EFFORT_LEVELS } from '@/features/pricing'
import { subscriptionNotificationsKey, subscriptionScopeKey, subscriptionDirectoryKey, subscriptionCanManageKey, subscriptionNavigationKey, subscriptionPresentationKey } from '@/features/subscriptions'
import { accountsAPI } from '@/features/resources'
import { groupsAPI } from '@/features/access-policy'
import { adminUsageAPI } from '@/features/usage'
import { getWebSearchEmulationConfig } from '@/features/system/api'
import { useAppStore } from '@/app/model/app'
import { isVendorOrigin } from '@/features/supply'
import { i18n } from '@/i18n'
import router from '../router'

export function installCatalogServices(app: App): void {
  const auth = useAuthStore()
  const settings = useAppStore()
  const portal = computed(() => import.meta.env.VITE_VENDOR_PORTAL === 'true' || isVendorOrigin(settings.cachedPublicSettings) ? 'supplier' : 'console')
  const privateScope = computed(() => auth.isAuthenticated ? JSON.stringify([auth.identityScopeKey, portal.value]) : '')
  const notifications = { showError: settings.showError, showSuccess: settings.showSuccess }
  const navigationError = () => settings.showError(i18n.global.t('ui.unknownError'))
  app.provide(catalogNotificationsKey, { showError: settings.showError })
  app.provide(catalogUserGroupRatesKey, signal => userGroupsAPI.getUserGroupRates({ signal }))
  app.provide(catalogScopeKey, computed(() => privateScope.value || 'anonymous:' + portal.value))
  app.provide(catalogPresentationKey, computed(() => ({
    peakRate: peakRatePresentation,
    components: { GroupBadge },
    pricing: { token: BILLING_MODE_TOKEN, perRequest: BILLING_MODE_PER_REQUEST, image: BILLING_MODE_IMAGE, video: BILLING_MODE_VIDEO, reasoningEffortLevels: REASONING_EFFORT_LEVELS },
    authenticated: auth.isAuthenticated,
    embedded: router.currentRoute.value.query.embedded === '1' && auth.isAuthenticated,
    dashboardTarget: auth.adminLandingPath || '/dashboard',
    loginTarget: { path: '/login', query: { redirect: '/model-plaza' } },
    settingsLoaded: settings.publicSettingsLoaded,
    refreshSettings: () => settings.fetchPublicSettings(true),
    settings: settings.cachedPublicSettings
  })))
  app.provide(pricingNotificationsKey, notifications)
  app.provide(pricingScopeKey, privateScope)
  app.provide(pricingPermissionsKey, {
    get isSuperAdmin() { return auth.isSuperAdmin },
    can: auth.can, canEditAdminField: auth.canEditAdminField, editableAdminFields: auth.editableAdminFields
  })
  app.provide(pricingCatalogKey, { listPlatformIds, supportsPricingModelSync, platformLabel })
  app.provide(pricingDirectoryKey, {
    listGroups: signal => groupsAPI.getAll(undefined, { signal }),
    async searchAccounts(platform, keyword, signal) {
      const response = await accountsAPI.list(1, 20, { platform, search: keyword }, { signal })
      return response.items.map(({ id, name, platform }) => ({ id, name, platform }))
    },
    async getAccount(id, signal) {
      const account = await accountsAPI.getById(id, { signal })
      return { id: account.id, name: account.name, platform: account.platform }
    }
  })
  app.provide(pricingWebSearchKey, async signal => {
    const config = await getWebSearchEmulationConfig({ signal })
    return config.enabled === true && config.providers.length > 0
  })
  app.provide(subscriptionNotificationsKey, notifications)
  app.provide(subscriptionScopeKey, privateScope)
  app.provide(subscriptionCanManageKey, computed(() => auth.can('subscriptions.manage')))
  app.provide(subscriptionPresentationKey, { peakRate: peakRatePresentation, groupPlatformOptions: computed(() => readonly(GROUP_PLATFORM_OPTIONS)), serverUtcOffset: computed(() => settings.cachedPublicSettings?.server_utc_offset), platformLabel, components: { GroupBadge, GroupOptionItem } })
  app.provide(subscriptionDirectoryKey, {
    listGroups: signal => groupsAPI.getAll(undefined, { signal }),
    async searchAssignableUsers(keyword, signal) {
      return (await usersAPI.list(1, 30, { search: keyword, sort_by: 'email', sort_order: 'asc' }, { signal })).items
    },
    searchHistoryUsers: (keyword, signal) => adminUsageAPI.searchUsers(keyword, { signal })
  })
  app.provide(subscriptionNavigationKey, {
    openKeys: () => { router.push('/keys').catch(navigationError) },
    openPurchase: groupId => { router.push({ path: '/purchase', query: { tab: 'subscription', group: String(groupId) } }).catch(navigationError) },
    usageTarget: userId => ({ path: '/admin/usage', query: { user_id: String(userId) } }),
    groupsTarget: { path: '/admin/groups' }
  })
}
import { GroupBadge, GroupOptionItem } from '@/features/access-policy/presentation'
