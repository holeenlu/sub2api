import * as errorPresentation from '@/features/usage/errorPresentation'
import { monitorServicesKey } from '@/features/operations/panels'
import { ModelTagInput } from '@/features/pricing/panels'
import { UsageProgressBar } from '@/features/resources/panels'
import { keysAPI } from '@/features/keys'
import { userGroupsAPI } from '@/features/access-policy'
import { computed, type App } from 'vue'
import { useAuthStore, totpAPI } from '@/features/identity'
import { operationsServicesKey, type OperationsMonitoring } from '@/features/operations'
import { listPlatforms } from '@/features/catalog'
import { groupsAPI } from '@/features/access-policy'
import { accountsAPI } from '@/features/resources'
import { useAppStore } from '@/app/model/app'
import { useAdminSettingsStore } from '@/app/model/adminSettings'

export function installOperationsServices(app: App, copyText: (text: string, message?: string) => Promise<boolean>): void {
  const auth = useAuthStore()
  const settings = useAppStore()
  const admin = useAdminSettingsStore()
  app.provide(monitorServicesKey, {
    canReadAdmin: computed(() => auth.isAuthenticated && auth.can('channel_monitor.read')),
    displaySettings: computed(() => ({
      defaultIntervalSeconds: settings.cachedPublicSettings?.channel_monitor_default_interval_seconds,
      hideThroughput: settings.cachedPublicSettings?.channel_monitor_hide_throughput === true,
      hideUserRanking: settings.cachedPublicSettings?.channel_monitor_hide_user_ranking === true,
      showQuota: settings.cachedPublicSettings?.channel_monitor_show_quota === true
    })),
    async listLinkedAccounts({ provider, search, signal }) {
      const response = await accountsAPI.list(1, 50, { platform: provider, ...(search ? { search } : {}) }, { signal })
      return response.items.map(({ id, name, platform }) => ({ id, name, platform }))
    },
    async getLinkedAccount(id, options) {
      const account = await accountsAPI.getById(id, options)
      return { id: account.id, name: account.name, platform: account.platform }
    },
    async listActiveKeys(options) {
      return (await keysAPI.list(1, 100, { status: 'active' }, options)).items.map(({ id, name, key, status, expires_at, group }) => ({
        id, name, key, status, expires_at,
        group: group ? { id: group.id, name: group.name, platform: group.platform, subscription_type: group.subscription_type, rate_multiplier: group.rate_multiplier } : null
      }))
    },
    getUserGroupRates: userGroupsAPI.getUserGroupRates,
    components: { ModelTagInput, UsageProgressBar, GroupBadge }
  })
  app.provide(operationsServicesKey, {
    errorPresentation,
    scopeKey: computed(() => auth.isAuthenticated ? auth.identityScopeKey : null),
    permissions: computed(() => ({ isSuperAdmin: auth.isSuperAdmin, canManageOps: auth.can('ops.manage'), canReadPromptContent: auth.can('prompt_audit.content.read') })),
    platformOptions: computed(() => listPlatforms().map(({ id, display_name }) => ({ value: id, label: display_name }))),
    monitoring: computed<OperationsMonitoring>(() => ({
      enabled: admin.opsMonitoringEnabled,
      realtimeEnabled: admin.opsRealtimeMonitoringEnabled,
      queryModeDefault: admin.opsQueryModeDefault === 'raw' || admin.opsQueryModeDefault === 'preagg' ? admin.opsQueryModeDefault : 'auto'
    })),
    publicSettings: computed(() => settings.cachedPublicSettings),
    loadMonitoringSettings: options => admin.fetch(false, options),
    disableRealtimeMonitoring: () => admin.setOpsRealtimeMonitoringEnabledLocal(false),
    async listGroups({ includeInactive, signal }) {
      const groups = includeInactive ? await groupsAPI.getAllIncludingInactive({ signal }) : await groupsAPI.getAll(undefined, { signal })
      return groups.map(({ id, name, platform, status }) => ({ id, name, platform, status }))
    },
    async listAccountNames(page, pageSize, options) {
      const response = await accountsAPI.list(page, pageSize, undefined, options)
      return { items: response.items.map(({ id, name }) => ({ id, name })), total: response.total }
    },
    getTotpStatus: options => totpAPI.getStatus(options),
    notifySuccess: settings.showSuccess, notifyError: settings.showError, notifyWarning: settings.showWarning, copyText
  })
}
import { GroupBadge } from '@/features/access-policy/presentation'
