import { lazyPanel } from './panelLoadingServices'
import { publicKeyUsageServicesKey } from '@/features/usage'
import { FeatureFlags, isFeatureFlagEnabled } from '@/app/navigation/featureFlags'
import { computed, type App } from 'vue'
import { useAuthStore, usersAPI } from '@/features/identity'
import { keysAPI } from '@/features/keys'
import { usageServicesKey, type UsageServices } from '@/features/usage'
import { accountsAPI } from '@/features/resources'
import { groupsAPI, userGroupsAPI } from '@/features/access-policy'
import { useAppStore } from '@/app/model/app'
import dashboardAPI, { getUserBreakdown, getAPIKeyBreakdown } from '@/features/usage/adminDashboard'
import { opsAPI } from '@/features/operations'
import { isVendorOrigin } from '@/features/supply'
import { loadOpsErrorLogTable, loadOpsErrorDetailModal } from '@/features/operations/panelLoaders'


const OpsErrorLogTable = lazyPanel(loadOpsErrorLogTable)
const OpsErrorDetailModal = lazyPanel(loadOpsErrorDetailModal)
const UserBalanceHistoryModal = lazyPanel(() => import('./user-directory/UserBalanceHistoryModal.vue'))

export function installUsageServices(
  app: App,
  copyText: (text: string, successMessage?: string) => Promise<boolean>
): void {
  const auth = useAuthStore()
  const settings = useAppStore()
  app.provide(publicKeyUsageServicesKey, {
    appearance: computed(() => ({ siteName: settings.siteName, siteLogo: settings.siteLogo, docUrl: settings.cachedPublicSettings?.doc_url || '', subscriptionEnabled: isFeatureFlagEnabled(FeatureFlags.subscription) })),
    ensureSettings: () => settings.fetchPublicSettings(),
    notifyInfo: settings.showInfo, notifySuccess: settings.showSuccess, notifyError: settings.showError
  })
  const services: UsageServices = {
    panels: { balanceHistory: UserBalanceHistoryModal, adminErrorsTable: OpsErrorLogTable, adminErrorDetail: OpsErrorDetailModal },
    scopeKey: computed(() => auth.isAuthenticated ? JSON.stringify([
      auth.identityScopeKey,
      import.meta.env.VITE_VENDOR_PORTAL === 'true' || isVendorOrigin(settings.cachedPublicSettings) ? 'supplier' : 'console'
    ]) : null),
    permissions: computed(() => ({
      isSuperAdmin: auth.isSuperAdmin,
      canExport: auth.can('usage.export'),
      canReadBillingCost: auth.can('billing.cost.read')
    })),
    publicSettings: computed(() => settings.cachedPublicSettings),
    subscriptionEnabled: computed(() => isFeatureFlagEnabled(FeatureFlags.subscription)),
    notifySuccess: settings.showSuccess, notifyError: settings.showError,
    notifyInfo: settings.showInfo, notifyWarning: settings.showWarning, copyText,
    async listUserKeys(page, pageSize, options) {
      const data = await keysAPI.list(page, pageSize, undefined, options)
      return { ...data, items: data.items.map(({ id, name }) => ({ id, name })) }
    },
    getUserGroups: userGroupsAPI.getAvailable,
    // Historical usage must still resolve deleted users, as before the refactor.
    getAdminUser: (id, options) => usersAPI.getById(id, true, options),
    async listAdminGroups(options) {
      return (await groupsAPI.list(1, 1000, undefined, options)).items
    },
    async searchAdminAccounts(keyword, options) {
      const data = await accountsAPI.list(1, 20, { search: keyword }, options)
      return data.items.map(({ id, name }) => ({ id, name }))
    },
    getAdminSnapshot: dashboardAPI.getSnapshotV2,
    getAdminModelStats: dashboardAPI.getModelStats,
    getAdminUserBreakdown: getUserBreakdown,
    getAdminKeyBreakdown: getAPIKeyBreakdown,
    getAdminErrors: (params, options) => opsAPI.listErrorLogs(params, options)
  }
  app.provide(usageServicesKey, services)
}
