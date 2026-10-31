import { lazyPanel } from './panelLoadingServices'
import { peakRatePresentation } from '@/features/pricing'
import { platformLabel, CONCRETE_PLATFORM_OPTIONS } from '@/features/catalog'
import { groupPresentationKey } from '@/features/access-policy/presentation'
import { computed, readonly, type App } from 'vue'
import { useAuthStore, usersAPI } from '@/features/identity'
import { accessPolicyScopeKey, accessPolicyNotificationsKey, accessPolicyPermissionsKey, accessPolicyAccountsKey, accessPolicyUsersKey, riskControlSettingsKey, riskControlProxiesKey, accessPolicyModelSelectorKey } from '@/features/access-policy'
import { accountsAPI, proxiesAPI } from '@/features/resources'
import { loadProxyPicker } from '@/features/resources/panelLoaders'
import { settingsAPI } from '@/features/system/api'
import { useAppStore } from '@/app/model/app'
import { useOnboardingStore } from '@/app/onboarding/store'
import { groupEditorCreatedKey } from './group-editor/services'

const RiskModelSelector = lazyPanel((): Promise<typeof import('./RiskModelSelector.vue')> => import('./RiskModelSelector.vue'))
const ProxyPicker = lazyPanel(loadProxyPicker)

export function installAccessPolicyServices(app: App): void {
  const auth = useAuthStore()
  const settings = useAppStore()
  const tour = useOnboardingStore()
  app.provide(groupPresentationKey, { peakRate: peakRatePresentation, concretePlatformOptions: computed(() => readonly(CONCRETE_PLATFORM_OPTIONS)), platformLabel, serverUtcOffset: computed(() => settings.cachedPublicSettings?.server_utc_offset), isSimpleMode: computed(() => auth.isSimpleMode) })
  app.provide(accessPolicyScopeKey, computed(() => auth.isAuthenticated ? auth.identityScopeKey : ''))
  app.provide(accessPolicyNotificationsKey, { showSuccess: settings.showSuccess, showError: settings.showError })
  app.provide(accessPolicyPermissionsKey, {
    get isSuperAdmin() { return auth.isSuperAdmin }, get isSimpleMode() { return auth.isSimpleMode },
    can: auth.can, canEditAdminField: auth.canEditAdminField, editableAdminFields: auth.editableAdminFields
  })
  app.provide(accessPolicyAccountsKey, {
    async search(keyword, options, signal) {
      const result = await accountsAPI.list(1, 20, { ...options, search: keyword }, { signal })
      return result.items.map(({ id, name }) => ({ id, name }))
    },
    async getById(id, signal) { const account = await accountsAPI.getById(id, { signal }); return { id: account.id, name: account.name } }
  })
  app.provide(accessPolicyUsersKey, { async search(keyword, signal) { return (await usersAPI.list(1, 10, { search: keyword }, { signal })).items } })
  app.provide(riskControlProxiesKey, signal => proxiesAPI.getAll({ signal }))
  app.provide(accessPolicyModelSelectorKey, RiskModelSelector)
  app.provide(accessPolicyProxyPickerKey, ProxyPicker)
  app.provide(riskControlSettingsKey, {
    async enable(context) {
      const current = () => { if (context.signal.aborted || !context.isCurrent()) throw new DOMException('Risk settings scope changed', 'AbortError') }
      current()
      const snapshot = await settingsAPI.getSettings({ signal: context.signal })
      current()
      await settingsAPI.updateSettings({ revision: snapshot.revision, risk_control_enabled: true })
      current()
      await settings.fetchPublicSettings(true)
    }
  })
  app.provide(groupEditorCreatedKey, () => {
    if (tour.isCurrentStep('[data-tour="group-form-submit"]')) {
      tour.nextStep(500).catch(() => settings.showError('Unable to advance the onboarding tour'))
    }
  })
}

import { accessPolicyProxyPickerKey } from '@/features/access-policy'
