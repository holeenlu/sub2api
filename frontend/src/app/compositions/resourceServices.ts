import { lazyPanel } from './panelLoadingServices'
import { loadModelDistributionChart, loadEndpointDistributionChart } from '@/features/usage/panelLoaders'
import type { App } from 'vue'
import { useAuthStore } from '@/features/identity'
import * as catalog from '@/features/catalog'
import { managedModelsAPI } from '@/features/catalog/managedModels'
import { ModelIcon } from '@/features/catalog/modelIcon'
import { resourceEditorServicesKey, type ResourceEditorServices, excelAuthorizationAPI, openAccountTest } from '@/features/resources'
import { getWebSearchEmulationConfig } from '@/features/system/api'
import { useAppStore } from '@/app/model/app'
import { isVendorOrigin, useSupplyStore } from '@/features/supply'
import { tlsFingerprintProfileAPI } from '@/features/resources/tlsFingerprintProfile'
import grok from '@/features/resources/grok'
import gemini from '@/features/resources/gemini'
import antigravity from '@/features/resources/antigravity'
import cnProviders from '@/features/resources/cnProviders'
import { getClaudeResetCredits, redeemClaudeResetCredit } from '@/features/resources/claudeResetCredits'

const ModelDistributionChart = lazyPanel(loadModelDistributionChart)
const EndpointDistributionChart = lazyPanel(loadEndpointDistributionChart)

export function installResourceServices(app: App): void {
  const auth = useAuthStore()
  const settings = useAppStore()
  const supply = useSupplyStore()
  const services: ResourceEditorServices = {
    components: { GroupBadge, ModelIcon },
    statisticsCharts: { ModelDistributionChart, EndpointDistributionChart },
    scopeKey: () => auth.isAuthenticated ? JSON.stringify([auth.identityScopeKey, import.meta.env.VITE_VENDOR_PORTAL === 'true' || isVendorOrigin(settings.cachedPublicSettings) ? 'supplier' : 'console', supply.vendor?.id, supply.vendor?.policy_version, supply.vendor?.contract_version]) : '',
    notices: { showError: settings.showError, showSuccess: settings.showSuccess, showInfo: settings.showInfo, showWarning: settings.showWarning },
    access: {
      get isSuperAdmin() { return auth.isSuperAdmin },
      get isSimpleMode() { return auth.isSimpleMode },
      get canAuthorize() { return auth.can('accounts.authorize') },
      canEditAdminField: auth.canEditAdminField,
      get canManage() { return auth.isAuthenticated && auth.can('accounts.manage') && import.meta.env.VITE_VENDOR_PORTAL !== 'true' && !isVendorOrigin(settings.cachedPublicSettings) },
      get canReadModelCatalog() { return auth.isAuthenticated && auth.can('accounts.read') && import.meta.env.VITE_VENDOR_PORTAL !== 'true' && !isVendorOrigin(settings.cachedPublicSettings) },
      get canTestProxies() { return auth.can('proxies.manage') }
    },
    catalog: { listPlatforms: catalog.listPlatforms, getProviderProfile: catalog.getProviderProfile, getProviderMode: catalog.getProviderMode, supportsUpstreamModelSync: catalog.supportsUpstreamModelSync, builtin: catalog.BUILTIN_PLATFORM_CATALOG },
    get quotaNotificationEnabled() { return settings.cachedPublicSettings?.account_quota_notify_enabled === true },
    managedModelCatalog: managedModelsAPI.catalog,
    modelPresets: catalog.getPresetMappingsByPlatform,
    tlsProfiles: tlsFingerprintProfileAPI.list,
    async webSearchConfig() { const config = await getWebSearchEmulationConfig(); return { enabled: config.enabled, providers: config.providers.map(provider => provider.type) } },
    credentialAuthorization: { grok, gemini, antigravity },
    providerObservations: cnProviders,
    excelAuthorization: excelAuthorizationAPI,
    claudeCredits: { getClaudeResetCredits, redeemClaudeResetCredit },
    openAccountTest
  }
  app.provide(resourceEditorServicesKey, services)
}
import { GroupBadge } from '@/features/access-policy/presentation'
