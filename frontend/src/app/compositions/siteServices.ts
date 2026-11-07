import { lazyPanel } from './panelLoadingServices'
import { announcementServicesKey } from '@/features/site/announcements'
import { groupsAPI } from '@/features/access-policy'
import { GroupSelector } from '@/features/access-policy/presentation'
import { computed, type App } from 'vue'
import { useAuthStore } from '@/features/identity'
import { siteSettingsKey, siteActorKey, sitePresentationKey } from '@/features/site'
import { loadPlazaGroupSection } from '@/features/catalog/panelLoaders'
import { useModelPlazaQuery } from '@/features/catalog'
import { ModelIcon } from '@/features/catalog/modelIcon'
import { useAppStore } from '@/app/model/app'

const PlazaGroupSection = lazyPanel(loadPlazaGroupSection)

export function installSiteServices(app: App, copy: (text: string) => Promise<boolean>): void {
  const auth = useAuthStore()
  const settings = useAppStore()
  app.provide(announcementServicesKey, {
    scopeKey: computed(() => auth.isAuthenticated ? auth.identityScopeKey : null),
    canManage: computed(() => auth.can('announcements.manage')),
    notifySuccess: settings.showSuccess, notifyError: settings.showError,
    async listSubscriptionGroups(options) {
      return (await groupsAPI.getAll(undefined, options)).filter(group => group.subscription_type === 'subscription')
    },
    GroupSelector
  })
  app.provide(siteSettingsKey, {
    get current() { return settings.cachedPublicSettings },
    get loaded() { return settings.publicSettingsLoaded },
    load: () => settings.fetchPublicSettings()
  })
  app.provide(siteActorKey, {
    get isAuthenticated() { return auth.isAuthenticated },
    get adminLandingPath() { return auth.adminLandingPath },
    get user() { return auth.user ? { email: auth.user.email } : null }
  })
  app.provide(sitePresentationKey, {
    GroupPricingPanel: PlazaGroupSection,
    ModelIcon,
    get brand() { return { name: settings.siteName, logo: settings.siteLogo, apiBaseUrl: settings.apiBaseUrl } },
    copy,
    createModelQuery() {
      const query = useModelPlazaQuery()
      return { data: query.data, loading: query.loading, loadFailed: query.loadFailed, availability: query.availability, load: query.reload }
    }
  })
}
