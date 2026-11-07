import { lazyPanel } from '../panelLoadingServices'
import { pricingSettingsServicesKey } from '@/features/pricing/settings'
import { SITE_BILLING_MODES, SITE_BILLING_MODE_I18N_KEYS, resolveSiteBillingMode, billingModeToSettings } from '@/app/navigation/siteBillingMode'
import { computed, type App } from 'vue'
import { useAppStore } from '@/app/model/app'
import { useAdminSettingsStore } from '@/app/model/adminSettings'
import { useClipboard } from '@/shared/lib/useClipboard'
import { useAuthStore, useStepUp, TotpStepUpDialog, isStepUpCancelled, isStepUpBlocked, stepUpBlockReason, usersAPI } from '@/features/identity'
import { listPlatformIds, listPlatforms } from '@/features/catalog'
import { groupsAPI } from '@/features/access-policy/groups'
import { accountsAPI, proxiesAPI } from '@/features/resources'
import { adminPaymentAPI } from '@/features/payments'
import { adminUsageAPI } from '@/features/usage'
import { settingsAPI } from '@/features/system/api'
import { createAuthSourceDefaultsServices } from './authSourceDefaults'

import { identitySettingsServicesKey } from '@/features/identity/settings'
import { accesspolicySettingsServicesKey } from '@/features/access-policy/settings'
import { resourcesSettingsServicesKey } from '@/features/resources/settings'
import { subscriptionsSettingsServicesKey } from '@/features/subscriptions/settings'
import { paymentsSettingsServicesKey } from '@/features/payments/settings'
import { systemSettingsServicesKey } from '@/features/system/settings'
import { settingsWorkspaceServicesKey } from './ports'

/** Editor-only ports. The app separately installs Site and System page services. */
const DelegatedSettings = lazyPanel(() => import('./DelegatedSettings.vue'))

export function installSettingsServices(app: App): void {
  const settings = useAppStore()
  const auth = useAuthStore()
  const adminSettings = useAdminSettingsStore()
  const clipboard = useClipboard()
  const notifications = { showError: settings.showError, showSuccess: settings.showSuccess, showWarning: settings.showWarning, showInfo: settings.showInfo }
  const security = { createStepUp: useStepUp, StepUpDialog: TotpStepUpDialog, isStepUpCancelled, isStepUpBlocked, stepUpBlockReason }
  const scope = computed(() => auth.isAuthenticated ? auth.identityScopeKey : null)
  app.provide(settingsWorkspaceServicesKey, {
    scope, get isSuperAdmin() { return auth.isSuperAdmin }, notifications,
    DelegatedSettings,
    async refreshSettings() {
      const captured = scope.value
      // Force alone shares in-flight reads; a saved mutation starts a new cache generation.
      settings.clearPublicSettingsCache()
      const publicSettings = await settings.fetchPublicSettings(true)
      if (!publicSettings) throw new Error('Public settings refresh failed')
      // Delegated admin projections read the public cache, so refresh them after it settles.
      if (captured === scope.value) await adminSettings.fetch(true, { throwOnError: true })
    }
  })
  app.provide(systemSettingsServicesKey, { scope, notifications, ...security, control: settingsAPI })
  app.provide(identitySettingsServicesKey, {
    authDefaults: createAuthSourceDefaultsServices(listPlatformIds),
    components: { GroupBadge, GroupOptionItem },
    copy: clipboard.copyToClipboard, groups: groupsAPI, platformIds: listPlatformIds,
    get platformOptions() { return listPlatforms().map(platform => ({ value: platform.id, label: platform.display_name })) }
  })
  app.provide(accesspolicySettingsServicesKey, {
    notifications, control: settingsAPI,
    users: { search: adminUsageAPI.searchUsers, get: id => usersAPI.getById(id, true) }
  })
  app.provide(resourcesSettingsServicesKey, { notifications, control: settingsAPI, accounts: accountsAPI, proxies: proxiesAPI })
  app.provide(subscriptionsSettingsServicesKey, { groups: groupsAPI, platformIds: listPlatformIds, components: { GroupBadge, GroupOptionItem } })
  app.provide(pricingSettingsServicesKey, {
    modes: SITE_BILLING_MODES.map(mode => ({ value: mode, labelKey: `admin.settings.features.siteBillingMode.options.${SITE_BILLING_MODE_I18N_KEYS[mode]}`, hintKey: `admin.settings.features.siteBillingMode.hints.${SITE_BILLING_MODE_I18N_KEYS[mode]}` })),
    resolve: resolveSiteBillingMode, patch: billingModeToSettings
  })
  app.provide(paymentsSettingsServicesKey, { scopeKey: scope, notifications, payment: adminPaymentAPI, get docUrl() { return settings.cachedPublicSettings?.doc_url || '' } })
}
import { GroupBadge, GroupOptionItem } from '@/features/access-policy/presentation'
