import { lazyPanel } from './panelLoadingServices'
import { loadProfilePasswordForm, loadProfileTotpCard, loadProfilePasskeyCard } from '@/features/identity/panelLoaders'
import { createIpGeoLookup, ipGeoLookupKey } from '@/shared/lib/ipGeoLookup'
import { apiClient, type HttpRequestConfig } from '@/shared/http'
import { loadEditAccountModal, loadBulkEditAccountModal } from '@/features/resources/panelLoaders'
import { installAccessPolicyServices } from './accessPolicyServices'
import { installIdentityUserServices } from './identityUserServices'
import { installResourceServices } from './resourceServices'
import { installOperationsServices } from './operationsServices'
import { installPaymentsServices } from './paymentsServices'
import { installSupplyServices, vendorPortalSecurityPanelsKey, vendorPortalResourcePanelsKey } from './vendor-portal'
import { installCatalogServices } from './catalogServices'
import { installJobsServices } from './jobsServices'
import { installSiteServices } from './siteServices'
import { installSystemServices } from './systemServices'
import { installSettingsServices } from './settings'
import { installUsageServices } from './usageServices'
import { computed, effectScope, type App } from 'vue'
import { uiFeedbackKey } from '@/shared/ui/feedback'
import {
  useAuthStore, identityNotificationsKey, identitySettingsKey,
  identityPortalKey, identityAuthShellKey, identityClipboardKey
} from '@/features/identity'
import { keysServicesKey, type KeysServices } from '@/features/keys'
import { useAppStore } from '@/app/model/app'
import { useOnboardingStore } from '@/app/onboarding/store'
import { userGroupsAPI } from '@/features/access-policy/userGroups'
import { getDashboardApiKeysUsage } from '@/features/usage'
import { buildCodexModelCatalogUrl, fetchCodexModelsManifest } from '@/features/catalog/codexManifest'
import { useClipboard } from '@/shared/lib/useClipboard'
import { isVendorOrigin, safePortalRedirect, useSupplyStore } from '@/features/supply'
import AuthLayout from '../layouts/AuthLayout.vue'

/** Composition only: each feature keeps its own state and public contract. */
const EditAccountModal = lazyPanel(loadEditAccountModal)
const BulkEditAccountModal = lazyPanel(loadBulkEditAccountModal)
const ProfilePasswordForm = lazyPanel(loadProfilePasswordForm)
const ProfileTotpCard = lazyPanel(loadProfileTotpCard)
const ProfilePasskeyCard = lazyPanel(loadProfilePasskeyCard)

export function installFeatureServices(app: App): () => void {
  const scope = effectScope()
  let disposeSupply: (() => void) | undefined
  let disposed = false
  const dispose = (): void => {
    if (disposed) return
    disposed = true
    try { scope.stop() } finally { disposeSupply?.() }
  }
  app.onUnmount(dispose)
  try {
    scope.run(() => app.runWithContext(() => {
      const settings = useAppStore()
      const auth = useAuthStore()
      const tour = useOnboardingStore()
      const supply = useSupplyStore()
      app.provide(uiFeedbackKey, { success: settings.showSuccess, error: settings.showError })
      const clipboard = useClipboard()
      const supplierPortal = computed(() =>
        import.meta.env.VITE_VENDOR_PORTAL === 'true' || isVendorOrigin(settings.cachedPublicSettings)
      )
      app.provide(ipGeoLookupKey, createIpGeoLookup({
        scope: computed(() => auth.isAuthenticated ? JSON.stringify([
          auth.identityScopeKey, supplierPortal.value ? 'supplier' : 'console',
          supply.vendor?.id, supply.vendor?.policy_version, supply.vendor?.contract_version
        ]) : null),
        async requestJson(url, signal) {
          const config: HttpRequestConfig = { sessionPolicy: 'anonymous', signal }
          return (await apiClient.get<unknown>(url, config)).data
        }
      }))
      app.provide(identityNotificationsKey, {
        showError: settings.showError, showSuccess: settings.showSuccess,
        showWarning: settings.showWarning, showInfo: settings.showInfo
      })
      app.provide(identitySettingsKey, {
        get current() { return settings.cachedPublicSettings },
        get loaded() { return settings.publicSettingsLoaded },
        async load() {
          const loaded = await settings.fetchPublicSettings(true)
          if (!loaded) throw new Error('Public settings are unavailable')
          return loaded
        }
      })
      app.provide(identityPortalKey, {
        get isSupplierPortal() { return supplierPortal.value },
        safePortalRedirect
      })
      app.provide(identityAuthShellKey, AuthLayout)
      app.provide(vendorPortalResourcePanelsKey, { editAccount: EditAccountModal, bulkEditAccounts: BulkEditAccountModal })
      app.provide(vendorPortalSecurityPanelsKey, { password: ProfilePasswordForm, totp: ProfileTotpCard, passkey: ProfilePasskeyCard })
      app.provide(identityClipboardKey, clipboard.copyToClipboard)
      const services: KeysServices = {
        components: { GroupBadge, GroupOptionItem },
        scopeKey: computed(() => auth.isAuthenticated
          ? JSON.stringify([auth.identityScopeKey, supplierPortal.value ? 'supplier' : 'user']) : null),
        publicSettings: computed(() => settings.cachedPublicSettings),
        getAvailableGroups: userGroupsAPI.getAvailable,
        getUserGroupRates: userGroupsAPI.getUserGroupRates,
        getKeyUsage: getDashboardApiKeysUsage,
        notifySuccess: settings.showSuccess, notifyError: settings.showError,
        copyText: clipboard.copyToClipboard,
        onKeyCreated: () => {
          if (tour.isCurrentStep('[data-tour="key-form-submit"]')) {
            tour.nextStep(500).catch(() => { console.warn('Unable to advance the onboarding tour') })
          }
        },
        codexModels: { buildCatalogUrl: buildCodexModelCatalogUrl, fetchManifest: fetchCodexModelsManifest }
      }
      app.provide(keysServicesKey, services)
      installUsageServices(app, clipboard.copyToClipboard)
      installCatalogServices(app)
      installJobsServices(app, clipboard.copyToClipboard)
      installSiteServices(app, clipboard.copyToClipboard)
      installSystemServices(app)
      installSettingsServices(app)
      installOperationsServices(app, clipboard.copyToClipboard)
      installPaymentsServices(app)
      installResourceServices(app)
      installAccessPolicyServices(app)
      installIdentityUserServices(app)
      disposeSupply = installSupplyServices(app)
    }))
  } catch (error) {
    dispose()
    throw error
  }
  return dispose
}
import { GroupBadge, GroupOptionItem } from '@/features/access-policy/presentation'
